//! Sampler-turn pipeline for `SessionActor`: tool definitions, model auth
//! facts/gates and retry, sampler config reconstruction, sampling-failure
//! recovery, and per-response usage recording.
use super::*;

/// Classify only observed failures that make a provider unsuitable for a
/// *later* new task. This function deliberately does not authorize retry or
/// model switching for the current turn: after any output or side effect,
/// replay would be unsafe.
pub(super) fn provider_failure_kind_for_sampling_error(
    error: &xai_grok_sampler::SamplingErrorInfo,
) -> Option<&'static str> {
    use xai_grok_sampler::SamplingErrorKind;

    if error.status_code == Some(402) {
        return Some("quota_exhausted");
    }
    let message = error.message.to_ascii_lowercase();
    if matches!(error.kind, SamplingErrorKind::Api)
        && [
            "out of credits",
            "run out of credits",
            "spending limit",
            "insufficient balance",
        ]
        .iter()
        .any(|needle| message.contains(needle))
    {
        return Some("quota_exhausted");
    }
    match error.kind {
        SamplingErrorKind::RateLimited => Some("rate_limited"),
        SamplingErrorKind::IdleTimeout => Some("timeout"),
        SamplingErrorKind::Http => Some("upstream"),
        SamplingErrorKind::Api if error.status_code.is_some_and(|status| status >= 500) => {
            Some("upstream")
        }
        _ => None,
    }
}

/// Map only mutations whose cache effect is represented faithfully by the
/// provider-neutral evidence vocabulary. Other durable rewrites still rotate
/// the epoch, but are not mislabeled as a compaction or memory change.
fn wire_mutation_reasons(
    mutation: xai_chat_state::CommittedHistoryMutation,
) -> Vec<lumen_discipline::WireMutationReason> {
    use lumen_discipline::WireMutationReason;
    use xai_chat_state::CommittedHistoryMutation;

    match mutation {
        CommittedHistoryMutation::CompactionReplace => vec![WireMutationReason::FullCompaction],
        CommittedHistoryMutation::RetainedToolPrune => vec![WireMutationReason::ToolResultPruned],
        CommittedHistoryMutation::MemoryReminderPersisted => {
            vec![WireMutationReason::MemoryChanged]
        }
        CommittedHistoryMutation::ConversationReplace
        | CommittedHistoryMutation::HistoryRepair
        | CommittedHistoryMutation::SystemHeadReplace
        | CommittedHistoryMutation::SnapshotRestore
        | CommittedHistoryMutation::Rewind
        | CommittedHistoryMutation::IntegrityRepair => Vec::new(),
    }
}

/// Auth-failure detector for tool errors. Matches strictly on HTTP 401
/// when the error carries a structured status code, mirroring
/// `SamplingError::is_auth_error` in xai-grok-sampling-types: 403 is
/// deliberately excluded because it means "authenticated but forbidden"
/// (content-safety blocks, ZDR-gated requests, remote settings gates), where
/// a token refresh would be a no-op and would surface to the client as
/// a spurious auth_required teardown.
///
/// String fallbacks remain for tools that surface auth failures without
/// going through the structured `HttpFailure` path (e.g. JSON-only
/// `invalid_token` payloads, BYOK key-validation messages).
pub(super) fn is_auth_tool_error(err: &xai_tool_runtime::ToolError) -> bool {
    if let Some(details) = &err.details
        && let Some(status) = details
            .get(HTTP_STATUS_DETAILS_KEY)
            .and_then(|s| s.as_u64())
    {
        return status == 401;
    }
    let lower = err.to_string().to_ascii_lowercase();
    lower.contains("unauthorized")
        || lower.contains("invalid api key")
        || lower.contains("invalid_token")
}
/// Gate inputs bundled with the composed decision so the 401-recovery log can
/// report the components.
#[derive(Clone, Copy)]
struct SessionTokenAuthGate {
    is_session_based: bool,
    model_byok: crate::agent::auth_method::ModelByok,
    /// Whether the request targets a first-party host. Lets an `Unknown`
    /// BYOK status still refresh against cli-chat-proxy / `*.x.ai` without
    /// risking a session-token leak to a third-party BYOK endpoint.
    endpoint_is_first_party: bool,
}
impl SessionTokenAuthGate {
    /// Single place `is_session_based` / `endpoint_is_first_party` are derived,
    /// so all call sites assemble the gate identically.
    fn new(
        auth_method_id: Option<&acp::AuthMethodId>,
        model_byok: crate::agent::auth_method::ModelByok,
        base_url: &str,
    ) -> Self {
        Self {
            is_session_based: auth_method_id
                .is_some_and(crate::agent::auth_method::is_session_based_method),
            model_byok,
            endpoint_is_first_party: crate::util::is_xai_api_url(base_url),
        }
    }
    fn active(self) -> bool {
        crate::agent::auth_method::session_token_auth_gate(
            self.is_session_based,
            self.model_byok,
            self.endpoint_is_first_party,
        )
    }
}
/// Run a tool call; on an auth-shaped failure, attempt recovery via
/// `AuthManager` and one retry. When `shared_recovery` is `Some`, concurrent
/// 401s in the same batch deduplicate via `OnceCell::get_or_init`.
pub(super) async fn call_with_auth_retry<F, Fut>(
    auth_manager: Option<&std::sync::Arc<crate::auth::AuthManager>>,
    shared_recovery: Option<&tokio::sync::OnceCell<bool>>,
    tool_name: &str,
    mut call: F,
) -> Result<xai_grok_tools::types::output::ToolRunResult, xai_tool_runtime::ToolError>
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<
            Output = Result<
                xai_grok_tools::types::output::ToolRunResult,
                xai_tool_runtime::ToolError,
            >,
        >,
{
    let result = call().await;
    let Err(ref err) = result else { return result };
    if !is_auth_tool_error(err) {
        return result;
    }
    let Some(am) = auth_manager else {
        return result;
    };
    let src = crate::auth::recovery::RecoverySource::Background;
    let recovered = match shared_recovery {
        Some(cell) => *cell.get_or_init(|| am.try_recover_unauthorized(src)).await,
        None => am.try_recover_unauthorized(src).await,
    };
    if recovered {
        tracing::info!(
            tool = tool_name,
            "auth recovery: tool 401, recovered, retrying"
        );
        call().await
    } else {
        tracing::warn!(tool = tool_name, "auth recovery: tool 401, refresh failed");
        xai_grok_telemetry::unified_log::warn(
            "auth recovery: tool 401, refresh failed",
            None,
            Some(serde_json::json!({ "tool": tool_name })),
        );
        result
    }
}
impl SessionActor {
    /// Resolve the domain that scopes a provider cache epoch. It is recomputed
    /// from live session state rather than cached in `SamplerConfig`, because
    /// model, permission and tool changes are dynamic session facts.
    pub(super) async fn cache_domain(&self) -> crate::session::cache_epoch::CacheDomain {
        let config = self.reconstruct_full_config().await;
        let definitions = self.prepare_tool_definitions_inner().await;
        let specs = self.turn_base_tool_specs(&definitions);
        let credential_scope = self
            .auth_method_id
            .load()
            .as_deref()
            .map(|method| format!("{method:?}"));
        crate::session::cache_epoch::CacheDomain {
            // The configured auth scheme identifies the provider contract without
            // retaining a bearer or API-key-derived identity.
            provider: format!("{:?}", config.auth_scheme),
            base_url: config.base_url,
            backend: format!("{:?}", config.api_backend),
            model: config.model,
            effective_effort: config.reasoning_effort.map(|value| format!("{:?}", value)),
            credential_scope,
            permission_domain: self.permission_mode_label().to_owned(),
            tool_manifest_fingerprint: crate::session::cache_epoch::ordered_manifest_fingerprint(
                &specs,
            ),
        }
    }

    /// Ensure a durable epoch exists for the current domain. Startup uses this
    /// path to retain a validated record or rotate a stale one; fork admission
    /// forces a fresh epoch.
    pub(super) async fn load_cache_epoch(
        &self,
    ) -> std::io::Result<crate::session::cache_epoch::CacheEpochRecord> {
        let domain = self.cache_domain().await;
        let is_fork = self.startup_hints.inherited_prefix_len.is_some();
        crate::session::cache_epoch::load_or_rotate(
            &crate::session::persistence::session_dir(&self.session_info),
            &domain,
            is_fork,
        )
        .map(|(record, _)| record)
    }

    /// Rotate only after ChatState has committed a durable history rewrite.
    pub(super) async fn rotate_cache_epoch_after_history_mutation(
        &self,
        mutation: xai_chat_state::CommittedHistoryMutation,
    ) -> std::io::Result<crate::session::cache_epoch::CacheEpochRecord> {
        let domain = self.cache_domain().await;
        crate::session::cache_epoch::rotate_after_history_mutation(
            &crate::session::persistence::session_dir(&self.session_info),
            &domain,
            wire_mutation_reasons(mutation),
        )
    }

    pub(super) async fn prepare_tool_definitions_timed(&self) -> (Vec<ToolDefinition>, u64) {
        let mcp_wait_start = std::time::Instant::now();
        match self.mcp_strategy {
            McpInitStrategy::Blocking => {
                if !self.mcp_state.lock().await.is_initialized() {
                    tracing::info!(
                        "Blocking strategy: waiting for MCP initialization before first prompt..."
                    );
                    self.wait_for_mcp_initialized().await;
                }
            }
            McpInitStrategy::Progressive => {}
        }
        let mcp_wait_ms = mcp_wait_start.elapsed().as_millis() as u64;
        let defs = self.prepare_tool_definitions_inner().await;
        (defs, mcp_wait_ms)
    }
    pub(super) async fn prepare_tool_definitions(&self) -> Vec<ToolDefinition> {
        self.prepare_tool_definitions_timed().await.0
    }
    /// The exact tool specs a turn sends, BEFORE the turn-specific
    /// structured-output append. Single source of truth shared by the turn
    /// (`acp_session_impl/turn.rs`) and the `SnapshotToolDefinitions` handler, so
    /// a verbatim-fork child's tool prefix can never silently drift from what the
    /// parent turn actually sends. `defs` is the already-resolved tool list
    /// (`prepare_tool_definitions_*`); this applies only the `web_search` drop
    /// under backend search and the `ToolSpec::from` mapping.
    pub(crate) fn turn_base_tool_specs(&self, defs: &[ToolDefinition]) -> Vec<ToolSpec> {
        let backend_search_active = self.backend_search_active();
        defs.iter()
            .filter(|td| !backend_search_active || td.function.name != "web_search")
            .cloned()
            .map(ToolSpec::from)
            .collect()
    }
    /// Hosted tools with overrides applied, plus the applied overrides to echo, in one pass.
    fn resolve_hosted(
        &self,
    ) -> (
        Vec<xai_grok_sampling_types::HostedTool>,
        xai_grok_sampling_types::ToolOverrides,
    ) {
        let mut tools = self.agent.borrow().hosted_tools().to_vec();
        let applied = xai_grok_sampling_types::apply_tool_overrides(
            &mut tools,
            self.tool_overrides.borrow().as_ref(),
        );
        (tools, applied)
    }
    /// Ungated. Prefer [`Self::hosted_tools_for_turn`], which folds in the backend-search gate.
    pub(crate) fn effective_hosted_tools(&self) -> Vec<xai_grok_sampling_types::HostedTool> {
        self.resolve_hosted().0
    }
    pub(crate) fn hosted_tools_for_turn(&self) -> Vec<xai_grok_sampling_types::HostedTool> {
        if self.backend_search_active() {
            self.effective_hosted_tools()
        } else {
            Vec::new()
        }
    }
    /// The applied overrides to echo, or `None` when backend search is off.
    pub(crate) fn effective_tool_overrides(
        &self,
    ) -> Option<xai_grok_sampling_types::ToolOverrides> {
        if !self.backend_search_active() {
            return None;
        }
        let applied = self.resolve_hosted().1;
        (!applied.is_empty()).then_some(applied)
    }
    pub(crate) fn backend_search_active(&self) -> bool {
        self.agent.borrow().backend_search_enabled() && self.supports_backend_search.get()
    }
    /// Set the per-turn override and emit it before any turn runs, so a subagent spawned this turn
    /// inherits it.
    pub(crate) fn set_tool_overrides(&self, overrides: xai_grok_sampling_types::ToolOverrides) {
        *self.tool_overrides.borrow_mut() = Some(overrides);
        self.emit_resolved_tool_overrides();
    }
    /// Fold a per-turn update at promotion: an object sets, `null` clears to the seed, absent leaves.
    pub(crate) fn apply_tool_overrides_update(
        &self,
        update: Option<xai_grok_sampling_types::ToolOverridesUpdate>,
    ) {
        let Some(update) = update else { return };
        {
            let mut slot = self.tool_overrides.borrow_mut();
            *slot = update.apply(slot.take());
        }
        self.emit_resolved_tool_overrides();
    }
    /// Store this session's cutoff in the cell a subagent spawn reads. Not gated on backend search,
    /// so a bounded parent bounds a searching child even if it isn't searching.
    pub(crate) fn emit_resolved_tool_overrides(&self) {
        let seed = self.agent.borrow().definition().tool_overrides.clone();
        let effective = resolve_configured_cutoff(seed, self.tool_overrides.borrow().as_ref());
        self.resolved_tool_overrides
            .store((!effective.is_empty()).then(|| std::sync::Arc::new(effective)));
    }
    pub(super) async fn prepare_tool_definitions_inner(&self) -> Vec<ToolDefinition> {
        let bridge = self.agent.borrow().tool_bridge().clone();
        let defs = bridge.tool_definitions_builtins_only().await;
        let plan_active = self.plan_mode.lock().is_active();
        filter_cursor_tools_by_plan_mode(defs, plan_active)
    }
    pub(super) fn model_auth_facts(&self, model_id: &str) -> crate::agent::config::ModelAuthFacts {
        self.model_auth_state(model_id).0
    }
    pub(super) fn model_auth_provider(
        &self,
        model_id: &str,
    ) -> Option<crate::auth::AuthProviderRef> {
        self.model_auth_state(model_id).1
    }
    /// Drop the memoized per-model auth state; see [`Self::model_auth_memo`]
    /// for why each model/credential chokepoint must call this.
    pub(crate) fn invalidate_model_auth_memo(&self) {
        self.model_auth_memo.replace(None);
    }
    /// Reads and populates [`Self::model_auth_memo`]; a fresh `Unknown`
    /// falls back to the last definite entry (see the field's contract).
    fn model_auth_state(
        &self,
        model_id: &str,
    ) -> (
        crate::agent::config::ModelAuthFacts,
        Option<crate::auth::AuthProviderRef>,
    ) {
        use crate::agent::auth_method::ModelByok;
        use crate::session::acp_session::ModelAuthMemo;
        if let Some(memo) = self.model_auth_memo.borrow().as_ref()
            && memo.model_id == model_id
            && memo.facts.byok != ModelByok::Unknown
        {
            return (memo.facts, memo.provider.clone());
        }
        let (fresh, provider) =
            crate::agent::config::resolve_model_auth_facts_and_provider(model_id);
        if fresh.byok == ModelByok::Unknown {
            if let Some(memo) = self.model_auth_memo.borrow().as_ref()
                && memo.model_id == model_id
            {
                return (memo.facts, memo.provider.clone());
            }
            return (fresh, provider);
        }
        *self.model_auth_memo.borrow_mut() = Some(ModelAuthMemo {
            model_id: model_id.to_string(),
            facts: fresh,
            provider: provider.clone(),
        });
        (fresh, provider)
    }
    /// The single writer of a provider mint/rotation into chat-state credentials.
    async fn set_chat_api_key(&self, new_key: String) {
        let mut creds = self.chat_state_handle.get_credentials().await;
        creds.api_key = Some(new_key);
        self.chat_state_handle.update_credentials(creds);
    }
    /// Pre-turn arm for a provider-backed model: mint on a cold cache,
    /// re-mint near expiry, and adopt a rotation chat-state missed. No-op
    /// when `current_key` is already the fresh cached token.
    async fn refresh_provider_token_pre_turn(
        &self,
        provider: &crate::auth::AuthProviderRef,
        current_key: Option<&str>,
        model_id: &str,
    ) {
        match provider.ensure_fresh_token(current_key).await {
            crate::auth::ProviderRefreshOutcome::Rotated(new_key) => {
                tracing::info!(
                    model = %model_id,
                    provider = %provider.name,
                    cold = current_key.is_none(),
                    "auth provider token rotated pre-turn"
                );
                self.set_chat_api_key(new_key).await;
            }
            crate::auth::ProviderRefreshOutcome::Unchanged => {}
            crate::auth::ProviderRefreshOutcome::MintFailed => {
                tracing::warn!(
                    session_id = %self.session_info.id.0,
                    provider = %provider.name,
                    model = %model_id,
                    "auth provider pre-turn refresh failed"
                );
                xai_grok_telemetry::unified_log::warn(
                    "auth provider pre-turn refresh failed",
                    Some(self.session_info.id.0.as_ref()),
                    Some(serde_json::json!({
                        "provider": provider.name,
                        "model": model_id,
                        "cold": current_key.is_none(),
                    })),
                );
            }
            crate::auth::ProviderRefreshOutcome::Unusable => {}
        }
    }
    /// 401 arm for a provider-backed model: re-run the helper once and
    /// resubmit. A missing key means the cold mint failed and the request
    /// went out unauthenticated, so mint instead. Returns `false` when the
    /// fresh-mint guard blocked the re-run or the helper failed; the 401
    /// then surfaces as a terminal error.
    async fn try_provider_401_recovery(&self, provider: &crate::auth::AuthProviderRef) -> bool {
        let rejected_key = self.chat_state_handle.get_credentials().await.api_key;
        let recovered = match rejected_key {
            Some(ref rejected_key) => provider.recover_rejected_token(rejected_key).await,
            None => provider.ensure_fresh_token(None).await.rotated(),
        };
        let Some(new_key) = recovered else {
            tracing::warn!(
                session_id = %self.session_info.id.0,
                provider = %provider.name,
                "auth recovery: sampler 401, provider re-mint declined or failed"
            );
            xai_grok_telemetry::unified_log::warn(
                "auth recovery: sampler 401, provider re-mint declined or failed",
                Some(self.session_info.id.0.as_ref()),
                Some(serde_json::json!({ "provider": provider.name })),
            );
            return false;
        };
        tracing::info!(
            session_id = %self.session_info.id.0,
            provider = %provider.name,
            "auth recovery: sampler 401, auth provider re-mint, retrying"
        );
        xai_grok_telemetry::unified_log::info(
            "auth recovery: sampler 401, auth provider re-mint, retrying",
            Some(self.session_info.id.0.as_ref()),
            None,
        );
        self.set_chat_api_key(new_key).await;
        true
    }
    /// Gate inputs for `model_id` routed to `base_url`. See
    /// [`crate::agent::auth_method::session_token_auth_gate`] for the rationale
    /// (`base_url` keeps an `Unknown` BYOK status refreshable only
    /// against first-party xAI hosts).
    fn auth_gate(&self, model_id: &str, base_url: &str) -> SessionTokenAuthGate {
        let byok = self.model_auth_facts(model_id).byok;
        let auth_method = self.auth_method_id.load();
        SessionTokenAuthGate::new(auth_method.as_deref(), byok, base_url)
    }
    /// Emit a unified-log breadcrumb whenever the session-token refresh gate is
    /// evaluated with an **`Unknown`** per-model BYOK status on a session-based
    /// method — the condition that (pre-fix) silently demoted live sessions to
    /// stale-token 401s. The uploaded per-turn unified log then shows whether
    /// the first-party-endpoint fallback kept refresh active or withheld it, so
    /// we can confirm the fix works (or catch a residual demotion) per session
    /// even when server-side metrics only show the aggregate 401. No-op for a
    /// definite `Byok`/`NotByok`, so steady-state turns stay quiet — a burst of
    /// these is itself the signal that `Unknown` is being hit in the field.
    fn log_auth_gate_unknown(&self, site: &str, gate: SessionTokenAuthGate, base_url: &str) {
        use crate::agent::auth_method::ModelByok;
        if gate.model_byok != ModelByok::Unknown || !gate.is_session_based {
            return;
        }
        let refresh_active = gate.active();
        let ctx = serde_json::json!({
            "site": site,
            "model_byok": gate.model_byok.as_str(),
            "is_session_based": gate.is_session_based,
            "endpoint_is_first_party": gate.endpoint_is_first_party,
            "refresh_active": refresh_active,
            "base_url": base_url,
        });
        let sid = Some(self.session_info.id.0.as_ref());
        if refresh_active {
            xai_grok_telemetry::unified_log::info(
                "auth gate: Unknown BYOK on first-party endpoint — session-token refresh kept active",
                sid,
                Some(ctx),
            );
        } else {
            xai_grok_telemetry::unified_log::warn(
                "auth gate: Unknown BYOK on non-first-party endpoint — refresh withheld (may surface stale-token 401)",
                sid,
                Some(ctx),
            );
        }
    }
    /// Reconstruct a full `SamplerConfig` (with credentials) by combining
    /// the actor's `SamplingConfig` and `Credentials`. Folds in the
    /// URL-derived headers (cli-chat-proxy auth, the staging auth header)
    /// so the sampler crate stays URL-agnostic.
    pub(super) async fn reconstruct_full_config(&self) -> SamplingConfig {
        #[allow(clippy::items_after_statements)]
        #[derive(Debug)]
        struct TraceContextInjector;
        impl xai_grok_sampler::HeaderInjector for TraceContextInjector {
            fn inject(&self, headers: &mut reqwest::header::HeaderMap) {
                if let Some(tp) = xai_file_utils::trace_context::current_traceparent()
                    && let Ok(v) = reqwest::header::HeaderValue::from_str(&tp)
                {
                    headers.insert("traceparent", v);
                }
            }
        }
        let cfg = self
            .chat_state_handle
            .get_sampling_config()
            .await
            .unwrap_or_else(|| xai_grok_sampling_types::SamplingConfig {
                base_url: String::new(),
                model: String::new(),
                max_completion_tokens: None,
                temperature: None,
                top_p: None,
                api_backend: Default::default(),
                extra_headers: Default::default(),
                query_params: Default::default(),
                env_http_headers: Default::default(),
                context_window: std::num::NonZeroU64::new(256_000).unwrap(),
                reasoning_effort: None,
                stream_tool_calls: None,
            });
        let creds = self.chat_state_handle.get_credentials().await;
        let model_facts = self.model_auth_facts(cfg.model.as_str());
        let auth_method = self.auth_method_id.load();
        let gate =
            SessionTokenAuthGate::new(auth_method.as_deref(), model_facts.byok, &cfg.base_url);
        let use_bearer_resolver = gate.active();
        self.log_auth_gate_unknown("reconstruct_full_config", gate, &cfg.base_url);
        if use_bearer_resolver && let Some(am) = self.auth_manager.as_ref() {
            let _ = am.auth().await;
        }
        let api_key = if use_bearer_resolver {
            self.auth_manager
                .as_ref()
                .and_then(|am| am.current_wire_valid().map(|a| a.key))
        } else {
            creds.api_key
        };
        let auth_scheme = model_facts.auth_scheme;
        let mut extra_headers = cfg.extra_headers;
        crate::agent::config::inject_url_derived_headers(
            &mut extra_headers,
            creds.alpha_test_key.as_deref(),
            &cfg.base_url,
        );
        let compaction_at_tokens = self.compaction_at_tokens.get();
        let compactions_remaining = self.compactions_remaining.get();
        if compactions_remaining.is_some() || compaction_at_tokens.is_some() {
            let has_compaction_summary = self
                .chat_state_handle
                .get_last_compaction_prompt_index()
                .await
                .is_some();
            if let Some(value) =
                compactions_remaining.and_then(|c| c.resolve(has_compaction_summary))
            {
                extra_headers.insert("x-compactions-remaining".to_string(), value.to_string());
            }
            if !has_compaction_summary
                && let Some(value) = compaction_at_tokens.and_then(|c| {
                    c.resolve(
                        cfg.context_window.get(),
                        self.compaction.threshold_percent.get(),
                    )
                })
            {
                extra_headers.insert("x-compaction-at".to_string(), value.to_string());
            }
        }
        SamplingConfig {
            api_key,
            base_url: cfg.base_url,
            model: cfg.model,
            max_completion_tokens: cfg.max_completion_tokens,
            temperature: cfg.temperature,
            top_p: cfg.top_p,
            api_backend: cfg.api_backend,
            auth_scheme,
            extra_headers,
            query_params: cfg.query_params.clone(),
            env_http_headers: cfg.env_http_headers.clone(),
            context_window: cfg.context_window.get(),
            client_version: creds.client_version,
            reasoning_effort: cfg.reasoning_effort,
            force_http1: false,
            // P0: a turn without a durable provider-attempt receipt is never
            // safe to replay automatically. The actor policy is the second
            // ceiling; keep this per-turn config explicit as well.
            max_retries: Some(NO_RECEIPT_MAX_RETRIES),
            stream_tool_calls: cfg.stream_tool_calls.unwrap_or(false),
            idle_timeout_secs: None,
            client_identifier: self.client_identifier.clone(),
            deployment_id: crate::managed_config::resolve_deployment_id(
                crate::managed_config::resolve_deployment_key().as_deref(),
            ),
            user_id: self
                .auth_manager
                .as_ref()
                .and_then(|am| am.current_or_expired())
                .filter(|a| a.is_xai_auth())
                .map(|a| a.user_id),
            origin_client: self.origin_client.clone(),
            attribution_callback: self.attribution_callback.clone(),
            bearer_resolver: if use_bearer_resolver {
                self.auth_manager.as_ref().map(|am| {
                    crate::auth::credential_provider::WireValidBearerResolver::shared(am.clone())
                })
            } else {
                None
            },
            supports_backend_search: self.supports_backend_search.get(),
            compactions_remaining: self.compactions_remaining.get(),
            compaction_at_tokens: self.compaction_at_tokens.get(),
            doom_loop_recovery: self.doom_loop_recovery,
            header_injector: Some(std::sync::Arc::new(TraceContextInjector)),
            request_observer: Some(crate::session::cache_epoch::durable_request_observer(
                crate::session::persistence::session_dir(&self.session_info),
            )),
        }
    }
    /// Install auto-mode permission classifier with a live LLM side-query
    /// (laziness-classifier pattern: `prepare_chat_completion` +
    /// `conversation_collect` on a LocalSet task; channel bridges the
    /// `Send` permission actor). Heuristic runs only when the side-query
    /// errors or returns unparseable text.
    pub(crate) async fn wire_permission_auto_llm_classifier(self: &Arc<Self>) {
        if !self.permissions.is_auto_mode() {
            return;
        }
        if self.permissions.has_llm_side_query() {
            return;
        }
        let auto_cfg = crate::util::config::resolve_auto_mode_config_from_disk();
        let session_model = self
            .chat_state_handle
            .get_sampling_config()
            .await
            .map(|c| c.model)
            .unwrap_or_default();
        let aux_classifier_sampler = match auto_cfg.classifier_model.as_deref() {
            Some(slug) => self.resolve_auto_classifier_sampler(slug).await,
            None => None,
        };
        let models = self.models_manager.models();
        let effective_supports_re = crate::agent::config::effective_classifier_supports_re(
            aux_classifier_sampler
                .as_ref()
                .map(|(_, model)| model.as_str()),
            &session_model,
            &models,
        );
        let (prompt_type, classifier_reasoning_effort) =
            crate::util::config::auto_mode_classifier_defaults(&auto_cfg, effective_supports_re);
        let classify_timeout = crate::util::config::auto_mode_classify_timeout(&auto_cfg);
        let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel::<(
            Vec<xai_grok_workspace::permission::ClassifierMessage>,
            tokio::sync::oneshot::Sender<
                Result<String, xai_grok_workspace::permission::ClassifierFailure>,
            >,
        )>();
        let session = Arc::clone(self);
        tokio::task::spawn_local(async move {
            while let Some((messages, respond_to)) = rx.recv().await {
                let result = async {
                    let (sampling_client, model) = match &aux_classifier_sampler {
                        Some((client, model)) => (client.clone(), model.clone()),
                        None => {
                            let client = session
                                .prepare_chat_completion(false)
                                .await
                                .map_err(|e| xai_grok_workspace::permission::ClassifierFailure::TransportError(
                                    e.to_string(),
                                ))?;
                            let model = session
                                .chat_state_handle
                                .get_sampling_config()
                                .await
                                .map(|c| c.model)
                                .unwrap_or_default();
                            (client, model)
                        }
                    };
                    let session_id = session.session_info.id.to_string();
                    let items = messages
                        .into_iter()
                        .map(|m| match m.role {
                            xai_grok_workspace::permission::ClassifierMessageRole::System => {
                                ConversationItem::system(m.text)
                            }
                            xai_grok_workspace::permission::ClassifierMessageRole::User => {
                                ConversationItem::user(m.text)
                            }
                        })
                        .collect::<Vec<_>>();
                    let request = ConversationRequest {
                        items,
                        tools: vec![],
                        hosted_tools: vec![],
                        tool_choice: None,
                        model: Some(model),
                        temperature: None,
                        max_output_tokens: None,
                        json_schema: Some(
                            xai_grok_workspace::permission::classifier_output_json_schema(),
                        ),
                        reasoning_effort: classifier_reasoning_effort,
                        x_grok_conv_id: Some(
                            format!("perm-classifier-{}", uuid::Uuid::new_v4()),
                        ),
                        x_grok_req_id: Some(
                            format!("xai-perm-auto-{}", uuid::Uuid::new_v4()),
                        ),
                        x_grok_session_id: Some(session_id),
                        x_grok_agent_id: Some(xai_grok_telemetry::id::agent_id()),
                        ..ConversationRequest::default()
                    };
                    let fut = sampling_client.conversation_collect(request);
                    let response = tokio::time::timeout(classify_timeout, fut)
                        .await
                        .map_err(|_| {
                            xai_grok_workspace::permission::ClassifierFailure::Timeout
                        })?
                        .map_err(|e| xai_grok_workspace::permission::ClassifierFailure::TransportError(
                            e.to_string(),
                        ))?;
                    Ok(response.assistant_text())
                }
                    .await;
                if let Err(error) = &result {
                    tracing::warn!(%error, "permission auto classifier side-query failed");
                }
                let _ = respond_to.send(result);
            }
        });
        let clf =
            xai_grok_workspace::permission::LlmPermissionClassifier::with_channel(tx, prompt_type);
        debug_assert!(
            clf.has_side_query(),
            "channel-wired classifier must report has_side_query"
        );
        self.permissions.set_classifier_with_side_query(clf, true);
        tracing::info!(
            session_id = %self.session_info.id,
            "Wired live LLM permission auto-mode classifier (session sampling channel)"
        );
    }
    /// Resolve a standalone aux-model `SamplerConfig` for `slug` via the shared
    /// catalog routing (Tier-1 catalog creds / Tier-2 xAI-proxy via session token
    /// / `XAI_API_KEY` / deployment key), gathering the session-local auth context
    /// once. Shared by image-describe and the classifier so the gather can't
    /// drift. `None` ⇒ caller falls back to the session model.
    pub(super) async fn resolve_aux_sampler_config(
        &self,
        slug: &str,
    ) -> Option<xai_grok_sampler::SamplerConfig> {
        let creds = self.chat_state_handle.get_credentials().await;
        let session_key = self
            .auth_manager
            .as_ref()
            .and_then(|am| am.current_or_expired().map(|a| a.key.clone()));
        let models = self.models_manager.models();
        let endpoints = self.models_manager.endpoints();
        let disable_api_key_auth = self
            .auth_manager
            .as_ref()
            .map(|am| am.grok_com_config().api_key_auth_disabled())
            .unwrap_or(false);
        crate::agent::config::resolve_aux_model_sampling_config(
            slug,
            &models,
            &endpoints,
            session_key.as_deref(),
            disable_api_key_auth,
            creds.alpha_test_key.clone(),
            creds.client_version.clone(),
        )
    }
    /// Resolve a dedicated sampler for the Auto-mode classifier model `slug`,
    /// stamping session-local auth/attribution like image-describe (which relies
    /// on the resolver, not a config override, for `base_url`/`api_backend` so
    /// credentials stay consistent). `None` ⇒ caller falls back to the session
    /// client + model.
    async fn resolve_auto_classifier_sampler(
        &self,
        slug: &str,
    ) -> Option<(xai_grok_sampler::SamplingClient, String)> {
        let active_session_config = self.reconstruct_full_config().await;
        let mut cfg = self.resolve_aux_sampler_config(slug).await?;
        crate::agent::config::stamp_session_local_sampler_fields(
            &mut cfg,
            &active_session_config,
            self.client_identifier.clone(),
            Some(self.max_retries),
        );
        let model = cfg.model.clone();
        let client = xai_grok_sampler::SamplingClient::new(cfg)
            .map_err(|e| {
                tracing::warn!(error = %e, "auto classifier aux sampler build failed; using session model")
            })
            .ok()?;
        Some((client, model))
    }
    #[tracing::instrument(
        name = "session.prepare_chat_completion",
        skip_all,
        fields(force_http1)
    )]
    pub(super) async fn prepare_chat_completion(
        &self,
        force_http1: bool,
    ) -> Result<xai_grok_sampler::SamplingClient, acp::Error> {
        self.refresh_token_if_expired().await;
        let mut full_config = self.reconstruct_full_config().await;
        full_config.force_http1 = force_http1;
        let sampling_client =
            xai_grok_sampler::SamplingClient::new(full_config).map_err(|e| self.to_acp_error(e))?;
        Ok(sampling_client)
    }
    /// Push a fresh `SamplerConfig` into the per-session sampler actor
    /// before each turn. Mirrors `prepare_chat_completion`'s
    /// auth-refresh + config rebuild, but routes the result to the
    /// `xai-grok-sampler` instead of constructing a new
    /// `OaiCompatClient`.
    ///
    /// Behaviour parity: we run the same `refresh_token_if_expired()`
    /// and `reconstruct_full_config()` so the sampler picks up any
    /// newly issued session token. The previous client cache inside
    /// the sampler actor is invalidated automatically by
    /// `update_config`.
    pub(crate) async fn prepare_sampler_for_turn(&self) {
        self.refresh_token_if_expired().await;
        let mut sampler_config = self.reconstruct_full_config().await;
        sampler_config.max_retries = Some(NO_RECEIPT_MAX_RETRIES);
        sampler_config.doom_loop_recovery = None;
        if self.tool_context.task_output_token_budget.is_some()
            || self.tool_context.sampler_retry_only_before_output
        {
            sampler_config.doom_loop_recovery = None;
        }
        sampler_config.idle_timeout_secs = Some(self.inference_idle_timeout.as_secs());
        self.sampler_handle.update_config(sampler_config);
    }

    /// Apply the user-selected ordinary model pool once, before the first
    /// sampler submission of a root turn. A tool-loop continuation is not a
    /// new admission and must not silently change its model mid-turn.
    pub(crate) async fn maybe_select_ordinary_model_for_task(
        &self,
        task: &str,
        is_initial_root_sampling_attempt: bool,
    ) {
        if !is_initial_root_sampling_attempt
            || self.tool_context.subagent_depth > 0
            || self.models_manager.user_selected_model()
        {
            return;
        }
        let policy = self.models_manager.model_routing_config();
        if !policy.enabled || policy.model_pool.is_empty() {
            return;
        }
        let Some(next_model) = self.models_manager.select_healthy_model_for_task(
            &policy.model_pool,
            &policy.priority,
            &policy.task_preferences,
            task,
        ) else {
            return;
        };
        let active = self.reconstruct_full_config().await;
        if active.model == next_model {
            return;
        }
        let Some(mut config) = self.resolve_aux_sampler_config(&next_model).await else {
            return;
        };
        crate::agent::config::stamp_session_local_sampler_fields(
            &mut config,
            &active,
            self.client_identifier.clone(),
            Some(NO_RECEIPT_MAX_RETRIES),
        );
        if self
            .handle_set_session_model(
                config,
                false,
                false,
                true,
                self.compaction.threshold_percent.get(),
            )
            .await
            .is_ok()
        {
            self.models_manager
                .set_current_model_id_for_routing(acp::ModelId::new(next_model.clone()));
            xai_grok_telemetry::unified_log::info(
                "model routing: selected ordinary turn model from user pool",
                Some(self.session_info.id.0.as_ref()),
                Some(serde_json::json!({
                    "to_model": next_model,
                    "reason": if policy.priority.is_empty() { "task_policy" } else { "user_priority" },
                })),
            );
        }
    }

    fn log_terminal_failure(&self, error_type: &str, status_code: Option<u16>, message: &str) {
        let auth = self
            .auth_manager
            .as_ref()
            .and_then(|am| am.current_or_expired());
        let reauthable = is_reauthable_failure(Some(error_type), message);
        xai_grok_telemetry::unified_log::warn(
            "turn.terminal_failure",
            Some(self.session_info.id.0.as_ref()),
            Some(serde_json::json!({
                "error_type": error_type,
                "status_code": status_code,
                "reauthable": reauthable,
                "auth_mode": auth.as_ref().map(|a| format!("{:?}", a.auth_mode)),
                "key_prefix": auth.as_ref().map(|a| crate::auth::token_suffix(&a.key).to_owned()),
                "expires_at": auth
                    .as_ref()
                    .and_then(|a| a.expires_at.map(|e| e.to_rfc3339())),
                "message": crate::util::truncate(message, 300),
            })),
        );
    }
    pub(crate) async fn handle_sampling_failure(
        self: &Arc<Self>,
        error: xai_grok_sampler::SamplingErrorInfo,
    ) -> Result<std::convert::Infallible, acp::Error> {
        use xai_grok_sampler::SamplingErrorKind;
        if self.tool_context.task_output_token_budget.is_some() {
            self.tool_context.fail_task_output_usage_closed();
            let message = format!(
                "budgeted workflow child model request failed; output grant exhausted: {}",
                error.message
            );
            self.log_terminal_failure("output_budget_usage_unknown", error.status_code, &message);
            return Err(acp::Error::internal_error().data(message));
        }
        if self.tool_context.sampler_retry_only_before_output {
            let handle = self.chat_state_handle.clone();
            tokio::spawn(async move {
                let _ = handle.mark_usage_incomplete(true, true).await;
            });
            let message = format!(
                "workflow child model request failed; usage may understate real spend: {}",
                error.message
            );
            self.log_terminal_failure(
                "workflow_child_sampling_failed",
                error.status_code,
                &message,
            );
            return Err(acp::Error::internal_error().data(message));
        }
        let compact_would_be_required = self.should_compact_on_error(&error).await;
        let mut detailed_message = error.message.clone();
        if compact_would_be_required {
            let context_window = error
                .model_metadata
                .as_ref()
                .and_then(|metadata| metadata.context_window)
                .expect("should_compact_on_error guarantees context_window");
            tracing::warn!(
                session_id = %self.session_info.id.0,
                context_window,
                "context overflow is terminal until a new task; refusing error-triggered compaction and replay without a provider-attempt receipt"
            );
            detailed_message.push_str(
                "\n\nThe request was not automatically compacted or resubmitted. Start a new task to retry after reviewing the context." ,
            );
        }
        let (failed_model_id, failed_base_url) = self
            .chat_state_handle
            .get_sampling_config()
            .await
            .map(|c| (c.model, c.base_url))
            .unwrap_or_default();
        // Failure escalation: repeated ordinary-turn failures must never read
        // as "stuck". The counter resets on the next successful response.
        let consecutive_failures = self
            .tool_context
            .consecutive_sampling_failures
            .fetch_add(1, std::sync::atomic::Ordering::Relaxed)
            + 1;
        let provider_failure_kind = provider_failure_kind_for_sampling_error(&error);
        if let Some(kind) = provider_failure_kind {
            self.models_manager
                .record_provider_failure(&failed_base_url, kind);
            tracing::info!(
                session_id = %self.session_info.id.0,
                failed_model = %failed_model_id,
                failure_kind = kind,
                "model routing: recorded provider failure for a later root admission; refusing same-turn reroute"
            );
        }
        if matches!(error.kind, SamplingErrorKind::Api)
            && error.status_code == Some(400)
            && error.message.contains("encrypted_content")
        {
            self.signals_handle()
                .record_error_typed("encrypted_content_mismatch");
            let friendly = "This session's conversation history is incompatible \
                            with the current model. Please start a new session."
                .to_string();
            self.log_terminal_failure("encrypted_content_mismatch", error.status_code, &friendly);
            self.send_xai_notification(XaiSessionUpdate::RetryState(
                crate::extensions::notification::RetryState::Failed {
                    error_type: "encrypted_content_mismatch".to_string(),
                    message: friendly.clone(),
                },
            ))
            .await;
            return Err(acp::Error::invalid_params().data(friendly));
        }
        if matches!(error.kind, SamplingErrorKind::RateLimited) {
            self.log_terminal_failure("rate_limited", error.status_code, &detailed_message);
            self.send_xai_notification(XaiSessionUpdate::RetryState(
                crate::extensions::notification::RetryState::Exhausted {
                    attempts: 0,
                    reason: detailed_message.clone(),
                    is_rate_limited: true,
                },
            ))
            .await;
            let acp_err = acp::Error::new(
                crate::sampling::error::RATE_LIMITED_ERROR_CODE,
                "Rate limited".to_string(),
            )
            .data(detailed_message);
            return Err(acp_err);
        }
        let auth_provider =
            if matches!(error.kind, SamplingErrorKind::Auth) || error.status_code == Some(401) {
                self.model_auth_provider(&failed_model_id)
            } else {
                None
            };
        let auth_recovery_eligible = matches!(error.kind, SamplingErrorKind::Auth) && {
            let gate = self.auth_gate(&failed_model_id, &failed_base_url);
            let eligible = gate.active();
            self.log_auth_gate_unknown("handle_sampling_failure", gate, &failed_base_url);
            if !eligible && auth_provider.is_none() {
                tracing::warn!(
                    session_id = %self.session_info.id.0,
                    is_session_based = gate.is_session_based,
                    model_byok = gate.model_byok.as_str(),
                    endpoint_is_first_party = gate.endpoint_is_first_party,
                    "auth recovery: sampler 401 not refreshable (api-key auth) — surfacing 401",
                );
                xai_grok_telemetry::unified_log::warn(
                    "auth recovery: sampler 401 not eligible (api-key auth)",
                    Some(self.session_info.id.0.as_ref()),
                    Some(serde_json::json!({
                        "kind": error.kind.as_str(),
                        "status_code": error.status_code,
                        "is_session_based": gate.is_session_based,
                        "model_byok": gate.model_byok.as_str(),
                        "endpoint_is_first_party": gate.endpoint_is_first_party,
                    })),
                );
            }
            eligible
        };
        debug_assert!(
            !(auth_recovery_eligible && auth_provider.is_some()),
            "a provider-backed model must not be session-recovery-eligible"
        );
        if !matches!(error.kind, SamplingErrorKind::Auth)
            && error.status_code == Some(401)
            && auth_provider.is_none()
        {
            xai_grok_telemetry::unified_log::warn(
                "auth recovery: sampler 401 not eligible (non-auth error kind)",
                Some(self.session_info.id.0.as_ref()),
                Some(serde_json::json!({
                    "kind": error.kind.as_str(),
                    "status_code": error.status_code,
                })),
            );
        }
        let mut credentials_refreshed_for_next_task = false;
        if auth_recovery_eligible
            && crate::auth::devbox_login::is_devbox_environment()
            && let Some(ref am) = self.auth_manager
        {
            match am.try_devbox_recovery().await {
                Ok(auth) => {
                    tracing::info!(
                        session_id = %self.session_info.id.0,
                        user_id = %auth.user_id,
                        "auth recovery: sampler 401, devbox re-mint succeeded for a later task; refusing same-turn replay"
                    );
                    credentials_refreshed_for_next_task = true;
                }
                Err(e) => {
                    tracing::warn!(
                        session_id = %self.session_info.id.0,
                        error = %e,
                        "auth recovery: sampler 401, devbox re-mint failed"
                    );
                    xai_grok_telemetry::unified_log::warn(
                        "auth recovery: sampler 401, devbox re-mint failed",
                        Some(self.session_info.id.0.as_ref()),
                        Some(serde_json::json!({ "error": format!("{e}") })),
                    );
                }
            }
        }
        if !credentials_refreshed_for_next_task
            && auth_recovery_eligible
            && let Some(ref am) = self.auth_manager
        {
            if am
                .try_recover_unauthorized(crate::auth::recovery::RecoverySource::Turn)
                .await
            {
                tracing::info!(session_id = %self.session_info.id.0, "auth recovery: sampler 401, recovered for a later task; refusing same-turn replay");
                xai_grok_telemetry::unified_log::info(
                    "auth recovery: sampler 401, recovered for a later task; refusing same-turn replay",
                    Some(self.session_info.id.0.as_ref()),
                    None,
                );
                credentials_refreshed_for_next_task = true;
            }
            tracing::warn!(session_id = %self.session_info.id.0, "auth recovery: sampler 401, refresh failed");
            xai_grok_telemetry::unified_log::warn(
                "auth recovery: sampler 401, refresh failed",
                Some(self.session_info.id.0.as_ref()),
                None,
            );
        }
        if !credentials_refreshed_for_next_task
            && let Some(ref provider) = auth_provider
            && self.try_provider_401_recovery(provider).await
        {
            tracing::info!(
                session_id = %self.session_info.id.0,
                provider = %provider.name,
                "auth recovery: provider credential refreshed for a later task; refusing same-turn replay"
            );
            credentials_refreshed_for_next_task = true;
        }
        if matches!(error.kind, SamplingErrorKind::IdleTimeout) {
            self.signals_handle().record_idle_timeout();
        }
        if matches!(error.kind, SamplingErrorKind::EmptyResponse) {
            if let Some(ref ctx) = error.empty_response_context {
                tracing::warn!(
                    empty_response = true,
                    empty_reason = ctx.reason.as_str(),
                    had_reasoning = ctx.had_reasoning,
                    content_len = ctx.content_len,
                    tool_call_count = ctx.tool_call_count,
                    completion_tokens = ctx.completion_tokens.unwrap_or(0),
                    reasoning_tokens = ctx.reasoning_tokens.unwrap_or(0),
                    finish_reason = ctx.finish_reason_str(),
                    first_choice_seen = ctx.first_choice_seen,
                    model = %ctx.model,
                    "empty response after retries exhausted: {reason}",
                    reason = ctx.reason,
                );
                {
                    let mut cap = self.streaming_turn_capture.lock();
                    cap.reasoning_tokens = ctx.reasoning_tokens;
                    cap.completion_tokens = ctx.completion_tokens;
                    cap.finish_reason = ctx.finish_reason.clone();
                    cap.empty_reason = Some(ctx.reason.as_str().to_owned());
                }
            }
            self.signals_handle().record_error_typed("empty_response");
        }
        let auth_mode = self
            .auth_manager
            .as_ref()
            .and_then(|am| am.current())
            .map(|a| a.auth_mode)
            .unwrap_or(crate::auth::AuthMode::ApiKey);
        let auth_mode_str = format!("{auth_mode:?}");
        let client_version = xai_grok_version::VERSION;
        if auth_mode == crate::auth::AuthMode::WebLogin {
            let msg = format!(
                "{detailed_message}\n\n\
                 You are using a deprecated authentication method (WebLogin).\n\
                 This auth method is no longer supported and will cause errors.\n\n\
                 To fix: run `grok logout` then `grok login` to re-authenticate with OAuth2.\n\n\
                 Version: {client_version}"
            );
            self.log_terminal_failure("legacy_auth", error.status_code, &msg);
            self.send_xai_notification(XaiSessionUpdate::RetryState(
                crate::extensions::notification::RetryState::Failed {
                    error_type: "legacy_auth".to_string(),
                    message: msg.clone(),
                },
            ))
            .await;
            return Err(acp::Error::internal_error().data(msg));
        }
        let is_model_404 =
            error.status_code == Some(404) && detailed_message.contains("does not exist");
        let is_auth_401 =
            error.status_code == Some(401) || matches!(error.kind, SamplingErrorKind::Auth);
        let detailed_message = if is_model_404 || is_auth_401 {
            let current_model = self
                .chat_state_handle
                .get_sampling_config()
                .await
                .map(|c| c.model)
                .unwrap_or_else(|| "unknown".to_string());
            let available: Vec<String> = self
                .models_manager
                .models()
                .values()
                .map(|m| m.model.clone())
                .collect();
            let mut msg = format!("{detailed_message}\n");
            msg.push_str(&format!("\n  Model:     {current_model}"));
            msg.push_str(&format!("\n  Auth:      {auth_mode_str}"));
            if let Some(ref provider) = auth_provider {
                msg.push_str(
                    &format!(
                    "\n  Provider:  [auth_provider.{}] (check the provider command and the debug log)",
                    provider.name
                ),
                );
            }
            msg.push_str(&format!("\n  Version:   {client_version}"));
            if available.is_empty() {
                msg.push_str("\n  Available: (none)");
            } else {
                msg.push_str(&format!("\n  Available: {}", available.join(", ")));
            }
            if is_model_404 && !available.iter().any(|m| m == &current_model) {
                msg.push_str(&format!(
                    "\n\n  '{}' is not in your available models.",
                    current_model
                ));
                msg.push_str("\n  Switch models with /model or start a new session.");
            }
            msg
        } else {
            detailed_message
        };
        let error_type = if xai_grok_sampling_types::is_context_length_error(&error.message) {
            "context_length"
        } else {
            error.kind.as_str()
        };
        let (error_type, detailed_message) = if error_type == "auth"
            && self
                .auth_manager
                .as_ref()
                .is_some_and(|am| !am.requires_manual_reauth())
        {
            xai_grok_telemetry::unified_log::info(
                "auth: turn failure downgraded to auth_transient (refreshable credential present)",
                Some(self.session_info.id.0.as_ref()),
                Some(serde_json::json!({ "status_code": error.status_code })),
            );
            (
                "auth_transient",
                format!(
                    "{detailed_message}\n\nAuthentication is temporarily unavailable \
                     (often a network blip right after wake). Your session is still \
                     signed in. This failed request was not automatically resubmitted; \
                     submit a new task to retry."
                ),
            )
        } else {
            (error_type, detailed_message)
        };
        let detailed_message = if credentials_refreshed_for_next_task {
            format!(
                "{detailed_message}\n\nCredentials were refreshed for a new task, but this request was not automatically resubmitted."
            )
        } else {
            format!(
                "{detailed_message}\n\nThis request was not automatically resubmitted because no provider-attempt receipt can prove replay is safe. Submit a new task to retry."
            )
        };
        // Escalation guidance turns repeated identical failures into an
        // actionable hint instead of a silent wall of the same error.
        let is_auth_failure = matches!(error.kind, SamplingErrorKind::Auth)
            || error.status_code == Some(401);
        let model_pinned = self.models_manager.user_selected_model();
        let provider_degraded = matches!(
            self.models_manager.provider_health(&failed_base_url),
            crate::agent::models::ProviderHealthSnapshot::Degraded { .. }
        );
        let detailed_message = match crate::session::nextgen_control::failure_escalation_guidance(
            consecutive_failures,
            is_auth_failure,
            error.status_code,
            model_pinned,
            provider_degraded,
        ) {
            Some(guidance) => format!("{detailed_message}\n\n{guidance}"),
            None => detailed_message,
        };
        self.log_terminal_failure(error_type, error.status_code, &detailed_message);
        self.send_xai_notification(XaiSessionUpdate::RetryState(
            crate::extensions::notification::RetryState::Failed {
                error_type: error_type.to_string(),
                message: detailed_message.clone(),
            },
        ))
        .await;
        Err(
            acp::Error::internal_error().data(crate::sampling::error::terminal_error_data(
                detailed_message,
                error.status_code,
                error.kind,
            )),
        )
    }
    /// Drive a single turn through the sampler-based path.
    ///
    /// Calls `prepare_sampler_for_turn` first (auth refresh + config
    /// push), then submits via `SamplerHandle::submit_and_collect` and
    /// returns a model response or a terminal failure already reported via
    /// `send_xai_notification(RetryState::Failed)`.
    ///
    /// S8: every failure path seals a durable attempt receipt. A **clean**
    /// seal (zero output / zero tools / complete observation) that admits
    /// under P4b may authorize **one** same-turn resubmit after auth
    /// refresh. All other failures stay terminal (INV-11 / P0-NR-A).
    /// Sampler actor `max_retries` remains [`NO_RECEIPT_MAX_RETRIES`] so
    /// transport-level replay cannot reopen without this admission gate.
    pub(crate) async fn run_turn_via_sampler(
        self: &Arc<Self>,
        request: ConversationRequest,
        is_initial_root_sampling_attempt: bool,
    ) -> Result<
        (
            Box<ConversationResponse>,
            Box<xai_grok_sampler::InferenceLatencyStats>,
        ),
        acp::Error,
    > {
        let task_hint = request
            .items
            .iter()
            .rev()
            .find_map(|item| match item {
                ConversationItem::User(_) => Some(item.text_content()),
                _ => None,
            })
            .unwrap_or_default();
        self.maybe_select_ordinary_model_for_task(&task_hint, is_initial_root_sampling_attempt)
            .await;

        // Shell-level auth-class retries already used this turn (bounded by
        // durable clean seal admission; independent of sampler max_retries).
        let mut auth_class_retries_used: u32 = 0;
        loop {
            self.prepare_sampler_for_turn().await;
            let stream_drained_rx = {
                let (tx, rx) = tokio::sync::oneshot::channel();
                *self.turn_stream_drained.lock() = Some(tx);
                rx
            };
            let request_id = xai_grok_sampler::RequestId::random();
            let request_id_str = request_id.as_str().to_string();
            let mut seal =
                crate::session::nextgen_control::begin_attempt_seal(request_id_str.clone());
            let session_dir =
                crate::session::persistence::session_dir(&self.session_info);
            let wire_context = {
                let domain = self.cache_domain().await;
                match crate::session::cache_epoch::load_or_rotate(&session_dir, &domain, false) {
                    Ok((epoch, _)) => Some(lumen_discipline::WireObservationContext {
                        cache_domain_hash: domain.fingerprint(),
                        cache_epoch_id: epoch.epoch_id.to_string(),
                        mutation_reasons:
                            crate::session::cache_epoch::take_pending_mutation_reasons(
                                &session_dir,
                                epoch.epoch_id,
                            )
                            .unwrap_or_else(|error| {
                                tracing::warn!(%error, "cache mutation attribution unavailable for wire observation");
                                Vec::new()
                            }),
                    }),
                    Err(error) => {
                        tracing::warn!(%error, "cache epoch unavailable for wire observation");
                        None
                    }
                }
            };
            match self
                .sampler_handle
                .submit_and_collect_with_wire_context(
                    request_id,
                    request.clone(),
                    wire_context,
                )
                .await
            {
                Ok((response, metrics)) => {
                    let span = tracing::Span::current();
                    span.record("request_id", request_id_str.as_str());
                    if let Some(ttft) = metrics.time_to_first_token_ms {
                        span.record("ttft_ms", ttft as i64);
                    }
                    if metrics.attempts > 0 {
                        span.record("attempt", i64::from(metrics.attempts));
                    }
                    if tokio::time::timeout(std::time::Duration::from_secs(5), stream_drained_rx)
                        .await
                        .is_err()
                    {
                        self.turn_stream_drained.lock().take();
                        tracing::warn!(
                            "stream-drain barrier timed out; proceeding to emit tool \
                             calls (eventId ordering may be imperfect this turn)"
                        );
                    }
                    // A successful sampling response resets the consecutive-failure
                    // escalation counter for the session model.
                    self.tool_context
                        .consecutive_sampling_failures
                        .store(0, std::sync::atomic::Ordering::Relaxed);
                    return Ok((Box::new(response), Box::new(metrics)));
                }
                Err(rich_err) => {
                    self.turn_stream_drained.lock().take();
                    let info = xai_grok_sampler::SamplingErrorInfo::from(&rich_err);

                    // S8: seal observations from streaming capture (fail-closed).
                    let (had_output, had_tool_call, observation_complete) =
                        self.seal_observations_from_streaming_capture();
                    seal.apply_failure_observations(
                        had_output,
                        had_tool_call,
                        observation_complete,
                    );
                    let store_path = session_dir.join("sealed-attempt-receipts.json");
                    let store = xai_grok_memory::SealedAttemptReceiptStore::with_path(store_path);
                    let (receipt, authority) = crate::session::nextgen_control::seal_and_authority(
                        &store,
                        seal.receipt().clone(),
                        Some(request_id_str.clone()),
                        Some(self.session_info.id.0.to_string()),
                    );
                    let model_pinned = self.models_manager.user_selected_model();
                    let is_auth = matches!(
                        info.kind,
                        xai_grok_sampler::SamplingErrorKind::Auth
                    ) || info.status_code == Some(401);

                    // Live P4b side conditions (pool / breaker / stale advice).
                    let failed_base_url = self
                        .chat_state_handle
                        .get_sampling_config()
                        .await
                        .map(|c| c.base_url)
                        .unwrap_or_default();
                    let side = self.collect_p4b_live_side_conditions(&failed_base_url);

                    // Bounded auth-class same-turn resubmit only when P4b
                    // admission grants remaining budget (clean durable seal,
                    // not pinned, pool/breaker/advice ok). GROK_MAX_RETRIES is
                    // not used here — shell auth-class ceiling is the seal
                    // budget (1).
                    let admission = crate::session::nextgen_control::authorize_ordinary_retry_budget(
                        &crate::session::nextgen_control::ordinary_retry_admission(
                            Some(&receipt),
                            authority,
                            model_pinned,
                            side.pool_exhausted,
                            side.breaker_open,
                            side.stale_advice,
                            xai_grok_memory::DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES,
                            auth_class_retries_used,
                        ),
                    );
                    let refresh_ok = if is_auth && admission.is_ok() {
                        self.try_auth_refresh_for_clean_seal_retry().await
                    } else {
                        false
                    };
                    match crate::session::nextgen_control::decide_auth_class_retry(
                        is_auth,
                        admission,
                        refresh_ok,
                        auth_class_retries_used,
                    ) {
                        crate::session::nextgen_control::AuthClassRetryAction::Resubmit {
                            next_used,
                        } => {
                            auth_class_retries_used = next_used;
                            tracing::info!(
                                session_id = %self.session_info.id.0,
                                attempt_id = %receipt.attempt_id,
                                auth_class_retries_used,
                                "S8: clean durable seal authorized one auth-class same-turn resubmit"
                            );
                            xai_grok_telemetry::unified_log::info(
                                "s8.auth_class_retry.authorized",
                                Some(self.session_info.id.0.as_ref()),
                                Some(serde_json::json!({
                                    "attempt_id": receipt.attempt_id,
                                    "auth_class_retries_used": auth_class_retries_used,
                                })),
                            );
                            continue;
                        }
                        crate::session::nextgen_control::AuthClassRetryAction::Terminal {
                            reason,
                        } => {
                            tracing::debug!(
                                session_id = %self.session_info.id.0,
                                attempt_id = %receipt.attempt_id,
                                reason,
                                kind = info.kind.as_str(),
                                "S8: auth-class retry terminal; sealed receipt recorded"
                            );
                        }
                    }

                    let never = self.handle_sampling_failure(info).await?;
                    match never {}
                }
            }
        }
    }

    /// Live P4b side conditions from models_manager + advice epoch atomics.
    ///
    /// - pool_exhausted: routing enabled, non-empty pool, no healthy candidate
    /// - breaker_open: provider domain is passively Degraded
    /// - stale_advice: advice issued under an older live_policy_epoch
    pub(crate) fn collect_p4b_live_side_conditions(
        &self,
        base_url: &str,
    ) -> crate::session::nextgen_control::P4bLiveSideConditions {
        use std::sync::atomic::Ordering;
        let policy = self.models_manager.model_routing_config();
        let any_healthy_in_pool = if policy.enabled && !policy.model_pool.is_empty() {
            self.models_manager
                .select_healthy_model_from_pool(
                    &policy.model_pool,
                    &policy.priority,
                    "",
                )
                .is_some()
        } else {
            // Pool not in play → not exhausted.
            true
        };
        let provider_degraded = matches!(
            self.models_manager.provider_health(base_url),
            crate::agent::models::ProviderHealthSnapshot::Degraded { .. }
        );
        let live_epoch = self
            .tool_context
            .live_policy_epoch
            .load(Ordering::Relaxed);
        let issued_raw = self
            .tool_context
            .advice_issued_policy_epoch
            .load(Ordering::Relaxed);
        let issued = if issued_raw == 0 {
            None
        } else {
            Some(issued_raw)
        };
        crate::session::nextgen_control::derive_p4b_side_conditions(
            policy.enabled,
            policy.model_pool.len(),
            any_healthy_in_pool,
            provider_degraded,
            issued,
            live_epoch,
        )
    }

    /// Positive observations from the turn's streaming capture for S8 seal.
    ///
    /// Returns `(had_output, had_tool_call, observation_complete)`.
    /// Tool-call signal is true when capture phase is [`CapturePhase::ToolCall`]
    /// or any retained segment ended in that phase (INV-11). Observation is
    /// complete when we hold the capture mutex after the stream-drain oneshot
    /// was taken (failure path always takes the oneshot).
    fn seal_observations_from_streaming_capture(&self) -> (bool, bool, bool) {
        use crate::session::acp_session::CapturePhase;
        let cap = self.streaming_turn_capture.lock();
        let had_output = !cap.response_text.is_empty()
            || !cap.reasoning_text.is_empty()
            || cap.text_chunks > 0
            || cap.reasoning_chunks > 0
            || cap.reasoning_tokens.is_some()
            || cap.completion_tokens.is_some()
            || cap.segments.iter().any(|s| {
                !s.response_text.is_empty()
                    || !s.reasoning_text.is_empty()
                    || s.text_chunks > 0
                    || s.reasoning_chunks > 0
            });
        let had_tool_call = cap.phase == CapturePhase::ToolCall
            || cap
                .segments
                .iter()
                .any(|s| s.phase == CapturePhase::ToolCall);
        let observation_complete = true;
        (had_output, had_tool_call, observation_complete)
    }

    /// Best-effort credential refresh for a clean-seal auth-class retry.
    /// Returns true only when a refresh path reports success.
    async fn try_auth_refresh_for_clean_seal_retry(&self) -> bool {
        let (failed_model_id, failed_base_url) = self
            .chat_state_handle
            .get_sampling_config()
            .await
            .map(|c| (c.model, c.base_url))
            .unwrap_or_default();
        let gate = self.auth_gate(&failed_model_id, &failed_base_url);
        if gate.active()
            && let Some(ref am) = self.auth_manager
            && am
                .try_recover_unauthorized(crate::auth::recovery::RecoverySource::Turn)
                .await
        {
            return true;
        }
        if let Some(provider) = self.model_auth_provider(&failed_model_id)
            && self.try_provider_401_recovery(&provider).await
        {
            return true;
        }
        false
    }
    /// Proactively refresh the auth token if near expiry.
    ///
    /// Session-token path is best-effort: on success, update credentials and
    /// return. On failure, do **not** fall through to the JWT/config.toml
    /// branch when the session gate was active — that path is for BYOK JWTs
    /// only. Falling through after a failed session refresh left hard-expired
    /// opaque tokens (External/OIDC) on the wire and guaranteed a 401.
    /// Soft failures with a still-usable access token still return here
    /// (grace / optimistic send); 401 recovery remains the safety net.
    pub(crate) async fn refresh_token_if_expired(&self) {
        if let Some(ref am) = self.auth_manager {
            let creds = self.chat_state_handle.get_credentials().await;
            let (model_id, base_url) = self
                .chat_state_handle
                .get_sampling_config()
                .await
                .map(|c| (c.model, c.base_url))
                .unwrap_or_default();
            if self.auth_gate(&model_id, &base_url).active() {
                match am.get_valid_token().await {
                    Ok(key) => {
                        if creds.api_key.as_deref() != Some(&key) {
                            let mut creds = creds;
                            creds.api_key = Some(key);
                            self.chat_state_handle.update_credentials(creds);
                        }
                        self.clear_auth_compact_suppression();
                        return;
                    }
                    Err(e) => {
                        let hard_expired = !am.has_usable_token();
                        if hard_expired && creds.api_key.is_some() {
                            let mut cleared = creds;
                            cleared.api_key = None;
                            self.chat_state_handle.update_credentials(cleared);
                        }
                        tracing::warn!(
                            error = %e,
                            hard_expired,
                            model = %model_id,
                            "auth: preflight get_valid_token failed"
                        );
                        xai_grok_telemetry::unified_log::warn(
                            "auth.preflight.refresh_failed",
                            Some(self.session_info.id.0.as_ref()),
                            Some(serde_json::json!({
                                "error": format!("{e}"),
                                "hard_expired": hard_expired,
                                "model": model_id,
                            })),
                        );
                        return;
                    }
                }
            }
        } else {
            xai_grok_telemetry::unified_log::debug(
                "token refresh skipped: no auth manager",
                Some(self.session_info.id.0.as_ref()),
                None,
            );
        }
        use crate::auth::{is_jwt_expired_or_near, parse_jwt_expiration};
        const REFRESH_THRESHOLD: chrono::Duration = chrono::Duration::minutes(5);
        let creds = self.chat_state_handle.get_credentials().await;
        let current_key = creds.api_key;
        let current_model_id = self
            .chat_state_handle
            .get_sampling_config()
            .await
            .map(|c| c.model)
            .unwrap_or_default();
        if let Some(provider) = self.model_auth_provider(&current_model_id) {
            self.refresh_provider_token_pre_turn(
                &provider,
                current_key.as_deref(),
                &current_model_id,
            )
            .await;
            return;
        }
        let Some(ref key) = current_key else { return };
        if !is_jwt_expired_or_near(key, REFRESH_THRESHOLD) {
            if let Some(exp) = parse_jwt_expiration(key) {
                let remaining_secs = (exp - chrono::Utc::now()).num_seconds();
                tracing::debug!(
                    model = %current_model_id,
                    remaining_secs,
                    "JWT token valid, no refresh needed"
                );
            } else {
                tracing::debug!(
                    model = %current_model_id,
                    key_len = key.len(),
                    "Token is not a JWT, expiry-based refresh not applicable"
                );
            }
            return;
        }
        let remaining_secs =
            parse_jwt_expiration(key).map_or(0, |exp| (exp - chrono::Utc::now()).num_seconds());
        tracing::info!(
            model = %current_model_id,
            remaining_secs,
            "JWT near expiry, refreshing from config.toml"
        );
        let Some(new_key) = self.reload_api_key_from_config(&current_model_id) else {
            return;
        };
        if key == &new_key {
            tracing::warn!(
                model = %current_model_id,
                "Config.toml returned same token (not yet rotated by external process?)"
            );
            return;
        }
        let new_remaining_secs = parse_jwt_expiration(&new_key)
            .map_or(0, |exp| (exp - chrono::Utc::now()).num_seconds());
        tracing::info!(
            model = %current_model_id,
            new_remaining_secs,
            key_len = new_key.len(),
            "Refreshed API token from config.toml"
        );
        let mut creds = self.chat_state_handle.get_credentials().await;
        creds.api_key = Some(new_key);
        self.chat_state_handle.update_credentials(creds);
    }
    fn reload_api_key_from_config(&self, current_model_id: &str) -> Option<String> {
        let raw_config = crate::config::load_effective_config()
            .map_err(|e| tracing::warn!(error = %e, "Failed to reload config"))
            .ok()?;
        let config = crate::agent::config::Config::new_from_toml_cfg(&raw_config)
            .map_err(|e| tracing::warn!(error = %e, "Failed to parse reloaded config.toml"))
            .ok()?;
        let config_model = config
            .config_models
            .iter()
            .find(|(k, v)| v.model.as_deref().unwrap_or(k.as_str()) == current_model_id)
            .map(|(_, v)| v);
        let Some(model) = config_model else {
            tracing::warn!(
                model = %current_model_id,
                available = ?config.config_models.keys().collect::<Vec<_>>(),
                "Model not found in config.toml [model.*]"
            );
            return None;
        };
        let key = crate::agent::config::first_own_credential(
            model.api_key.as_deref(),
            model.env_key.as_ref(),
        );
        if key.is_none() {
            tracing::warn!(
                model = %current_model_id,
                env_key = ?model.env_key,
                "No api_key or env_key resolved for model"
            );
        }
        key
    }
    /// Propagate the model-reported token usage from a turn response into
    /// chat state, the per-prompt usage ledger, and per-turn signals.
    ///
    /// This is the only place per-turn `total_tokens` is refreshed in the
    /// post-sampler-refactor path; without it `state.total_tokens` would
    /// stay frozen at the `estimate_conversation_tokens` seed from
    /// `ChatState::new`, freezing `/context` and corrupting the resume
    /// restore that reads `meta.totalTokens` from `updates.jsonl`.
    /// Resetting `estimated_tokens_since_model = 0` here also keeps the
    /// preflight-overflow guard accurate against the next turn's
    /// tool-result deltas.
    pub(crate) fn record_response_token_usage(
        &self,
        response: &ConversationResponse,
        api_duration_ms: Option<u64>,
    ) {
        if let Some(ref u) = response.usage {
            self.tool_context
                .record_task_model_output(u64::from(u.completion_tokens));
            self.chat_state_handle
                .record_token_usage(u64::from(u.total_tokens));
            self.chat_state_handle.record_last_turn_usage(u.clone());
            self.chat_state_handle.record_model_call_usage(
                response.assistant().and_then(|a| a.model_id.clone()),
                u.clone(),
                api_duration_ms,
                response.cost_usd_ticks,
            );
            self.signals_handle()
                .record_token_usage(u.completion_tokens, u.reasoning_tokens);
        } else if self.tool_context.task_output_token_budget.is_some() {
            self.tool_context.fail_task_output_usage_closed();
            let handle = self.chat_state_handle.clone();
            tokio::spawn(async move {
                let _ = handle.mark_usage_incomplete(true, true).await;
            });
        } else if self.tool_context.sampler_retry_only_before_output {
            let handle = self.chat_state_handle.clone();
            tokio::spawn(async move {
                let _ = handle.mark_usage_incomplete(true, true).await;
            });
        }
    }
    pub(super) async fn record_assistant_response(&self, assistant_item: ConversationItem) {
        self.signals_handle().record_assistant_message();
        if let ConversationItem::Assistant(ref a) = assistant_item {
            tracing::info!(model_id = ?a.model_id, "DEBUG record_assistant_response model_id");
        }
        if let ConversationItem::Assistant(ref a) = assistant_item
            && let Some(first_call) = a.tool_calls.first()
        {
            tracing::info!("Assistant requested tool call: {}", first_call.id);
        }
        self.chat_state_handle
            .push_assistant_response(assistant_item);
    }
}
/// Per-tool precedence: a non-empty `over` wins, else the non-empty `seed`.
fn prefer_non_empty<T>(
    over: Option<T>,
    seed: Option<T>,
    is_empty: impl Fn(&T) -> bool,
) -> Option<T> {
    over.filter(|o| !is_empty(o))
        .or_else(|| seed.filter(|s| !is_empty(s)))
}
/// The cutoff a subagent inherits: a non-empty per-turn `base` wins per tool, else the `seed`.
fn resolve_configured_cutoff(
    seed: Option<xai_grok_sampling_types::ToolOverrides>,
    base: Option<&xai_grok_sampling_types::ToolOverrides>,
) -> xai_grok_sampling_types::ToolOverrides {
    use xai_grok_sampling_types::{ToolOverrides, WebSearchOptions, XSearchOptions};
    let ToolOverrides {
        x_search: seed_x,
        web_search: seed_w,
    } = seed.unwrap_or_default();
    let (over_x, over_w) =
        base.map_or((None, None), |b| (b.x_search.clone(), b.web_search.clone()));
    ToolOverrides {
        x_search: prefer_non_empty(over_x, seed_x, XSearchOptions::is_empty),
        web_search: prefer_non_empty(over_w, seed_w, WebSearchOptions::is_empty),
    }
}
#[cfg(test)]
mod configured_cutoff_tests {
    use xai_grok_sampling_types::{
        SearchDateBound, ToolOverrides, WebSearchOptions, XSearchOptions,
    };
    fn x_cut(to: &str) -> XSearchOptions {
        XSearchOptions {
            date_bound: Some(SearchDateBound::new(None, Some(to.into())).unwrap()),
        }
    }
    #[test]
    fn seed_only_is_inherited_without_a_per_turn_update() {
        let seed = ToolOverrides {
            x_search: Some(x_cut("2020-01-01")),
            web_search: None,
        };
        assert_eq!(
            super::resolve_configured_cutoff(Some(seed.clone()), None),
            seed
        );
    }
    #[test]
    fn non_empty_base_wins_per_tool_and_empty_reverts_to_seed() {
        let seed = ToolOverrides {
            x_search: Some(x_cut("2020-01-01")),
            web_search: Some(WebSearchOptions {
                allowed_domains: Some(vec!["x.com".into()]),
            }),
        };
        let base = ToolOverrides {
            x_search: Some(x_cut("2019-06-01")),
            web_search: Some(WebSearchOptions {
                allowed_domains: Some(vec![]),
            }),
        };
        let got = super::resolve_configured_cutoff(Some(seed.clone()), Some(&base));
        assert_eq!(got.x_search, Some(x_cut("2019-06-01")));
        assert_eq!(got.web_search, seed.web_search);
    }
    /// The contamination invariant: `resolve_configured_cutoff` (inheritance) must resolve the same
    /// bound the wire/echo path (`apply_tool_overrides`) does for the same seed and per-turn base.
    /// Two independent precedence implementations, so drift on the inherited boundary fails CI.
    #[test]
    fn inherited_cutoff_agrees_with_the_wire_echo() {
        use xai_grok_sampling_types::{HostedTool, apply_tool_overrides};
        let web = WebSearchOptions {
            allowed_domains: Some(vec!["x.com".into()]),
        };
        let cases = [
            (
                Some(ToolOverrides {
                    x_search: Some(x_cut("2020-01-01")),
                    web_search: None,
                }),
                None,
            ),
            (
                Some(ToolOverrides {
                    x_search: Some(x_cut("2020-01-01")),
                    web_search: Some(web.clone()),
                }),
                Some(ToolOverrides {
                    x_search: Some(x_cut("2019-06-01")),
                    web_search: None,
                }),
            ),
            (
                None,
                Some(ToolOverrides {
                    x_search: Some(x_cut("2018-01-01")),
                    web_search: Some(web.clone()),
                }),
            ),
        ];
        for (seed, base) in cases {
            let mut tools = vec![
                HostedTool::WebSearch { options: None },
                HostedTool::XSearch { options: None },
            ];
            apply_tool_overrides(&mut tools, seed.as_ref());
            let wire_echo = apply_tool_overrides(&mut tools, base.as_ref());
            let inherited = super::resolve_configured_cutoff(seed.clone(), base.as_ref());
            assert_eq!(wire_echo, inherited, "seed={seed:?} base={base:?}");
        }
    }
}
