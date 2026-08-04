//! Shell runner adapter and spawn-context construction for [`MvpAgent`].
//! The shared coordinator actor lives in `xai-grok-tools`; this module plugs
//! its `!Send` local-session runner into `spawn_local`.
use super::*;
use crate::session::repo_changes::UploadMethod;
use xai_grok_tools::implementations::grok_build::task::{
    MergeApplyResult, MergeHandoffDenyReason, MergeReceiptV1, WriteScopeLease,
    evaluate_merge_handoff,
};
use xai_grok_memory::capability_grant::GrantCapabilityClass;
use xai_grok_memory::dispatch_permit::DispatchPermitV1;
use xai_tool_types::SubagentCapabilityMode;

/// A10 (NG-08): outbox exactly-once gate for child dispatch at the real
/// scheduler/daemon loop. A child whose successful terminal was already
/// delivered is never dispatched again (INV-31: replay consumes only already
/// recorded observations). Failed/cancelled terminals are NOT marked, so
/// legitimate retries and recovery re-dispatches stay open; the dedup only
/// blocks re-delivery of the same successful terminal event.
struct SchedulerLoopOutbox {
    delivered: std::sync::Mutex<std::collections::HashSet<String>>,
}

impl Default for SchedulerLoopOutbox {
    fn default() -> Self {
        Self {
            delivered: std::sync::Mutex::new(std::collections::HashSet::new()),
        }
    }
}

impl SchedulerLoopOutbox {
    /// True when this child id has no delivered terminal yet.
    fn may_dispatch(&self, child_id: &str) -> bool {
        let delivered = self
            .delivered
            .lock()
            .unwrap_or_else(|poison| poison.into_inner());
        let snapshot: Vec<String> = delivered.iter().cloned().collect();
        xai_grok_memory::kairos_lease_consumer::outbox_should_deliver(&snapshot, child_id)
    }

    fn mark_delivered(&self, child_id: &str) {
        self.delivered
            .lock()
            .unwrap_or_else(|poison| poison.into_inner())
            .insert(child_id.to_owned());
    }
}

struct ShellChildRunner {
    agent_ref: LocalRef<MvpAgent>,
    loop_outbox: Arc<SchedulerLoopOutbox>,
    /// DEBT-024(d): DispatchPermitV1 held by the spawn adapter, keyed by
    /// child id; verified again at completion (INV-12 re-check).
    spawn_permits: Arc<std::sync::Mutex<std::collections::HashMap<String, DispatchPermitV1>>>,
}

/// Map a requested capability mode to grant classes for the spawn permit
/// (monotonic ceiling: the sandbox still enforces the real boundary).
fn capability_mode_to_grant_classes(
    mode: Option<SubagentCapabilityMode>,
) -> Vec<GrantCapabilityClass> {
    match mode {
        None | Some(SubagentCapabilityMode::ReadOnly) => vec![GrantCapabilityClass::ReadOnly],
        Some(SubagentCapabilityMode::ReadWrite) => {
            vec![GrantCapabilityClass::ReadOnly, GrantCapabilityClass::ScopedWrite]
        }
        Some(SubagentCapabilityMode::Execute) | Some(SubagentCapabilityMode::All) => vec![
            GrantCapabilityClass::ReadOnly,
            GrantCapabilityClass::ScopedWrite,
            GrantCapabilityClass::SpawnChild,
        ],
    }
}

/// DEBT-024(d) core: mint the spawn-adapter permit from the request's real
/// identity (lineage), the capability ceiling, and the context hashes.
/// Pure — the caller supplies the manifest/snapshot hashes it holds.
fn mint_child_spawn_permit_core(
    request: &xai_grok_tools::implementations::grok_build::task::types::SubagentRequest,
    manifest_hash: &str,
    accepted_snapshot_hash: &str,
    now_unix: u64,
) -> Option<DispatchPermitV1> {
    use xai_grok_memory::capability_grant::{
        CapabilityGrantV1, GrantCapabilityClass, IssueGrantRequest,
    };
    use xai_grok_memory::dispatch_permit::mint_governed_spawn_permit;
    use xai_grok_memory::identity_envelope::issue_node_identity;
    if request.lineage.depth == 0 || request.lineage.root_session_id.is_empty() {
        return None; // root/ungoverned spawns carry no permit
    }
    let assignment_hash =
        format!("sha256:{}", blake3::hash(request.prompt.as_bytes()).to_hex());
    // The tools lineage path names ancestors only; the identity contract's
    // lineage_path is root..=node, so append the child itself.
    let mut lineage_path = request.lineage.lineage_path.clone();
    lineage_path.push(request.id.clone());
    let node = issue_node_identity(
        request.lineage.root_session_id.clone(),
        request.id.clone(),
        request.lineage.root_session_id.clone(),
        Some(request.lineage.immediate_parent_session_id.clone()),
        lineage_path,
        assignment_hash,
    )
    .ok()?;
    let capabilities = capability_mode_to_grant_classes(request.runtime_overrides.capability_mode);
    let grant = CapabilityGrantV1::issue(IssueGrantRequest {
        grant_id: format!("grant:{}", request.id),
        issuer_root_session_id: request.lineage.root_session_id.clone(),
        target_node_id: request.id.clone(),
        task_tree_id: request.lineage.root_session_id.clone(),
        capabilities,
        resource_scope_roots: request
            .cwd
            .clone()
            .map(|cwd| vec![cwd])
            .unwrap_or_default(),
        issued_at_unix: now_unix,
        ttl_secs: 24 * 60 * 60,
        reason: "governed child spawn".into(),
        approval_ref: format!("spawn:{}", request.id),
        revoke_token: format!("tok:{}", request.id),
        parent: None,
    })
    .ok()?;
    mint_governed_spawn_permit(
        &node,
        &grant,
        manifest_hash,
        accepted_snapshot_hash,
        &format!("spawn:{}", request.id),
        now_unix.saturating_add(24 * 60 * 60),
        now_unix,
    )
    .ok()
}

/// Agent-side wrapper: fetch the real context hashes the parent session holds
/// and mint the permit (fail-closed: absent manifest ⇒ no permit).
fn mint_child_spawn_permit(
    this: &MvpAgent,
    request: &xai_grok_tools::implementations::grok_build::task::types::SubagentRequest,
) -> Option<DispatchPermitV1> {
    let parent_sid = acp::SessionId::new(request.parent_session_id.clone());
    let manifest_hash = this
        .sessions
        .borrow()
        .get(&parent_sid)
        .and_then(|handle| handle.tool_context.task_tree_manifest_hash.clone());
    let manifest_hash = manifest_hash.unwrap_or_default();
    let snapshot_hash: String = this
        .sessions
        .borrow()
        .get(&parent_sid)
        .and_then(|handle| handle.tool_context.task_tree_memory_workspace_dir.clone())
        .and_then(|dir| {
            use xai_grok_memory::task_ledger::WorkingMemoryLedger;
            WorkingMemoryLedger::for_workspace_dir(
                dir,
                request.lineage.root_session_id.clone(),
            )
            .accepted_snapshot()
            .ok()
            .map(|snapshot| snapshot.accepted_set_hash)
        })
        .unwrap_or_default();
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    mint_child_spawn_permit_core(request, &manifest_hash, &snapshot_hash, now)
}

/// Select a model for a fresh root-owned scheduler iteration before the child
/// exists.  This deliberately reuses the user's ordinary-turn allowlist, but
/// has a narrower boundary: only the scheduler's internal loop request may
/// opt in.  A resume must retain its source model, an explicit override is a
/// pin, and nested children remain outside automatic routing until they have
/// their own retry/recovery contract.
fn scheduler_preflight_model(
    request: &xai_grok_tools::implementations::grok_build::task::types::SubagentRequest,
    parent_depth: u32,
    models_manager: &crate::agent::models::ModelsManager,
) -> Option<String> {
    if parent_depth > 0
        || request.resume_from.is_some()
        || request.runtime_overrides.model.is_some()
        || request.runtime_overrides.loop_task_id.is_none()
        || models_manager.user_selected_model()
    {
        return None;
    }
    let policy = models_manager.model_routing_config();
    if !policy.enabled || policy.model_pool.is_empty() {
        return None;
    }
    models_manager.select_healthy_model_for_task(
        &policy.model_pool,
        &policy.priority,
        &policy.task_preferences,
        &request.prompt,
    )
}

/// A6 (NG-04D-4 / INV-18): durable handoff delivery receipt for a child
/// terminal. The receipt is a [`HandoffPacketV1`] delivery appended to the
/// task-tree lifecycle journal, anchored under the same host-owned
/// task-tree memory root the coordinator uses for operation stores.
///
/// The snapshot reference is content-bound (hash of the child's final
/// output), so the receipt is re-derivable without storing the raw
/// transcript. Best-effort by design: a journaling failure is logged and
/// must never mask the completion itself.
fn journal_child_handoff(
    agent: &MvpAgent,
    completion: &xai_grok_tools::implementations::grok_build::task::coordinator::ChildCompletion<
        crate::agent::subagent::ShellCompletionData,
    >,
) {
    let Some(memory_root) = agent
        .sessions
        .borrow()
        .values()
        .find_map(|session| session.tool_context.task_tree_memory_workspace_dir.clone())
    else {
        tracing::debug!(
            subagent_id = %completion.request.id,
            "A6: no task-tree memory root wired; handoff receipt skipped"
        );
        return;
    };
    journal_child_handoff_into(completion, &memory_root);
}

/// The journaling half of [`journal_child_handoff`], separated so tests drive
/// the real shipped path with a temporary memory root.
fn journal_child_handoff_into(
    completion: &xai_grok_tools::implementations::grok_build::task::coordinator::ChildCompletion<
        crate::agent::subagent::ShellCompletionData,
    >,
    memory_root: &std::path::Path,
) -> Result<u64, String> {
    use xai_grok_memory::handoff_packet::HandoffPacketV1;
    use xai_grok_memory::lifecycle_journal::LifecycleJournal;
    use xai_grok_memory::nextgen_exit_gates::deliver_handoff_receipt;

    let request = &completion.request;
    let result = &completion.result;
    let root_session_id = &request.lineage.root_session_id;
    let output_hash = format!(
        "sha256:{}",
        blake3::hash(result.output.as_bytes()).to_hex()
    );
    let uncertainties: Vec<String> = result
        .error
        .as_deref()
        .filter(|error| !error.trim().is_empty())
        .map(str::to_owned)
        .into_iter()
        .collect();
    let terminal_reason = if result.cancelled {
        "cancelled"
    } else if result.success {
        "completed"
    } else {
        "failed"
    };
    let packet = HandoffPacketV1::build(
        request.id.clone(),
        root_session_id.clone(),
        request.id.clone(),
        output_hash.clone(),
        Vec::new(),
        vec![format!("output:{output_hash}")],
        uncertainties,
        "await root review".to_string(),
        Some(terminal_reason.to_string()),
    )
    .map_err(|deny| format!("handoff packet: {deny:?}"))?;
    let journal_dir = memory_root.join("task-tree-lifecycle");
    if let Err(error) = std::fs::create_dir_all(&journal_dir) {
        return Err(format!("create journal dir: {error}"));
    }
    let journal_path = journal_dir.join(format!(
        "{}.jsonl",
        &blake3::hash(root_session_id.as_bytes()).to_hex()[..16]
    ));
    let mut journal = LifecycleJournal::at_path(root_session_id.clone(), &journal_path);
    let sequence = journal.events().len() as u64;
    let occurred_at = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|duration| duration.as_secs())
        .unwrap_or(0);
    let event = deliver_handoff_receipt(
        &mut journal,
        &packet,
        format!("handoff:{}", request.id),
        root_session_id.clone(),
        sequence,
        occurred_at,
        1, // shipped policy revision baseline
    )
    .map_err(|error| format!("deliver handoff: {error:?}"))?;
    Ok(event.sequence)
}

/// DEBT-024(a): worktree auto-handoff at the real child-terminal seam.
///
/// A governed child cannot self-commit (S3 hard deny), so at completion the
/// worktree base commit is stable: observed base == expected base == current
/// HEAD. The helper computes the real delta (`git status --porcelain` +
/// per-path content hashes) and runs the root merge-handoff evaluation.
/// Fail-closed: without a root decision the evaluation denies
/// (`MissingRootDecision`), so nothing ever auto-applies.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) enum WorktreeHandoffOutcome {
    /// Workspace is not a git repository — nothing to merge.
    NotGitRepo,
    /// The handoff evaluation denied; `pending_root_decision` distinguishes
    /// the expected completion-time state (no decision yet) from a real deny
    /// (stale base, inactive lease, foreign tree, ...).
    Denied {
        reason: MergeHandoffDenyReason,
        pending_root_decision: bool,
    },
    /// Root decision present: a real receipt carrying the observed delta.
    Receipt(MergeReceiptV1),
}

/// Run `git -C <workspace> <args>` and return stdout on success.
fn git_output(workspace: &std::path::Path, args: &[&str]) -> Option<String> {
    let output = std::process::Command::new("git")
        .arg("-C")
        .arg(workspace)
        .args(args)
        .output()
        .ok()?;
    if !output.status.success() {
        return None;
    }
    Some(String::from_utf8_lossy(&output.stdout).into_owned())
}

/// Content hash of one changed path (deleted files hash the `deleted` marker).
fn changed_path_hash(workspace: &std::path::Path, relative: &str) -> String {
    match std::fs::read(workspace.join(relative)) {
        Ok(bytes) => format!("{relative}:{}", blake3::hash(&bytes).to_hex()),
        Err(_) => format!("{relative}:deleted"),
    }
}

/// Sync core of the handoff evaluation — unit-testable against a real temp
/// git repo; the async wrapper fetches the lease through the authority.
pub(super) fn evaluate_worktree_handoff_core(
    lease: &WriteScopeLease,
    workspace: &std::path::Path,
    root_decision_ref: Option<String>,
) -> WorktreeHandoffOutcome {
    let Some(head) = git_output(workspace, &["rev-parse", "HEAD"]) else {
        return WorktreeHandoffOutcome::NotGitRepo;
    };
    let head = head.trim().to_string();
    let porcelain = git_output(workspace, &["status", "--porcelain"]).unwrap_or_default();
    let changed_path_hashes: Vec<String> = porcelain
        .lines()
        .filter_map(|line| {
            let path = line.get(3..).map(str::trim).filter(|p| !p.is_empty())?;
            Some(changed_path_hash(workspace, path.trim_matches('"')))
        })
        .collect();
    let root_decision = root_decision_ref.clone().unwrap_or_default();
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|duration| duration.as_secs())
        .unwrap_or(0);
    match evaluate_merge_handoff(
        lease,
        &lease.root_tree_id,
        head.clone(),
        head,
        changed_path_hashes,
        &[],
        Vec::new(),
        root_decision,
        now,
    ) {
        Ok(receipt) => WorktreeHandoffOutcome::Receipt(receipt),
        Err(reason) => WorktreeHandoffOutcome::Denied {
            reason,
            pending_root_decision: root_decision_ref.is_none(),
        },
    }
}

/// Fetch the child's write-scope lease through the authority and evaluate the
/// merge handoff at completion. Best-effort: failures are logged, never
/// allowed to mask the completion itself.
fn handoff_write_scope_at_completion(
    agent_ref: &LocalRef<MvpAgent>,
    child_session_id: String,
    workspace: Option<std::path::PathBuf>,
) {
    let agent_ref = agent_ref.clone();
    tokio::task::spawn_local(async move {
        let this = agent_ref.get();
        let Some(handle) = this
            .sessions
            .borrow()
            .iter()
            .find(|(sid, _)| sid.0.as_ref() == child_session_id)
            .map(|(_, handle)| handle.clone())
        else {
            return;
        };
        let (tx, rx) = tokio::sync::oneshot::channel();
        if handle
            .cmd_tx
            .send(SessionCommand::GetWriteScopeLease { respond_to: tx })
            .is_err()
        {
            return;
        }
        let Ok(Some(lease)) = rx.await else {
            return; // root session / ungoverned child — no lease to hand off
        };
        let Some(workspace) =
            workspace.or_else(|| Some(std::path::PathBuf::from(handle.info.cwd.clone())))
        else {
            return;
        };
        match evaluate_worktree_handoff_core(&lease, &workspace, None) {
            WorktreeHandoffOutcome::NotGitRepo => {}
            WorktreeHandoffOutcome::Denied {
                reason,
                pending_root_decision: true,
            } => {
                tracing::info!(
                    child_session_id = %child_session_id,
                    deny = reason.code(),
                    "write-scope handoff pending root decision (fail-closed; nothing auto-applied)"
                );
            }
            outcome => {
                tracing::warn!(
                    child_session_id = %child_session_id,
                    ?outcome,
                    "unexpected worktree handoff outcome"
                );
            }
        }
    });
}

impl xai_grok_tools::implementations::grok_build::task::coordinator::ChildRunner
    for ShellChildRunner
{
    type Control = crate::agent::subagent::ShellChildRuntime;
    type CompletionData = crate::agent::subagent::ShellCompletionData;
    type RunFuture = xai_grok_tools::implementations::grok_build::task::coordinator::LocalBoxFuture<
        xai_grok_tools::implementations::grok_build::task::coordinator::ChildRunOutput<
            Self::CompletionData,
        >,
    >;
    type ValidateFuture =
        xai_grok_tools::implementations::grok_build::task::coordinator::LocalBoxFuture<
            xai_grok_tools::implementations::grok_build::task::types::SubagentValidateTypeOutcome,
        >;
    type DescribeFuture =
        xai_grok_tools::implementations::grok_build::task::coordinator::LocalBoxFuture<
            xai_grok_tools::implementations::grok_build::task::types::SubagentDescribeOutcome,
        >;
    fn run(
        &self,
        run: xai_grok_tools::implementations::grok_build::task::coordinator::ChildRunRequest<
            Self::Control,
        >,
    ) -> Self::RunFuture {
        let agent_ref = self.agent_ref.clone();
        let loop_outbox = self.loop_outbox.clone();
        let spawn_permits = self.spawn_permits.clone();
        let xai_grok_tools::implementations::grok_build::task::coordinator::ChildRunRequest {
            mut request,
            cancellation,
            reporter,
        } = run;
        // DEBT-024(d): the spawn adapter mints and holds a DispatchPermitV1
        // from the request's real identity/grant/manifest data (fail-closed:
        // no permit when context hashes are absent). Verified again at
        // completion.
        let permit = mint_child_spawn_permit(agent_ref.get(), &request);
        if let Some(permit) = permit {
            spawn_permits
                .lock()
                .unwrap_or_else(|poison| poison.into_inner())
                .insert(request.id.clone(), permit);
            tracing::debug!(
                subagent_id = %request.id,
                depth = request.lineage.depth,
                "governed spawn permit minted and held by the spawn adapter"
            );
        }
        // A10 (NG-08): kairos outbox exactly-once gate at the real
        // scheduler/daemon dispatch point. A child whose successful terminal
        // was already delivered is never dispatched again (INV-31); this
        // covers scheduler lease recovery/replay paths where the same child
        // id could be re-offered.
        if !loop_outbox.may_dispatch(&request.id) {
            tracing::warn!(
                subagent_id = %request.id,
                "A10 kairos outbox: child terminal already delivered; duplicate dispatch skipped"
            );
            let result = xai_grok_tools::implementations::grok_build::task::types::SubagentResult {
                success: false,
                output: std::sync::Arc::from(
                    "duplicate dispatch deduplicated by kairos outbox (terminal already delivered)",
                ),
                error: Some("already_delivered".to_owned()),
                cancelled: true,
                ..Default::default()
            };
            return Box::pin(async move {
                xai_grok_tools::implementations::grok_build::task::coordinator::ChildRunOutput {
                    result,
                    completion_data: Default::default(),
                    snapshot_ref: None,
                }
            });
        }
        Box::pin(async move {
            let this = agent_ref.get();
            let parent_sid = request.parent_session_id.clone();
            let Some(mut ctx) = this.try_build_subagent_spawn_context(&parent_sid) else {
                tracing::warn!(
                    parent_session_id = %parent_sid,
                    subagent_id = %request.id,
                    "Spawn for unknown or evicted parent session"
                );
                return xai_grok_tools::implementations::grok_build::task::coordinator::ChildRunOutput {
                    result: xai_grok_tools::implementations::grok_build::task::types::SubagentResult {
                        success: false,
                        error: Some(
                            "Parent session not found (evicted or torn down); cannot spawn subagent."
                                .to_owned(),
                        ),
                        subagent_id: request.id.clone(),
                        child_session_id: request.id,
                        ..Default::default()
                    },
                    completion_data: Default::default(),
                    snapshot_ref: None,
                };
            };
            let parent_handle = {
                let parent_sid = acp::SessionId::new(parent_sid);
                this.sessions.borrow().get(&parent_sid).cloned()
            };
            if let Some(handle) = parent_handle {
                ctx.parent_mcp_pool = handle.snapshot_mcp_pool().await;
                ctx.client_hooks = handle.snapshot_client_hooks().await;
                let definitions = handle.snapshot_tool_definitions().await;
                ctx.parent_tool_definitions = (!definitions.is_empty()).then_some(definitions);
            }
            if let Some(model) =
                scheduler_preflight_model(&request, ctx.parent_depth, &ctx.models_manager)
            {
                tracing::info!(
                    parent_session_id = %request.parent_session_id,
                    subagent_id = %request.id,
                    model = %model,
                    "Selected scheduler child model from user routing pool before execution"
                );
                request.runtime_overrides.model = Some(model);
                request.runtime_overrides.model_override_provenance =
                    xai_grok_tools::implementations::grok_build::task::types::ModelOverrideProvenance::Harness;
            }
            crate::agent::subagent::run_shell_child(
                request,
                ctx,
                cancellation,
                reporter,
                &this.gateway,
            )
            .await
        })
    }
    fn validate_type(
        &self,
        subagent_type: String,
        parent_session_id: String,
    ) -> Self::ValidateFuture {
        let agent_ref = self.agent_ref.clone();
        Box::pin(async move {
            let this = agent_ref.get();
            let ctx = this.build_subagent_validation_context(&parent_session_id);
            crate::agent::subagent::validate_subagent_type(&subagent_type, &ctx)
        })
    }
    fn describe_type(
        &self,
        subagent_type: String,
        harness_agent_type: Option<String>,
        parent_session_id: String,
    ) -> Self::DescribeFuture {
        let agent_ref = self.agent_ref.clone();
        Box::pin(async move {
            let this = agent_ref.get();
            match this.try_build_subagent_spawn_context(&parent_session_id) {
                Some(ctx) => crate::agent::subagent::describe_subagent_type(
                    &subagent_type,
                    harness_agent_type.as_deref(),
                    &ctx,
                ),
                None => {
                    tracing::warn!(
                        parent_session_id,
                        subagent_type,
                        "DescribeType for unknown/evicted parent session, replying Unavailable",
                    );
                    xai_grok_tools::implementations::grok_build::task::types::SubagentDescribeOutcome::Unavailable
                }
            }
        })
    }
    fn on_completed(
        &self,
        completion: xai_grok_tools::implementations::grok_build::task::coordinator::ChildCompletion<
            Self::CompletionData,
        >,
    ) {
        let gateway = self.agent_ref.get().gateway.clone();
        // A10 (NG-08): a *successful* terminal is the delivery event the
        // outbox records — failed/cancelled children stay dispatchable so
        // retries and recovery are never blocked.
        if completion.result.success {
            self.loop_outbox.mark_delivered(&completion.request.id);
        }
        // DEBT-024(d): re-verify the spawn permit at completion (INV-12).
        if let Some(permit) = self
            .spawn_permits
            .lock()
            .unwrap_or_else(|poison| poison.into_inner())
            .remove(&completion.request.id)
        {
            use xai_grok_memory::dispatch_permit::PermitConsumer;
            let now = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_secs())
                .unwrap_or(0);
            match permit.authorize(PermitConsumer::SpawnAdapter, now) {
                Ok(()) => {
                    tracing::debug!(
                        subagent_id = %completion.request.id,
                        "spawn permit verified at completion"
                    );
                }
                Err(deny) => {
                    tracing::warn!(
                        subagent_id = %completion.request.id,
                        deny = deny.code(),
                        "spawn permit invalid at completion (fail-closed)"
                    );
                }
            }
        }
        // A6 (NG-04D-4 / INV-18): every child terminal produces a bounded
        // handoff packet and a durable delivery receipt in the task-tree
        // lifecycle journal, so a late/foreign terminal reconciles instead of
        // being silently dropped. Best-effort journaling: a failure is logged,
        // never allowed to mask the completion itself.
        journal_child_handoff(self.agent_ref.get(), &completion);
        // DEBT-024(a): worktree auto-handoff — evaluate the root merge
        // handoff for governed children through the authority (fail-closed:
        // no root decision yet ⇒ pending; nothing auto-applies).
        handoff_write_scope_at_completion(
            &self.agent_ref,
            completion.result.child_session_id.clone(),
            completion.request.cwd.clone().map(std::path::PathBuf::from),
        );
        crate::agent::subagent::present_child_completion(completion, &gateway);
    }
    fn running_count_changed(&self, running: usize) {
        self.agent_ref
            .get()
            .activity
            .subagent_gauge()
            .store(running, std::sync::atomic::Ordering::Relaxed);
    }
    fn persisted_output_ref(&self, completion_data: &Self::CompletionData) -> Option<String> {
        completion_data
            .persisted_output_dir()
            .map(|path| path.to_string_lossy().into_owned())
    }
    fn load_persisted_output(&self, reference: &str) -> Option<std::sync::Arc<str>> {
        crate::agent::subagent::read_subagent_output(std::path::Path::new(reference))
            .map(std::sync::Arc::from)
    }
}
impl MvpAgent {
    /// Start the shared subagent coordinator actor.
    ///
    /// Takes `subagent_event_rx` once and `spawn_local`s one
    /// [`SubagentCoordinator`](xai_grok_tools::implementations::grok_build::task::coordinator::SubagentCoordinator)
    /// that drains `ChannelBackend` events (`Spawn` / await / cancel / inspect)
    /// through [`ShellChildRunner`]. The actor owns pending/active/completed
    /// state, waiters, deadlines, and completion disposition; the runner only
    /// builds shell child sessions via `run_shell_child`.
    ///
    /// Uses `LocalRef` so the `!Send` runner can touch `self` from the
    /// `LocalSet`. Idempotent: subsequent calls are no-ops.
    pub(super) fn start_subagent_coordinator(&self) {
        let Some(rx) = self.subagent_event_rx.borrow_mut().take() else {
            return;
        };
        let Some(control_rx) = self.subagent_control_rx.borrow_mut().take() else {
            // Do not start a coordinator that would silently downgrade Stop
            // and teardown to the data plane.
            tracing::error!("subagent coordinator control ingress missing; refusing to start");
            return;
        };
        let agent_ref = LocalRef::new(self);
        let runner = ShellChildRunner {
            agent_ref: agent_ref.clone(),
            loop_outbox: Arc::new(SchedulerLoopOutbox::default()),
            spawn_permits: Arc::new(std::sync::Mutex::new(std::collections::HashMap::new())),
        };
        let config =
            xai_grok_tools::implementations::grok_build::task::coordinator::CoordinatorConfig {
                foreground_budget:
                    xai_grok_tools::implementations::grok_build::task::backend::env_duration_or(
                        "GROK_SUBAGENT_AWAIT_BUDGET_MS",
                        std::time::Duration::from_secs(600),
                    ),
                max_live_children_per_parent: std::env::var("GROK_SUBAGENT_PARENT_FANOUT")
                    .ok()
                    .and_then(|value| value.parse::<usize>().ok())
                    .filter(|value| (1..=8).contains(value))
                    .unwrap_or(4),
                tree_wall_time_budget:
                    xai_grok_tools::implementations::grok_build::task::backend::env_duration_or(
                        "GROK_SUBAGENT_TREE_WALL_TIME_BUDGET_MS",
                        std::time::Duration::from_secs(2 * 60 * 60),
                    ),
                tree_total_token_budget: std::env::var("GROK_SUBAGENT_TREE_TOTAL_TOKEN_BUDGET")
                    .ok()
                    .and_then(|value| value.parse().ok()),
                tree_tool_call_budget: std::env::var("GROK_SUBAGENT_TREE_TOOL_CALL_BUDGET")
                    .ok()
                    .and_then(|value| value.parse().ok()),
                buffer_completions: true,
                buffered_completion_output_cap: None,
            };
        // The same host-owned workspace-memory root used by the shared ledger
        // also anchors operation journals.  Do not derive this from a child
        // request: task ids and paths are model-controlled input.
        let operation_store_dir = self
            .sessions
            .borrow()
            .values()
            .find_map(|session| session.tool_context.task_tree_memory_workspace_dir.clone());
        tokio::task::spawn_local(
            match operation_store_dir {
                Some(dir) => xai_grok_tools::implementations::grok_build::task::coordinator::SubagentCoordinator::with_operation_store_dir_and_control(
                    rx, control_rx, runner, config, dir,
                ).run(),
                None => xai_grok_tools::implementations::grok_build::task::coordinator::SubagentCoordinator::new_with_control(
                    rx, Some(control_rx), runner, config,
                ).run(),
            },
        );
        let (trace_tx, mut trace_rx) = tokio::sync::mpsc::unbounded_channel::<
            crate::upload::turn::SyntheticTurnTraceRequest,
        >();
        self.subagent_presentation.borrow_mut().synthetic_trace_tx = Some(trace_tx);
        tokio::task::spawn_local({
            let agent_ref = agent_ref.clone();
            async move {
                while let Some(request) = trace_rx.recv().await {
                    tokio::task::spawn_local({
                        let agent_ref = agent_ref.clone();
                        async move {
                            handle_synthetic_turn_trace(agent_ref, request).await;
                        }
                    });
                }
            }
        });
    }
    /// Lightweight context for the `SubagentEvent::ValidateType` drain arm;
    /// tolerates evicted parent sessions (returns built-in defaults + warns).
    pub(super) fn build_subagent_validation_context(
        &self,
        parent_session_id: &str,
    ) -> crate::agent::subagent::SubagentValidationContext {
        let parent_sid = acp::SessionId::new(parent_session_id);
        let (parent_cwd, allowed_subagent_types) = {
            let sessions = self.sessions.borrow();
            let ps = sessions.get(&parent_sid);
            warn_on_missing_parent_session_for_validate_type(parent_session_id, ps.is_some());
            (
                ps.map(|h| std::path::PathBuf::from(&h.info.cwd))
                    .unwrap_or_default(),
                ps.and_then(|h| h.allowed_subagent_types.clone()),
            )
        };
        let (cli_agent_names, subagent_toggle) = {
            let cfg = self.cfg.borrow();
            (
                cfg.cli_agents.iter().map(|d| d.name.clone()).collect(),
                cfg.subagent_toggle.clone(),
            )
        };
        crate::agent::subagent::SubagentValidationContext {
            parent_cwd,
            plugin_registry: self.plugin_registry_handle.snapshot(),
            subagent_toggle,
            allowed_subagent_types,
            cli_agent_names,
        }
    }
    /// Test-only infallible wrapper around
    /// [`Self::try_build_subagent_spawn_context`]. Production spawn paths use
    /// the fallible variant and fail the request when the parent session is
    /// absent (evicted, or a child-session spawn whose re-parent lookup
    /// missed).
    #[cfg(test)]
    pub(super) fn build_subagent_spawn_context(
        &self,
        parent_session_id: &str,
    ) -> crate::agent::subagent::SubagentSpawnContext {
        self.try_build_subagent_spawn_context(parent_session_id)
            .expect("parent session must exist when spawning subagents")
    }
    /// Build a `SubagentSpawnContext` from the current agent state and the
    /// parent session's shared resources. Returns `None` when the parent
    /// `SessionHandle` is absent (evicted / torn down) so callers can fail
    /// the request instead of panicking.
    ///
    /// This is the ONLY subagent-related method on MvpAgent besides the
    /// coordinator startup.
    pub(super) fn try_build_subagent_spawn_context(
        &self,
        parent_session_id: &str,
    ) -> Option<crate::agent::subagent::SubagentSpawnContext> {
        let parent_sid = acp::SessionId::new(parent_session_id);
        let (
            parent_model_id,
            parent_chat_state,
            parent_cmd_tx,
            parent_cwd,
            yolo_mode,
            parent_depth,
            hunk_tracker_handle,
            hunk_tracking_enabled,
            fs,
            terminal,
            session_env,
            task_tree_memory_workspace_dir,
            parent_attribution_callback,
            parent_agent_name,
            parent_managed_mcp_proxy_base_url,
        ) = {
            let sessions = self.sessions.borrow();
            let ps = sessions.get(&parent_sid);
            (
                ps.map(|h| h.model_id.clone())
                    .unwrap_or_else(|| self.models_manager.current_model_id()),
                ps.map(|h| h.chat_state_handle.clone()),
                ps.map(|h| h.cmd_tx.clone()),
                ps.map(|h| std::path::PathBuf::from(&h.info.cwd))
                    .unwrap_or_default(),
                ps.map(|h| h.yolo_mode).unwrap_or(self.default_yolo_mode),
                ps.map(|h| h.tool_context.subagent_depth).unwrap_or(0),
                ps.map(|h| h.tool_context.hunk_tracker_handle.clone())
                    .unwrap_or_else(xai_hunk_tracker::HunkTrackerHandle::noop),
                ps.map(|h| h.tool_context.hunk_tracking_enabled)
                    .unwrap_or(false),
                ps.map(|h| h.tool_context.fs.inner().clone())
                    .unwrap_or_else(|| {
                        let cwd = ps
                            .map(|h| std::path::PathBuf::from(&h.info.cwd))
                            .unwrap_or_default();
                        std::sync::Arc::new(xai_grok_workspace::file_system::LocalFs::new(cwd))
                    }),
                ps.map(|h| h.tool_context.terminal.clone())
                    .unwrap_or_else(|| {
                        std::sync::Arc::new(crate::terminal::TerminalRunner::new(
                            std::sync::Arc::new(self.gateway.clone()),
                            parent_sid.clone(),
                        ))
                    }),
                ps.map(|h| h.tool_context.session_env.clone())
                    .unwrap_or_else(|| std::sync::Arc::new(std::collections::HashMap::new())),
                ps.and_then(|h| h.tool_context.task_tree_memory_workspace_dir.clone()),
                ps.and_then(|h| h.attribution_callback.clone()),
                ps.map(|h| h.agent_name.clone()),
                ps.map(|h| h.managed_mcp_proxy_base_url.clone()),
            )
        };
        let (
            parent_workspace_ops,
            parent_terminal_backend,
            parent_notification_handle,
            parent_scheduler_handle,
        ) = {
            let sessions = self.sessions.borrow();
            sessions.get(&parent_sid).map(|ps| {
                (
                    ps.workspace_ops.clone(),
                    ps.terminal_backend.clone(),
                    ps.tools_notification_handle.clone(),
                    ps.scheduler_handle.clone(),
                )
            })
        }?;
        let available_models = self.models_manager.models();
        let (parent_lsp, parent_process_scope) = {
            let sessions = self.sessions.borrow();
            let parent = sessions.get(&parent_sid);
            (
                parent.and_then(|h| h.tool_context.lsp.clone()),
                parent.and_then(|h| h.tool_context.process_scope.clone()),
            )
        };
        let am = self.auth_manager.clone();
        let inference_idle_timeout_secs = {
            let per_model = config::find_model_by_id(&available_models, parent_model_id.0.as_ref())
                .and_then(|e| e.info.inference_idle_timeout_secs);
            let cfg = self.cfg.borrow();
            let remote = cfg
                .remote_settings
                .as_ref()
                .and_then(|s| s.inference_idle_timeout_secs);
            per_model.or(remote).unwrap_or(600).max(10)
        };
        let parent_hook_registry = {
            let sessions = self.sessions.borrow();
            sessions
                .get(&parent_sid)
                .and_then(|h| h.hook_registry.clone())
        };
        let parent_max_turns = {
            let sessions = self.sessions.borrow();
            sessions.get(&parent_sid).and_then(|h| h.max_turns)
        };
        let parent_model_agent_type =
            config::find_model_by_id(&available_models, parent_model_id.0.as_ref())
                .map(|e| e.info.agent_type.clone());
        let ask_user_question_enabled = {
            let sessions = self.sessions.borrow();
            sessions
                .get(&parent_sid)
                .map(|h| h.ask_user_question_enabled)
                .unwrap_or_else(|| self.cfg.borrow().resolve_ask_user_question().value)
        };
        let (gcs_upload_method, gcs_bucket_url) = match self.trace_upload_config_snapshot() {
            Some(method) => {
                let bucket = match &method {
                    UploadMethod::Direct { .. } => self
                        .cfg
                        .borrow()
                        .endpoints
                        .resolve_trace_bucket_url()
                        .map(|r| r.value),
                    UploadMethod::Proxy { .. } => Some("proxy-managed".to_string()),
                    UploadMethod::S3 { bucket, .. } => Some(format!("s3://{bucket}")),
                };
                match bucket {
                    Some(url) => (Some(method), Some(url)),
                    None => (None, None),
                }
            }
            None => (None, None),
        };
        let project_trusted = crate::agent::folder_trust::project_scope_allowed(&parent_cwd);
        let (base_roles, base_personas, subagent_model_overrides, subagent_toggle) = {
            let cfg = self.cfg.borrow();
            (
                cfg.subagent_roles.clone(),
                cfg.subagent_personas.clone(),
                cfg.subagent_model_overrides.clone(),
                cfg.subagent_toggle.clone(),
            )
        };
        let (subagent_roles, subagent_personas) =
            crate::config::SubagentsConfig::effective_definition_maps(
                &base_roles,
                &base_personas,
                &parent_cwd,
                project_trusted,
            );
        let inherited_tool_overrides = {
            let sessions = self.sessions.borrow();
            sessions
                .get(&parent_sid)
                .and_then(|ps| ps.resolved_tool_overrides.load_full().map(|o| (*o).clone()))
        };
        Some(crate::agent::subagent::SubagentSpawnContext {
            lsp: parent_lsp,
            process_scope: parent_process_scope,
            client_hooks: Default::default(),
            sampling_config: self.sampling_config.borrow().clone(),
            managed_mcp_proxy_base_url: parent_managed_mcp_proxy_base_url
                .unwrap_or_else(|| self.cli_chat_proxy_base_url()),
            alpha_test_key: self.alpha_test_key(),
            auth_method_id: self
                .auth_method_id
                .load()
                .as_deref()
                .cloned()
                .unwrap_or_else(|| acp::AuthMethodId::new("default")),
            model_id: parent_model_id,
            auth: self.current_or_buffered_auth(),
            parent_cwd: parent_cwd.clone(),
            parent_session_id: parent_session_id.to_string(),
            inherited_tool_overrides,
            yolo_mode,
            subagent_event_tx: self.subagent_event_tx.clone(),
            subagent_control_tx: self.subagent_control_tx.clone(),
            parent_depth,
            subagents_max_depth: self.cfg.borrow().subagents_max_depth,
            inference_idle_timeout_secs,
            auto_compact_threshold_tiers:
                crate::agent::subagent::AutoCompactThresholdTiers::capture(&self.cfg.borrow()),
            hunk_tracker_handle,
            hunk_tracking_enabled,
            fs,
            terminal,
            session_env,
            memory_config: self.memory_config.clone(),
            task_tree_memory_workspace_dir,
            web_search_sampling_config: self.prepare_web_search_sampling_config(),
            web_fetch_config: self.prepare_web_fetch_config(),
            image_gen_config: self.prepare_image_gen_config(),
            video_gen_config: self.prepare_video_gen_config(),
            app_builder_deployer_config: self.prepare_app_builder_deployer_config(),
            write_file_enabled: self.cfg.borrow().resolve_write_file().value,
            goal_enabled: self.cfg.borrow().resolve_goal().value,
            background_workflows_enabled: self.cfg.borrow().resolve_workflows().value,
            ask_user_question_enabled,
            parent_cmd_tx: parent_cmd_tx.clone(),
            parent_session_info: {
                let sessions = self.sessions.borrow();
                sessions
                    .get(&parent_sid)
                    .map(|h| crate::session::info::Info {
                        id: parent_sid.clone(),
                        cwd: h.info.cwd.clone(),
                    })
            },
            parent_chat_state,
            parent_max_turns,
            available_models,
            subagent_model_overrides,
            subagent_toggle,
            subagent_roles,
            subagent_personas,
            disable_web_search: self.cfg.borrow().disable_web_search,
            todo_gate: self.cfg.borrow().todo_gate,
            remote_settings: self.cfg.borrow().remote_settings.clone(),
            laziness_debug_log: self.cfg.borrow().laziness_debug_log.clone(),
            backend_tools_enabled: self.cfg.borrow().resolve_backend_tools().value,
            respect_gitignore: self.cfg.borrow().respect_gitignore,
            path_not_found_hints: self.cfg.borrow().path_not_found_hints,
            plugin_registry: self.plugin_registry_handle.snapshot(),
            models_manager: self.models_manager.clone(),
            file_tool_overrides: {
                let cfg = self.cfg.borrow();
                let effective = cfg
                    .toolset
                    .resolve_file_toolset(cfg.remote_settings.as_ref());
                if effective != crate::tools::FileToolset::Standard {
                    effective.tool_configs(&cfg.toolset.hashline).ok()
                } else {
                    None
                }
            },
            gcs_bucket_url,
            agent_config: Some(self.cfg.borrow().clone()),
            gcs_upload_method,
            hook_registry: parent_hook_registry,
            permission_handle: {
                let sessions = self.sessions.borrow();
                sessions
                    .get(&parent_sid)
                    .map(|h| h.permission_handle.clone())
            },
            worktree_type: self.worktree_type,
            api_key_provider: Some(Arc::new(crate::auth::manager::SharedAuthKeyProvider(
                am.clone(),
            ))),
            image_description_model: self.resolve_image_description_model(),
            workspace_ops: parent_workspace_ops.clone(),
            auth_manager: am.clone(),
            attribution_callback: parent_attribution_callback,
            parent_agent_name,
            parent_model_agent_type,
            allowed_subagent_types: {
                let sessions = self.sessions.borrow();
                sessions
                    .get(&parent_sid)
                    .and_then(|h| h.allowed_subagent_types.clone())
            },
            parent_mcp_configs: {
                let sessions = self.sessions.borrow();
                sessions
                    .get(&parent_sid)
                    .map(|h| h.mcp_servers.clone())
                    .unwrap_or_default()
            },
            managed_mcp_state: self.managed_mcp_cache.clone(),
            parent_mcp_pool: None,
            parent_tool_definitions: None,
            parent_skills: None,
            parent_skills_config: self.cfg.borrow().skills.clone(),
            parent_compat: self.cfg.borrow().compat_resolved,
            task_completion_reservations: {
                let sessions = self.sessions.borrow();
                sessions
                    .get(&parent_sid)
                    .and_then(|h| h.tool_context.task_completion_reservations.clone())
            },
            synthetic_trace_tx: {
                let sessions = self.sessions.borrow();
                sessions
                    .get(&parent_sid)
                    .and_then(|h| h.tool_context.synthetic_trace_tx.clone())
            },
            task_output_tool_name: {
                let sessions = self.sessions.borrow();
                sessions
                    .get(&parent_sid)
                    .map(|h| h.tool_context.task_output_tool_name.clone())
                    .unwrap_or_else(|| {
                        xai_grok_tools::reminders::task_completion::DEFAULT_TASK_OUTPUT_TOOL
                            .to_string()
                    })
            },
            auto_wake_enabled: self.cfg.borrow().auto_wake_enabled,
            goal_loop_active: {
                let sessions = self.sessions.borrow();
                sessions
                    .get(&parent_sid)
                    .map(|h| h.tool_context.goal_loop_active_gate.clone())
                    .unwrap_or_else(|| {
                        std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false))
                    })
            },
            parent_terminal_backend: parent_terminal_backend.clone(),
            parent_notification_handle: parent_notification_handle.clone(),
            parent_scheduler_handle: parent_scheduler_handle.clone(),
        })
    }
}

#[cfg(test)]
mod scheduler_model_routing_tests {
    use super::*;
    use xai_grok_tools::implementations::grok_build::task::types::{
        SubagentLineage, SubagentOwner, SubagentRequest, SubagentRuntimeOverrides,
    };

    fn scheduler_request() -> SubagentRequest {
        SubagentRequest {
            id: "scheduled-child".to_owned(),
            prompt: "review the security boundary".to_owned(),
            description: "scheduled review".to_owned(),
            subagent_type: "general-purpose".to_owned(),
            parent_session_id: "root".to_owned(),
            lineage: SubagentLineage::direct("root"),
            parent_prompt_id: None,
            resume_from: None,
            cwd: None,
            runtime_overrides: SubagentRuntimeOverrides {
                loop_task_id: Some("loop-1".to_owned()),
                ..Default::default()
            },
            run_in_background: true,
            surface_completion: true,
            await_to_completion: false,
            fork_context: false,
            owner: SubagentOwner::Task,
            cancel_token: tokio_util::sync::CancellationToken::new(),
        }
    }

    fn model_entry(model: &str, base_url: &str) -> crate::agent::config::ModelEntry {
        let mut info = crate::agent::config::ModelInfo::fallback(model);
        info.base_url = base_url.to_owned();
        crate::agent::config::ModelEntry {
            info,
            api_key: Some("test-key".to_owned()),
            env_key: None,
            auth_provider: None,
            api_base_url: Some(base_url.to_owned()),
        }
    }

    #[test]
    fn scheduler_pool_selects_only_fresh_root_loop_children() {
        let manager = crate::agent::models::ModelsManager::default();
        manager.insert_test_entry(
            "flash",
            model_entry("flash", "https://flash.example.test/v1"),
        );
        manager.insert_test_entry("grok", model_entry("grok", "https://grok.example.test/v1"));
        manager.set_model_routing_config(crate::agent::config::ModelRoutingConfig {
            enabled: true,
            model_pool: vec!["flash".to_owned(), "grok".to_owned()],
            priority: vec![],
            task_preferences: Default::default(),
        });

        let request = scheduler_request();
        assert_eq!(
            scheduler_preflight_model(&request, 0, &manager),
            Some("grok".to_owned()),
            "the review task uses the configured root pool before it starts"
        );

        manager.record_provider_failure("https://grok.example.test/v1", "quota_exhausted");
        assert_eq!(
            scheduler_preflight_model(&request, 0, &manager),
            Some("flash".to_owned()),
            "a later fresh scheduler iteration skips a quota-exhausted provider"
        );

        let mut resumed = request.clone();
        resumed.resume_from = Some("prior-child".to_owned());
        assert_eq!(scheduler_preflight_model(&resumed, 0, &manager), None);
        assert_eq!(scheduler_preflight_model(&request, 1, &manager), None);

        let mut explicit = request;
        explicit.runtime_overrides.model = Some("flash".to_owned());
        assert_eq!(scheduler_preflight_model(&explicit, 0, &manager), None);

        manager.set_current_model_id(acp::ModelId::new("flash"));
        assert_eq!(
            scheduler_preflight_model(&scheduler_request(), 0, &manager),
            None
        );
    }

    #[test]
    fn scheduler_loop_outbox_dedups_delivered_terminal_only() {
        let outbox = SchedulerLoopOutbox::default();
        assert!(outbox.may_dispatch("child-1"));
        outbox.mark_delivered("child-1");
        assert!(
            !outbox.may_dispatch("child-1"),
            "a delivered successful terminal must never be dispatched again (INV-31)"
        );
        assert!(
            outbox.may_dispatch("child-2"),
            "unrelated children stay dispatchable"
        );
        assert!(outbox.may_dispatch("child-1-retry"));
    }

    /// Create a real temp git repo with one committed file.
    fn temp_git_repo() -> (tempfile::TempDir, std::path::PathBuf) {
        let dir = tempfile::tempdir().expect("tempdir");
        let run = |args: &[&str]| {
            let output = std::process::Command::new("git")
                .arg("-C")
                .arg(dir.path())
                .args(args)
                .output()
                .expect("git runs");
            assert!(
                output.status.success(),
                "git {args:?} failed: {}",
                String::from_utf8_lossy(&output.stderr)
            );
        };
        run(&["init", "-q"]);
        run(&["config", "user.email", "test@example.com"]);
        run(&["config", "user.name", "test"]);
        std::fs::write(dir.path().join("src.rs"), "fn main() {}\n").expect("write");
        run(&["add", "src.rs"]);
        run(&["commit", "-q", "-m", "init"]);
        drop(run);
        let src = dir.path().join("src.rs");
        (dir, src)
    }

    #[test]
    fn worktree_handoff_denies_without_root_decision_and_receipts_with_one() {
        let (dir, src) = temp_git_repo();
        let lease = WriteScopeLease::issue(
            "grant-1",
            "tree-1",
            "node-1",
            vec![dir.path().to_path_buf()],
            3600,
        )
        .expect("lease");

        // No root decision at completion time -> fail-closed pending.
        let outcome = evaluate_worktree_handoff_core(&lease, dir.path(), None);
        assert_eq!(
            outcome,
            WorktreeHandoffOutcome::Denied {
                reason: MergeHandoffDenyReason::MissingRootDecision,
                pending_root_decision: true,
            },
            "without a root decision nothing may auto-apply"
        );

        // Root decision present -> real receipt with the observed delta.
        std::fs::write(&src, "fn main() { println!(\"hi\"); }\n").expect("modify");
        let outcome = evaluate_worktree_handoff_core(&lease, dir.path(), Some("approval-1".into()));
        match outcome {
            WorktreeHandoffOutcome::Receipt(receipt) => {
                assert_eq!(receipt.apply_result, MergeApplyResult::Applied);
                assert_eq!(receipt.root_decision_ref, "approval-1");
                assert_eq!(receipt.write_lease_id, "grant-1");
                assert_eq!(receipt.node_id, "node-1");
                assert!(
                    receipt
                        .changed_path_hashes
                        .iter()
                        .any(|hash| hash.starts_with("src.rs:")),
                    "the changed path must carry its content hash: {:?}",
                    receipt.changed_path_hashes
                );
            }
            other => panic!("expected a receipt, got {other:?}"),
        }
    }

    #[test]
    fn worktree_handoff_rejects_inactive_lease_and_non_repo() {
        let (dir, _) = temp_git_repo();
        let mut lease = WriteScopeLease::issue(
            "grant-2",
            "tree-1",
            "node-1",
            vec![dir.path().to_path_buf()],
            3600,
        )
        .expect("lease");
        lease.revoke();
        let outcome = evaluate_worktree_handoff_core(&lease, dir.path(), Some("approval-1".into()));
        assert_eq!(
            outcome,
            WorktreeHandoffOutcome::Denied {
                reason: MergeHandoffDenyReason::LeaseNotActive,
                pending_root_decision: false,
            }
        );

        // Not a git repo -> no handoff.
        let plain = tempfile::tempdir().expect("tempdir");
        std::fs::write(plain.path().join("file.txt"), "x").expect("write");
        let lease = WriteScopeLease::issue(
            "grant-3",
            "tree-1",
            "node-1",
            vec![plain.path().to_path_buf()],
            3600,
        )
        .expect("lease");
        assert_eq!(
            evaluate_worktree_handoff_core(&lease, plain.path(), Some("approval-1".into())),
            WorktreeHandoffOutcome::NotGitRepo
        );
    }

    #[test]
    fn spawn_permit_mints_for_governed_child_and_fails_closed_otherwise() {
        use xai_grok_memory::dispatch_permit::PermitConsumer;

        // Governed child (lineage depth 1) with real context hashes -> permit.
        let request = scheduler_request();
        let permit = mint_child_spawn_permit_core(
            &request,
            "sha256:manifest",
            "sha256:snapshot",
            1_000,
        )
        .expect("governed child mints a spawn permit");
        permit
            .authorize(PermitConsumer::SpawnAdapter, 1_000)
            .expect("authorize");
        let binding = permit.binding();
        assert_eq!(binding.budget_reservation_id, format!("spawn:{}", request.id));
        assert_eq!(binding.consumer, "spawn");

        // Root/ungoverned spawns carry no permit.
        let mut root = request.clone();
        root.lineage = SubagentLineage::direct("root");
        root.lineage.depth = 0;
        assert!(
            mint_child_spawn_permit_core(&root, "sha256:manifest", "sha256:snapshot", 1_000)
                .is_none(),
            "depth-0 spawns must not mint a permit"
        );

        // Missing manifest fails closed (no permit, never a fabricated one).
        assert!(
            mint_child_spawn_permit_core(&request, "", "sha256:snapshot", 1_000).is_none(),
            "absent manifest must fail closed"
        );

        // Capability ceiling mapping is monotone and conservative.
        use xai_tool_types::SubagentCapabilityMode;
        assert_eq!(
            capability_mode_to_grant_classes(Some(SubagentCapabilityMode::ReadOnly)),
            vec![GrantCapabilityClass::ReadOnly]
        );
        assert_eq!(
            capability_mode_to_grant_classes(None),
            vec![GrantCapabilityClass::ReadOnly]
        );
        assert!(
            capability_mode_to_grant_classes(Some(SubagentCapabilityMode::Execute))
                .contains(&GrantCapabilityClass::SpawnChild)
        );
    }

    #[test]
    fn child_terminal_journals_durable_handoff_receipt() {
        use xai_grok_tools::implementations::grok_build::task::coordinator::{
            ChildCompletion, CompletionDisposition,
        };
        use xai_grok_tools::implementations::grok_build::task::types::SubagentResult;

        let dir = tempfile::tempdir().expect("tempdir");
        let request = scheduler_request();
        let completion = ChildCompletion {
            request,
            result: SubagentResult {
                success: true,
                output: std::sync::Arc::from("evidence: tests pass\n"),
                error: None,
                cancelled: false,
                ..Default::default()
            },
            completion_data: crate::agent::subagent::ShellCompletionData::default(),
            disposition: CompletionDisposition {
                foreground_delivered: true,
                backgrounded: false,
                waiter_delivered: false,
                explicitly_killed: false,
                should_surface: true,
            },
        };
        let sequence = journal_child_handoff_into(&completion, dir.path())
            .expect("handoff receipt journaled");
        assert_eq!(sequence, 0, "first event in a fresh tree journal");

        // The receipt is durable: reload the journal from disk and verify the
        // event carries evidence refs and the terminal handoff marker.
        let journal_path = dir
            .path()
            .join("task-tree-lifecycle")
            .join(format!("{}.jsonl", &blake3::hash(b"root").to_hex()[..16]));
        assert!(journal_path.is_file(), "journal file must exist on disk");
        let journal = xai_grok_memory::lifecycle_journal::LifecycleJournal::at_path(
            "root",
            &journal_path,
        );
        let events = journal.events();
        assert_eq!(events.len(), 1);
        assert!(events[0].contract_hash.is_some());
        assert!(!events[0].evidence_refs.is_empty());
        assert_eq!(
            events[0].kind,
            xai_grok_memory::lifecycle_journal::GovernedLifecycleEventKind::Checkpointed
        );
    }
}
