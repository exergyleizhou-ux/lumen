//! Host-side call surfaces for NextGen pure control planes (offline-safe).
//!
//! These wrappers let shell / operator paths exercise Advisor shadow and
//! Kairos supervisor contracts without a second runtime and without claiming
//! durable daemon or exact-binary product readiness.

use xai_grok_memory::{
    AdviceReportV1, AdvisorContextCapsuleV1, AdvisorDeny, AdvisorMode, AdvisorRequestV1,
    AdvisorUsageReceiptV1, AttemptSealTracker, ConsultBlockReason, ConsultOutcome,
    DurableSealAuthority, KairosCommand, KairosDeny, KairosSupervisorState, LoopPhase,
    RetryAdmissionRequest, RetryDenyReason, SEALED_RECEIPT_SCHEMA_VERSION,
    SealedAttemptReceiptStore, SealedAttemptReceiptV1, TokenUsage, advice_may_mutate_authority,
    apply_kairos_command, authorize_in_process_retry_budget, build_usage_receipt,
    consult_timed_out, issue_shadow_advice, ordinary_turn_max_retries,
    ordinary_turn_max_retries_with_authority,
};

/// Session-local shadow advisor host: records advice, never mutates authority.
///
/// `live_policy_epoch` is bumped when catalog/health/pool policy changes.
/// Each `issue` stamps `last_advice_policy_epoch`; if live advances past that
/// stamp the advice is **stale** and P4b admission must deny (S8).
///
/// DEBT-007: the *production* epoch write side lives on `ToolContext` atomics
/// (stamped at the failure-convergence checkpoint in `run_turn_via_sampler`);
/// this host keeps its own epoch fields for offline host tests only. Do not
/// write production policy changes through this host — use
/// `ToolContext::bump_live_policy_epoch` / `record_advice_issued`.
#[derive(Debug, Clone)]
pub struct ShadowAdvisorHost {
    pub mode: AdvisorMode,
    pub reports: Vec<AdviceReportV1>,
    /// Live policy/catalog/health epoch (starts at 1).
    pub live_policy_epoch: u64,
    /// Epoch at which the last advice was issued. `None` = no advice on file.
    pub last_advice_policy_epoch: Option<u64>,
}

impl Default for ShadowAdvisorHost {
    fn default() -> Self {
        Self::new(AdvisorMode::Shadow)
    }
}

impl ShadowAdvisorHost {
    pub fn new(mode: AdvisorMode) -> Self {
        Self {
            mode,
            reports: Vec::new(),
            live_policy_epoch: 1,
            last_advice_policy_epoch: None,
        }
    }

    /// Issue and retain shadow advice. Always `applies_authority == false`.
    pub fn issue(
        &mut self,
        advice_id: impl Into<String>,
        summary: impl Into<String>,
        recommended_next_step: Option<String>,
        usage_receipt_ref: Option<String>,
    ) -> Result<&AdviceReportV1, AdvisorDeny> {
        let report = issue_shadow_advice(
            self.mode,
            advice_id,
            summary,
            recommended_next_step,
            usage_receipt_ref,
        )?;
        debug_assert!(!advice_may_mutate_authority(&report));
        self.reports.push(report);
        self.last_advice_policy_epoch = Some(self.live_policy_epoch);
        Ok(self.reports.last().expect("just pushed"))
    }

    pub fn last(&self) -> Option<&AdviceReportV1> {
        self.reports.last()
    }

    /// Advance live policy epoch (pool/health/model policy change).
    pub fn bump_policy_epoch(&mut self) {
        self.live_policy_epoch = self.live_policy_epoch.saturating_add(1);
    }

    /// True when advice exists and was issued under an older policy epoch.
    pub fn has_stale_advice(&self) -> bool {
        advice_is_stale(self.last_advice_policy_epoch, self.live_policy_epoch)
    }
}

/// Advice is stale when it was issued under an older policy epoch than live.
///
/// No advice (`None`) is **not** stale — only an issued-then-superseded
/// stamp fails closed.
pub fn advice_is_stale(issued_policy_epoch: Option<u64>, live_policy_epoch: u64) -> bool {
    match issued_policy_epoch {
        Some(issued) if issued < live_policy_epoch => true,
        _ => false,
    }
}

/// Live P4b side conditions derived from session observations (S8).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct P4bLiveSideConditions {
    pub pool_exhausted: bool,
    pub breaker_open: bool,
    pub stale_advice: bool,
}

/// Derive P4b side conditions from live observations.
///
/// - `pool_exhausted`: routing enabled, non-empty user pool, zero healthy
///   catalog-backed candidates remain.
/// - `breaker_open`: active provider endpoint is passively `Degraded`
///   (domain health ledger acts as the circuit for ordinary turns).
/// - `stale_advice`: see [`advice_is_stale`].
pub fn derive_p4b_side_conditions(
    routing_enabled: bool,
    pool_len: usize,
    any_healthy_in_pool: bool,
    provider_degraded: bool,
    advice_issued_policy_epoch: Option<u64>,
    live_policy_epoch: u64,
) -> P4bLiveSideConditions {
    let pool_exhausted = routing_enabled && pool_len > 0 && !any_healthy_in_pool;
    P4bLiveSideConditions {
        pool_exhausted,
        breaker_open: provider_degraded,
        stale_advice: advice_is_stale(advice_issued_policy_epoch, live_policy_epoch),
    }
}

/// Outcome of the S8 shell auth-class same-turn retry decision table.
///
/// Extracted from `run_turn_via_sampler` so the production loop body and unit
/// tests share one fail-closed matrix (Critic M2). Full SamplerActor mock is
/// not required to prove admission + budget + refresh gating.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AuthClassRetryAction {
    /// Perform one same-turn resubmit with a fresh request id.
    Resubmit { next_used: u32 },
    /// Fall through to terminal `handle_sampling_failure`.
    Terminal { reason: &'static str },
}

/// Pure decision for auth-class same-turn resubmit (S8).
///
/// Call order in production: seal → admit → refresh → this table.
/// Non-auth failures never resubmit. Admission deny reasons surface as
/// terminal reasons (stable codes from [`RetryDenyReason::code`]).
pub fn decide_auth_class_retry(
    is_auth_failure: bool,
    admission: Result<u32, RetryDenyReason>,
    refresh_succeeded: bool,
    auth_class_retries_used: u32,
) -> AuthClassRetryAction {
    if !is_auth_failure {
        return AuthClassRetryAction::Terminal {
            reason: "non_auth_failure",
        };
    }
    match admission {
        Err(deny) => AuthClassRetryAction::Terminal {
            reason: deny.code(),
        },
        // Defensive dead branch (DEBT-010): `authorize_in_process_retry_budget`
        // only returns Ok(n) with n >= 1 or Err(BudgetExhausted); Ok(0) is
        // unreachable by construction, kept for total-match coverage.
        Ok(0) => AuthClassRetryAction::Terminal {
            reason: "retry.budget_exhausted",
        },
        Ok(_) if !refresh_succeeded => AuthClassRetryAction::Terminal {
            reason: "auth_refresh_failed",
        },
        Ok(_) => AuthClassRetryAction::Resubmit {
            next_used: auth_class_retries_used.saturating_add(1),
        },
    }
}

/// Session-local ClientAdvisor consult host (S9 / NG-06A).
///
/// Modes (plan §3.4.3):
/// - `Off`: consult refuses with `PolicyRefused`; no report, no receipt, no
///   provider attempt.
/// - `Shadow`: records the report and an independent usage receipt with
///   `mode=Shadow` and **no provider attempt** (used for corpus evaluation).
/// - `Consult`: runs the (fixture-only) adapter; timeout / unavailable is
///   `Blocked`, never a downgraded success.
///
/// Advice can never mutate authority (`applies_authority` is forced false),
/// and every consult gets its own usage receipt (never the model receipt).
/// The host can persist reports + receipts as JSON (shadow marker on disk).
#[derive(Debug, Clone)]
pub struct ConsultAdvisorHost {
    pub mode: AdvisorMode,
    pub reports: Vec<AdviceReportV1>,
    pub receipts: Vec<AdvisorUsageReceiptV1>,
    /// Where `persist()` writes the JSON snapshot; `None` disables disk IO.
    pub persist_path: Option<std::path::PathBuf>,
}

impl Default for ConsultAdvisorHost {
    fn default() -> Self {
        Self::new(AdvisorMode::Off)
    }
}

impl ConsultAdvisorHost {
    pub fn new(mode: AdvisorMode) -> Self {
        Self {
            mode,
            reports: Vec::new(),
            receipts: Vec::new(),
            persist_path: None,
        }
    }

    /// True when a provider call would be attempted for this mode.
    pub fn mode_calls_provider(&self) -> bool {
        matches!(self.mode, AdvisorMode::Consult)
    }

    /// Run one consult through the mode gate and (consult-only) adapter.
    ///
    /// Returns the outcome and, on success, the report id. `Blocked` reasons
    /// are observable; a timeout/unavailable advisor never downgrades to a
    /// success and never reopens the primary task.
    pub fn run_consult(
        &mut self,
        request: &AdvisorRequestV1,
        capsule: &AdvisorContextCapsuleV1,
        adapter: &MockAdvisorAdapter,
        now_epoch_ms: u64,
        deadline_epoch_ms: Option<u64>,
    ) -> ConsultOutcome {
        match self.mode {
            AdvisorMode::Off => ConsultOutcome::Blocked {
                reason: ConsultBlockReason::PolicyRefused,
            },
            AdvisorMode::Shadow => {
                // Record "would consult" shadow report; no provider attempt.
                let report = issue_shadow_advice(
                    AdvisorMode::Shadow,
                    &request.request_id,
                    capsule.manifest_summary.clone(),
                    request.review_question.clone(),
                    Some(format!("usage://{}", request.request_id)),
                );
                match report {
                    Ok(r) => {
                        let id = r.advice_id.clone();
                        self.reports.push(r);
                        self.receipts.push(build_usage_receipt(
                            format!("u-{}", request.request_id),
                            &request.request_id,
                            now_epoch_ms,
                            AdvisorMode::Shadow,
                            None,
                            capsule.capsule_hash.clone(),
                            TokenUsage::Unknown,
                            deadline_epoch_ms,
                            None,
                            false,
                        ));
                        ConsultOutcome::Succeeded { report_id: id }
                    }
                    Err(deny) => ConsultOutcome::Blocked {
                        reason: ConsultBlockReason::Denied(deny),
                    },
                }
            }
            AdvisorMode::Consult => {
                if let Some(deadline) = deadline_epoch_ms
                    && consult_timed_out(now_epoch_ms, deadline, now_epoch_ms)
                {
                    self.receipts.push(build_usage_receipt(
                        format!("u-{}", request.request_id),
                        &request.request_id,
                        now_epoch_ms,
                        AdvisorMode::Consult,
                        None,
                        capsule.capsule_hash.clone(),
                        TokenUsage::Unknown,
                        Some(deadline),
                        Some(ConsultBlockReason::TimedOut.code().into()),
                        false,
                    ));
                    return ConsultOutcome::Blocked {
                        reason: ConsultBlockReason::TimedOut,
                    };
                }
                match adapter.run(request, capsule) {
                    Ok(report) => {
                        let id = report.advice_id.clone();
                        self.reports.push(report);
                        self.receipts.push(build_usage_receipt(
                            format!("u-{}", request.request_id),
                            &request.request_id,
                            now_epoch_ms,
                            AdvisorMode::Consult,
                            adapter.model_ref.clone(),
                            capsule.capsule_hash.clone(),
                            adapter.token_usage,
                            deadline_epoch_ms,
                            None,
                            false,
                        ));
                        ConsultOutcome::Succeeded { report_id: id }
                    }
                    Err(reason) => {
                        self.receipts.push(build_usage_receipt(
                            format!("u-{}", request.request_id),
                            &request.request_id,
                            now_epoch_ms,
                            AdvisorMode::Consult,
                            adapter.model_ref.clone(),
                            capsule.capsule_hash.clone(),
                            TokenUsage::Unknown,
                            deadline_epoch_ms,
                            Some(reason.code().into()),
                            false,
                        ));
                        ConsultOutcome::Blocked { reason }
                    }
                }
            }
        }
    }

    /// Persist reports + receipts as a JSON snapshot (shadow marker on disk).
    /// `persist_path` must be set; returns the path on success.
    pub fn persist(&self) -> Result<std::path::PathBuf, std::io::Error> {
        let Some(path) = &self.persist_path else {
            return Err(std::io::Error::new(
                std::io::ErrorKind::NotFound,
                "consult host persist_path is not set",
            ));
        };
        let payload = serde_json::json!({
            "schema": "lumen.advisor.consult_host.v1",
            "mode": serde_json::to_value(&self.mode).unwrap_or_default(),
            "reports": self.reports,
            "receipts": self.receipts,
            "shadow_marker": !self.mode_calls_provider(),
        });
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        std::fs::write(path, serde_json::to_string_pretty(&payload)?)?;
        Ok(path.clone())
    }
}

/// Fixture-only adapter for consult runs. **Never used for billable calls**;
/// the real provider adapter is wired by the product path behind the same
/// `ConsultBlockReason` surface.
#[derive(Debug, Clone)]
pub struct MockAdvisorAdapter {
    pub model_ref: Option<String>,
    pub token_usage: TokenUsage,
    pub respond: bool,
    pub timed_out: bool,
}

impl MockAdvisorAdapter {
    pub fn run(
        &self,
        request: &AdvisorRequestV1,
        capsule: &AdvisorContextCapsuleV1,
    ) -> Result<AdviceReportV1, ConsultBlockReason> {
        if self.timed_out {
            return Err(ConsultBlockReason::TimedOut);
        }
        if !self.respond {
            return Err(ConsultBlockReason::AdvisorUnavailable);
        }
        let mut report = issue_shadow_advice(
            AdvisorMode::Consult,
            &request.request_id,
            capsule.manifest_summary.clone(),
            request.review_question.clone(),
            Some(format!("usage://{}", request.request_id)),
        )
        .map_err(ConsultBlockReason::Denied)?;
        report.mode = AdvisorMode::Consult;
        Ok(report)
    }
}

/// Session-local Kairos operator surface (pure state machine only).
#[derive(Debug, Clone)]
pub struct KairosControlHost {
    pub state: KairosSupervisorState,
}

impl KairosControlHost {
    pub fn new(id: impl Into<String>) -> Self {
        Self {
            state: KairosSupervisorState::new(id),
        }
    }

    pub fn phase(&self) -> LoopPhase {
        self.state.phase()
    }

    pub fn apply(
        &mut self,
        cmd: KairosCommand,
        tree_id: Option<&str>,
        lease_epoch: Option<u64>,
    ) -> Result<LoopPhase, KairosDeny> {
        let (next, _effect) = apply_kairos_command(self.state.clone(), cmd, tree_id, lease_epoch)?;
        self.state = next;
        Ok(self.state.phase())
    }
}

/// Resolve ordinary-turn sampler max retries from an optional in-memory seal.
///
/// Without durable confirmation this is always 0 (INV-11 / P0-NR-A). Prefer
/// [`ordinary_sampler_max_retries_with_authority`] once a store has confirmed
/// a clean seal for the attempt.
pub fn ordinary_sampler_max_retries(receipt: Option<&SealedAttemptReceiptV1>) -> u32 {
    ordinary_turn_max_retries(receipt)
}

/// Ordinary-turn budget with durable seal authority (S8).
pub fn ordinary_sampler_max_retries_with_authority(
    receipt: Option<&SealedAttemptReceiptV1>,
    authority: DurableSealAuthority,
) -> u32 {
    ordinary_turn_max_retries_with_authority(receipt, authority)
}

/// P4b admission for a bounded same-turn in-process retry (auth-refresh class).
///
/// `actor_policy_max_retries` is the GROK_MAX_RETRIES / config ceiling and may
/// only lower the seal budget — never reopen a closed safety gate.
pub fn authorize_ordinary_retry_budget(
    req: &RetryAdmissionRequest<'_>,
) -> Result<u32, RetryDenyReason> {
    authorize_in_process_retry_budget(req)
}

/// Build a default admission request for ordinary turns (tests + shell).
pub fn ordinary_retry_admission<'a>(
    receipt: Option<&'a SealedAttemptReceiptV1>,
    durable_authority: DurableSealAuthority,
    model_pinned: bool,
    pool_exhausted: bool,
    breaker_open: bool,
    stale_advice: bool,
    actor_policy_max_retries: u32,
    already_used_retries: u32,
) -> RetryAdmissionRequest<'a> {
    RetryAdmissionRequest {
        receipt,
        durable_authority,
        schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
        expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
        model_pinned,
        pool_exhausted,
        breaker_open,
        stale_advice,
        actor_policy_max_retries,
        already_used_retries,
    }
}

/// Persist a sealed receipt and resolve durable authority for it.
pub fn seal_and_authority(
    store: &SealedAttemptReceiptStore,
    receipt: SealedAttemptReceiptV1,
    turn_id: Option<String>,
    session_id: Option<String>,
) -> (SealedAttemptReceiptV1, DurableSealAuthority) {
    match store.record(receipt.clone(), turn_id, session_id) {
        Ok(_) => {
            let authority = store.authority_for(&receipt);
            (receipt, authority)
        }
        Err(_) => (receipt, DurableSealAuthority::Untrusted),
    }
}

/// Escalation guidance appended to terminal sampling failures.
///
/// The first failure keeps the transient-blip framing ("resubmit"). Repeated
/// failures must not read as "stuck": auth failures point at re-login +
/// `/model`, provider failures point at `/model` or retry later, and a
/// user-pinned model is called out explicitly because routing deliberately
/// never overrides a pin (P0-NR-A / S11).
pub fn failure_escalation_guidance(
    consecutive_failures: u32,
    is_auth_failure: bool,
    status_code: Option<u16>,
    model_pinned: bool,
    provider_degraded: bool,
) -> Option<String> {
    if consecutive_failures < 2 {
        return None;
    }
    let mut lines: Vec<String> = Vec::new();
    if is_auth_failure {
        let code = status_code
            .map(|c| c.to_string())
            .unwrap_or_else(|| "401".to_string());
        lines.push(format!(
            "Repeated auth failure ({} x{consecutive_failures}): this is no longer a transient blip; the credential may be invalid.",
            code
        ));
        lines.push(
            "Re-authenticate (grok login or /login) and retry, or switch the model with /model."
                .to_string(),
        );
    } else if provider_degraded {
        lines.push(format!(
            "Repeated failure (x{consecutive_failures}): the provider may be unavailable; health was recorded."
        ));
        lines.push("Switch the model with /model, or retry later.".to_string());
    } else {
        lines.push(format!("Repeated failure (x{consecutive_failures})."));
        lines.push("Switch the model with /model, or retry later.".to_string());
    }
    if model_pinned {
        lines.push(
            "You manually selected this model, so automatic rerouting is disabled to respect your choice; /model switches immediately."
                .to_string(),
        );
    }
    Some(lines.join("\n"))
}

/// Build a fresh attempt seal for a turn id (preflight clean).
pub fn begin_attempt_seal(attempt_id: impl Into<String>) -> AttemptSealTracker {
    AttemptSealTracker::new(attempt_id)
}

#[cfg(test)]
mod tests {
    use super::*;
    use xai_grok_memory::{
        DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES, clean_preflight_receipt, mark_output_emitted,
        may_in_process_retry,
    };

    #[test]
    fn shadow_host_never_grants_authority() {
        let mut host = ShadowAdvisorHost::new(AdvisorMode::Shadow);
        let report = host
            .issue("a1", "run the unit tests before claiming done", None, None)
            .unwrap();
        assert!(!report.applies_authority);
        assert!(!advice_may_mutate_authority(report));
        assert_eq!(host.reports.len(), 1);
    }

    fn consult_request(id: &str) -> AdvisorRequestV1 {
        AdvisorRequestV1 {
            request_id: id.to_string(),
            kind: xai_grok_memory::AdvisorRequestKind::FailureConvergenceReview,
            review_question: Some("is the failure converging?".to_string()),
            artifact_refs: vec!["artifacts/r0/ok.md".to_string()],
        }
    }

    fn consult_capsule(request: &AdvisorRequestV1) -> AdvisorContextCapsuleV1 {
        xai_grok_memory::build_advisor_capsule(
            &request.request_id,
            request.kind,
            "manifest redacted ok",
            "snapshot ok",
            request.review_question.as_deref(),
            &request.artifact_refs,
            &["artifacts/"],
        )
        .expect("capsule builds")
    }

    #[test]
    fn consult_off_refuses_without_provider_attempt() {
        let mut host = ConsultAdvisorHost::new(AdvisorMode::Off);
        let req = consult_request("c1");
        let cap = consult_capsule(&req);
        let adapter = MockAdvisorAdapter {
            model_ref: Some("deepseek-v4-flash".into()),
            token_usage: TokenUsage::Unknown,
            respond: true,
            timed_out: false,
        };
        let outcome = host.run_consult(&req, &cap, &adapter, 100, Some(200));
        assert_eq!(
            outcome,
            ConsultOutcome::Blocked {
                reason: ConsultBlockReason::PolicyRefused
            }
        );
        assert!(host.reports.is_empty(), "off mode records nothing");
        assert!(host.receipts.is_empty(), "off mode records no receipt");
        assert!(!host.mode_calls_provider());
    }

    #[test]
    fn consult_shadow_records_report_and_receipt_without_provider() {
        let mut host = ConsultAdvisorHost::new(AdvisorMode::Shadow);
        let req = consult_request("c2");
        let cap = consult_capsule(&req);
        let adapter = MockAdvisorAdapter {
            model_ref: Some("deepseek-v4-flash".into()),
            token_usage: TokenUsage::Unknown,
            respond: true,
            timed_out: false,
        };
        let outcome = host.run_consult(&req, &cap, &adapter, 100, Some(200));
        assert!(matches!(outcome, ConsultOutcome::Succeeded { .. }));
        assert_eq!(host.reports.len(), 1);
        assert!(!host.reports[0].applies_authority, "shadow advice has no authority");
        assert_eq!(host.receipts.len(), 1);
        assert_eq!(host.receipts[0].mode, AdvisorMode::Shadow);
        assert!(!host.mode_calls_provider(), "shadow must not call a provider");
    }

    #[test]
    fn consult_timeout_blocks_and_receipt_records_reason() {
        let mut host = ConsultAdvisorHost::new(AdvisorMode::Consult);
        let req = consult_request("c3");
        let cap = consult_capsule(&req);
        // Deadline already passed → timed out before any adapter run.
        let adapter = MockAdvisorAdapter {
            model_ref: None,
            token_usage: TokenUsage::Unknown,
            respond: true,
            timed_out: true,
        };
        let outcome = host.run_consult(&req, &cap, &adapter, 500, Some(400));
        assert!(matches!(
            outcome,
            ConsultOutcome::Blocked {
                reason: ConsultBlockReason::TimedOut
            }
        ));
        assert!(host.reports.is_empty(), "blocked consult has no report");
        assert_eq!(host.receipts.len(), 1);
        assert_eq!(
            host.receipts[0].cancel_or_deny_reason.as_deref(),
            Some("advisor.timed_out")
        );
    }

    #[test]
    fn consult_unavailable_blocks_and_never_downgrades() {
        let mut host = ConsultAdvisorHost::new(AdvisorMode::Consult);
        let req = consult_request("c4");
        let cap = consult_capsule(&req);
        let adapter = MockAdvisorAdapter {
            model_ref: None,
            token_usage: TokenUsage::Unknown,
            respond: false,
            timed_out: false,
        };
        let outcome = host.run_consult(&req, &cap, &adapter, 100, Some(500));
        assert!(matches!(
            outcome,
            ConsultOutcome::Blocked {
                reason: ConsultBlockReason::AdvisorUnavailable
            }
        ));
        assert!(host.reports.is_empty());
        assert_eq!(
            host.receipts[0].cancel_or_deny_reason.as_deref(),
            Some("advisor.unavailable")
        );
    }

    #[test]
    fn consult_success_has_independent_usage_receipt() {
        let mut host = ConsultAdvisorHost::new(AdvisorMode::Consult);
        let req = consult_request("c5");
        let cap = consult_capsule(&req);
        let adapter = MockAdvisorAdapter {
            model_ref: Some("deepseek-v4-flash".into()),
            token_usage: TokenUsage::Known {
                input_tokens: 42,
                output_tokens: 7,
            },
            respond: true,
            timed_out: false,
        };
        let outcome = host.run_consult(&req, &cap, &adapter, 100, Some(500));
        assert!(matches!(outcome, ConsultOutcome::Succeeded { .. }));
        assert_eq!(host.reports.len(), 1);
        assert!(!host.reports[0].applies_authority);
        assert_eq!(host.receipts.len(), 1);
        // Independent receipt: request-scoped id, consult mode, adapter usage.
        assert_eq!(host.receipts[0].request_id, "c5");
        assert_eq!(host.receipts[0].mode, AdvisorMode::Consult);
        assert_eq!(
            host.receipts[0].token_usage,
            TokenUsage::Known {
                input_tokens: 42,
                output_tokens: 7
            }
        );
        assert!(host.receipts[0].cancel_or_deny_reason.is_none());
    }

    #[test]
    fn consult_persist_writes_shadow_marker_json() {
        let mut host = ConsultAdvisorHost::new(AdvisorMode::Shadow);
        let req = consult_request("c6");
        let cap = consult_capsule(&req);
        let adapter = MockAdvisorAdapter {
            model_ref: None,
            token_usage: TokenUsage::Unknown,
            respond: true,
            timed_out: false,
        };
        host.run_consult(&req, &cap, &adapter, 100, None);
        let dir = std::env::temp_dir().join(format!("consult-persist-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("advisor.json");
        host.persist_path = Some(path.clone());
        let written = host.persist().expect("persist succeeds");
        assert_eq!(written, path);
        let raw = std::fs::read_to_string(&path).expect("file readable");
        let value: serde_json::Value = serde_json::from_str(&raw).expect("valid json");
        assert_eq!(value["shadow_marker"], true);
        assert_eq!(value["reports"].as_array().map(|a| a.len()), Some(1));
        assert_eq!(value["receipts"].as_array().map(|a| a.len()), Some(1));
        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn stale_advice_epoch_write_side_triggers_p4b_deny() {
        use std::sync::atomic::{AtomicU64, Ordering};
        // Real epoch semantics through derive_p4b_side_conditions: advice
        // issued at epoch 1, live bumped to 2 → stale_advice=true (deny path).
        let live = AtomicU64::new(1);
        let issued = AtomicU64::new(0);
        // (simulated write side of ToolContext::record_advice_issued)
        issued.store(live.load(Ordering::SeqCst), Ordering::SeqCst);
        let side = derive_p4b_side_conditions(
            true,
            1,
            true,
            false,
            (issued.load(Ordering::SeqCst) != 0).then_some(issued.load(Ordering::SeqCst)),
            live.load(Ordering::SeqCst),
        );
        assert!(!side.stale_advice, "fresh advice is not stale");
        // (simulated write side of ToolContext::bump_live_policy_epoch)
        live.fetch_add(1, Ordering::SeqCst);
        let side = derive_p4b_side_conditions(
            true,
            1,
            true,
            false,
            (issued.load(Ordering::SeqCst) != 0).then_some(issued.load(Ordering::SeqCst)),
            live.load(Ordering::SeqCst),
        );
        assert!(side.stale_advice, "bumped live epoch makes advice stale");
        // The full production flow (ToolContext methods) is covered by
        // tool_context::tests::advice_epoch_write_side_flows_into_stale_detection.
    }

    #[test]
    fn kairos_host_claim_freeze_takeover() {
        let mut host = KairosControlHost::new("k1");
        assert_eq!(
            host.apply(KairosCommand::Claim, Some("tree"), Some(1))
                .unwrap(),
            LoopPhase::Running
        );
        assert_eq!(
            host.apply(KairosCommand::Freeze, None, None).unwrap(),
            LoopPhase::Frozen
        );
        assert_eq!(
            host.apply(KairosCommand::TakeOver, Some("tree"), Some(2))
                .unwrap(),
            LoopPhase::Running
        );
    }

    #[test]
    fn ordinary_sampler_budget_is_zero_without_durable_store() {
        // Without durable confirmation, clean in-memory seals stay at 0
        // (P0-NR-A baseline). S8 only opens budget via ConfirmedClean.
        assert_eq!(ordinary_sampler_max_retries(None), 0);
        let mut seal = begin_attempt_seal("turn-1");
        assert!(seal.may_retry().is_ok());
        assert_eq!(ordinary_sampler_max_retries(Some(seal.receipt())), 0);
        seal.mark_output();
        assert!(may_in_process_retry(seal.receipt()).is_err());
        assert_eq!(
            ordinary_sampler_max_retries(Some(&mark_output_emitted(clean_preflight_receipt(
                "x"
            )))),
            0
        );
    }

    #[test]
    fn ordinary_sampler_budget_opens_for_durable_clean_seal_only() {
        let clean = clean_preflight_receipt("s8-clean");
        assert_eq!(
            ordinary_sampler_max_retries_with_authority(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean
            ),
            DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES
        );
        let store = SealedAttemptReceiptStore::in_memory();
        let (sealed, authority) = seal_and_authority(&store, clean.clone(), None, None);
        assert_eq!(authority, DurableSealAuthority::ConfirmedClean);
        assert_eq!(
            ordinary_sampler_max_retries_with_authority(Some(&sealed), authority),
            1
        );
    }

    #[test]
    fn p4b_admission_denies_all_required_reject_paths() {
        let clean = clean_preflight_receipt("p4b");
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean,
                true,
                false,
                false,
                false,
                15,
                0,
            ))
            .unwrap_err(),
            RetryDenyReason::ModelPinned,
            "pin bypass must deny"
        );
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean,
                false,
                true,
                false,
                false,
                15,
                0,
            ))
            .unwrap_err(),
            RetryDenyReason::PoolExhausted,
            "pool exhausted must deny"
        );
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean,
                false,
                false,
                true,
                false,
                15,
                0,
            ))
            .unwrap_err(),
            RetryDenyReason::BreakerOpen,
            "breaker open must deny"
        );
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean,
                false,
                false,
                false,
                true,
                15,
                0,
            ))
            .unwrap_err(),
            RetryDenyReason::StaleAdvice,
            "stale advice must deny"
        );
        let dirty = mark_output_emitted(clean_preflight_receipt("out"));
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&dirty),
                DurableSealAuthority::ConfirmedClean,
                false,
                false,
                false,
                false,
                15,
                0,
            ))
            .unwrap_err()
            .code(),
            "retry.output_emitted",
            "existing output must deny"
        );
        let mut schema = ordinary_retry_admission(
            Some(&clean),
            DurableSealAuthority::ConfirmedClean,
            false,
            false,
            false,
            false,
            15,
            0,
        );
        schema.schema_version = 0;
        assert_eq!(
            authorize_ordinary_retry_budget(&schema).unwrap_err(),
            RetryDenyReason::SchemaMismatch,
            "schema mismatch must deny"
        );
    }

    #[test]
    fn grok_max_retries_cannot_raise_seal_budget_via_shell_admission() {
        let clean = clean_preflight_receipt("env-cap");
        // Actor policy 15 (GROK_MAX_RETRIES territory) still caps at seal budget.
        let remaining = authorize_ordinary_retry_budget(&ordinary_retry_admission(
            Some(&clean),
            DurableSealAuthority::ConfirmedClean,
            false,
            false,
            false,
            false,
            15,
            0,
        ))
        .unwrap();
        assert_eq!(remaining, 1);
        // Actor policy 0 forces closed even with clean durable seal.
        assert!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean,
                false,
                false,
                false,
                false,
                0,
                0,
            ))
            .is_err()
        );
        // Dirty seal: actor 15 cannot reopen.
        let dirty = mark_output_emitted(clean_preflight_receipt("env-dirty"));
        assert!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&dirty),
                DurableSealAuthority::ConfirmedClean,
                false,
                false,
                false,
                false,
                15,
                0,
            ))
            .is_err()
        );
    }

    #[test]
    fn clean_durable_auth_class_retry_is_bounded_positive_path() {
        let store = SealedAttemptReceiptStore::in_memory();
        let mut tracker = begin_attempt_seal("auth-clean");
        tracker.apply_failure_observations(false, false, true);
        assert!(tracker.may_retry().is_ok());
        let (receipt, authority) =
            seal_and_authority(&store, tracker.receipt().clone(), None, None);
        assert_eq!(authority, DurableSealAuthority::ConfirmedClean);
        let remaining = authorize_ordinary_retry_budget(&ordinary_retry_admission(
            Some(&receipt),
            authority,
            false,
            false,
            false,
            false,
            15,
            0,
        ))
        .expect("clean durable seal must authorize one auth-class retry");
        assert_eq!(remaining, 1);
        // Second attempt consumes budget.
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&receipt),
                authority,
                false,
                false,
                false,
                false,
                15,
                1,
            ))
            .unwrap_err(),
            RetryDenyReason::BudgetExhausted
        );
    }

    #[test]
    fn escalation_first_failure_stays_transient() {
        assert_eq!(
            failure_escalation_guidance(0, false, None, false, false),
            None
        );
        assert_eq!(
            failure_escalation_guidance(1, true, Some(401), false, false),
            None,
            "first failure keeps the transient-blip framing"
        );
    }

    #[test]
    fn escalation_repeated_auth_guides_relogin_and_model_switch() {
        let guidance = failure_escalation_guidance(2, true, Some(401), false, false)
            .expect("second auth failure must escalate");
        assert!(guidance.contains("401"), "guidance: {guidance}");
        assert!(guidance.contains("grok login"), "guidance: {guidance}");
        assert!(guidance.contains("/model"), "guidance: {guidance}");
        assert!(!guidance.contains("manually selected"));
    }

    #[test]
    fn escalation_pinned_model_is_called_out() {
        let guidance = failure_escalation_guidance(3, true, Some(401), true, false)
            .expect("must escalate");
        assert!(
            guidance.contains("manually selected"),
            "pin must be visible: {guidance}"
        );
        assert!(guidance.contains("/model switches immediately"));
    }

    #[test]
    fn escalation_provider_failure_mentions_health() {
        let guidance = failure_escalation_guidance(2, false, Some(503), false, true)
            .expect("must escalate");
        assert!(guidance.contains("provider"), "guidance: {guidance}");
        assert!(guidance.contains("health was recorded"), "guidance: {guidance}");
        assert!(!guidance.contains("credential"));
    }

    #[test]
    fn derive_p4b_side_conditions_from_live_observations() {
        // Healthy routing pool → no denies.
        let ok = derive_p4b_side_conditions(true, 2, true, false, None, 1);
        assert!(!ok.pool_exhausted);
        assert!(!ok.breaker_open);
        assert!(!ok.stale_advice);

        // Enabled non-empty pool, zero healthy → pool exhausted.
        let pool = derive_p4b_side_conditions(true, 2, false, false, None, 1);
        assert!(pool.pool_exhausted);
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean_preflight_receipt("p")),
                DurableSealAuthority::ConfirmedClean,
                false,
                pool.pool_exhausted,
                pool.breaker_open,
                pool.stale_advice,
                1,
                0,
            ))
            .unwrap_err(),
            RetryDenyReason::PoolExhausted
        );

        // Provider degraded → breaker open.
        let br = derive_p4b_side_conditions(false, 0, true, true, None, 1);
        assert!(br.breaker_open);
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean_preflight_receipt("b")),
                DurableSealAuthority::ConfirmedClean,
                false,
                br.pool_exhausted,
                br.breaker_open,
                br.stale_advice,
                1,
                0,
            ))
            .unwrap_err(),
            RetryDenyReason::BreakerOpen
        );

        // Advice issued at epoch 1, live advanced to 2 → stale.
        let stale = derive_p4b_side_conditions(false, 0, true, false, Some(1), 2);
        assert!(stale.stale_advice);
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&clean_preflight_receipt("s")),
                DurableSealAuthority::ConfirmedClean,
                false,
                stale.pool_exhausted,
                stale.breaker_open,
                stale.stale_advice,
                1,
                0,
            ))
            .unwrap_err(),
            RetryDenyReason::StaleAdvice
        );

        // Routing disabled / empty pool never counts as exhausted.
        let no_pool = derive_p4b_side_conditions(false, 0, false, false, None, 1);
        assert!(!no_pool.pool_exhausted);
        let empty = derive_p4b_side_conditions(true, 0, false, false, None, 1);
        assert!(!empty.pool_exhausted);
    }

    #[test]
    fn shadow_advisor_epoch_detects_stale_advice() {
        let mut host = ShadowAdvisorHost::new(AdvisorMode::Shadow);
        assert!(!host.has_stale_advice());
        host.issue("a1", "consider running tests", None, None)
            .unwrap();
        assert!(!host.has_stale_advice(), "fresh advice is not stale");
        host.bump_policy_epoch();
        assert!(host.has_stale_advice(), "policy bump must stale prior advice");
        assert!(advice_is_stale(Some(1), 2));
        assert!(!advice_is_stale(None, 99));
        assert!(!advice_is_stale(Some(5), 5));
    }

    #[test]
    fn auth_class_retry_decision_matrix_covers_required_paths() {
        let clean = clean_preflight_receipt("dec");
        let admit_ok = authorize_ordinary_retry_budget(&ordinary_retry_admission(
            Some(&clean),
            DurableSealAuthority::ConfirmedClean,
            false,
            false,
            false,
            false,
            1,
            0,
        ));
        assert!(admit_ok.is_ok());

        // (a) 401 + clean seal + refresh ok → resubmit once
        assert_eq!(
            decide_auth_class_retry(true, admit_ok.clone(), true, 0),
            AuthClassRetryAction::Resubmit { next_used: 1 }
        );

        // (b) second 401 after budget used → terminal budget exhausted
        let admit_spent = authorize_ordinary_retry_budget(&ordinary_retry_admission(
            Some(&clean),
            DurableSealAuthority::ConfirmedClean,
            false,
            false,
            false,
            false,
            1,
            1,
        ));
        assert_eq!(
            decide_auth_class_retry(true, admit_spent, true, 1),
            AuthClassRetryAction::Terminal {
                reason: "retry.budget_exhausted"
            }
        );

        // (c) refresh failed → terminal, no resubmit
        assert_eq!(
            decide_auth_class_retry(true, Ok(1), false, 0),
            AuthClassRetryAction::Terminal {
                reason: "auth_refresh_failed"
            }
        );

        // (d) non-auth failure → terminal, never resubmit
        assert_eq!(
            decide_auth_class_retry(false, Ok(1), true, 0),
            AuthClassRetryAction::Terminal {
                reason: "non_auth_failure"
            }
        );

        // Admission deny reasons surface as terminal codes.
        assert_eq!(
            decide_auth_class_retry(true, Err(RetryDenyReason::ModelPinned), true, 0),
            AuthClassRetryAction::Terminal {
                reason: "retry.model_pinned"
            }
        );
        assert_eq!(
            decide_auth_class_retry(true, Err(RetryDenyReason::PoolExhausted), true, 0),
            AuthClassRetryAction::Terminal {
                reason: "retry.pool_exhausted"
            }
        );
        assert_eq!(
            decide_auth_class_retry(true, Err(RetryDenyReason::BreakerOpen), true, 0),
            AuthClassRetryAction::Terminal {
                reason: "retry.breaker_open"
            }
        );
        assert_eq!(
            decide_auth_class_retry(true, Err(RetryDenyReason::StaleAdvice), true, 0),
            AuthClassRetryAction::Terminal {
                reason: "retry.stale_advice"
            }
        );
    }

    #[test]
    fn mid_stream_tool_delta_forbids_in_process_retry() {
        // L3: tool signal seals dirty — same path as CapturePhase::ToolCall.
        let mut tracker = begin_attempt_seal("tool-delta");
        tracker.apply_failure_observations(false, true, true);
        assert_eq!(
            tracker.may_retry().unwrap_err(),
            RetryDenyReason::ToolCallEmitted
        );
        let store = SealedAttemptReceiptStore::in_memory();
        let (receipt, authority) =
            seal_and_authority(&store, tracker.receipt().clone(), None, None);
        // Dirty durable seal never ConfirmedClean → budget 0.
        assert_ne!(authority, DurableSealAuthority::ConfirmedClean);
        assert_eq!(
            authorize_ordinary_retry_budget(&ordinary_retry_admission(
                Some(&receipt),
                DurableSealAuthority::ConfirmedClean, // even if forced
                false,
                false,
                false,
                false,
                15,
                0,
            ))
            .unwrap_err(),
            RetryDenyReason::ToolCallEmitted
        );
        assert_eq!(
            decide_auth_class_retry(
                true,
                Err(RetryDenyReason::ToolCallEmitted),
                true,
                0
            ),
            AuthClassRetryAction::Terminal {
                reason: "retry.tool_call_emitted"
            }
        );
    }
}
