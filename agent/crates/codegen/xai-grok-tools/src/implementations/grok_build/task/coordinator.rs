//! Single-writer subagent coordinator actor.
//!
//! The actor owns the command receiver, pending/active/completed state,
//! concrete blocking waiters, foreground deadlines, cancellation, and the
//! terminal delivery disposition. All hosts drive it through `ChannelBackend`;
//! only their `ChildRunner` implementations differ.
//!
//! There is intentionally no shared mutable state in this module. A runner's
//! associated futures may be `Send` or non-`Send`; the resulting actor future
//! inherits that property naturally on stable Rust.

mod query;

use std::collections::{HashMap, HashSet, VecDeque};
use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use futures::FutureExt;
use futures::stream::{FuturesUnordered, StreamExt};
use tokio::sync::{mpsc, oneshot};

use super::HARD_MAX_SUBAGENT_DEPTH;
use super::authority_log::{AuthorityEventKind, TreeAuthorityLog};
use super::budget::{
    BudgetDenial, BudgetLedger, ReleaseOutcome, ReservationId, TreeBudgetV1, UsageSettlement,
};
use super::coordinator_state::{
    ActiveChild, BlockingWaiter, BufferedCompletion, ChildRecord, CompletedChild, InternalEvent,
    ListRequest, PendingChild, ProgressFuture, ProgressTarget, ReplyFuture, TaggedFuture,
    active_summary, background_at_deadline, background_if_caller_gone, completed_snapshot,
    completion_summary, sleep_until, workflow_outstanding,
};
use super::governed_operation::GovernedOperationStore;
use super::types::{
    SpawnedSubagentRef, SubagentCancelOutcome, SubagentCancelTarget, SubagentDescribeOutcome,
    SubagentEvent, SubagentLineage, SubagentOutstandingReply, SubagentOwner,
    SubagentRecoveredTerminalRequest, SubagentRegistryCounts, SubagentRequest, SubagentResult,
    SubagentResumeLookup, SubagentResumeSource, SubagentValidateTypeOutcome,
};

pub use super::coordinator_state::{
    ChildCompletion, ChildControl, ChildReporter, ChildRunOutput, ChildRunRequest, ChildRunner,
    CompletionDisposition, CoordinatorConfig, LocalBoxFuture, MAX_COMPLETED_ENTRIES, SendBoxFuture,
    StartedChild, SubagentProgress,
};

/// A single root task tree may have at most this many pending or active
/// children.  The limit intentionally counts startup work too: otherwise a
/// fast fan-out could queue an unbounded number of runtimes before they report
/// `Started`.  Depth limits control shape; this limit controls width.
pub const MAX_LIVE_SUBAGENTS_PER_TREE: usize = 8;

/// Runner-to-coordinator lifecycle traffic is bounded separately from the
/// host command ingress. A child must wait for the single writer to observe
/// its `Started`/resume lookup rather than being able to allocate an unbounded
/// in-memory backlog while the coordinator is busy handling commands.
pub const MAX_INTERNAL_LIFECYCLE_EVENTS: usize = 64;

/// Durable interpretation of a child terminal receipt's attempted handoff.
/// `Undelivered` is normal for background work whose buffered completion has
/// not yet been consumed. `Uncertain` is narrower: a foreground receiver was
/// expected but was closed, so the coordinator must not imply a handoff.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum SpawnDeliveryObservation {
    Delivered,
    Undelivered,
    Uncertain,
}

/// Channel-owned subagent lifecycle actor.
pub struct SubagentCoordinator<R: ChildRunner> {
    commands: mpsc::UnboundedReceiver<SubagentEvent>,
    /// Independent host control lane. `Cancel`, `TeardownSession`, and
    /// admission reopening must not be stuck behind model-generated work.
    control_commands: Option<mpsc::UnboundedReceiver<SubagentEvent>>,
    internal_tx: mpsc::Sender<InternalEvent<R::Control>>,
    internal_rx: mpsc::Receiver<InternalEvent<R::Control>>,
    runner: R,
    config: CoordinatorConfig,
    pending: HashMap<String, PendingChild>,
    active: HashMap<String, ActiveChild<R::Control>>,
    completed: HashMap<String, CompletedChild>,
    completed_order: VecDeque<String>,
    /// Host-reconstructed terminal snapshots from a prior process. They are
    /// queryable but never treated as live handles or completion deliveries.
    recovered_terminals: HashMap<String, RecoveredTerminal>,
    waiters: HashMap<String, Vec<BlockingWaiter>>,
    workflow_cancel_waiters: HashMap<String, Vec<oneshot::Sender<SubagentCancelOutcome>>>,
    /// Parent sessions that received `ParentSession` cancel. Non-workflow spawns
    /// are rejected until [`SubagentEvent::OpenSpawnAdmission`] (next turn) or
    /// teardown, so a detached late `TaskTool` spawn cannot outrun Stop.
    spawn_blocked_sessions: HashSet<String>,
    /// Root task trees whose total wall-clock budget has expired. Kept until
    /// root teardown so a completed child cannot restart an exhausted tree.
    expired_tree_roots: HashSet<String>,
    /// First admitted child time for every root tree. Sequential fan-out
    /// therefore shares one wall-clock budget.
    tree_started_at: HashMap<String, tokio::time::Instant>,
    /// Provider-reported aggregate completed-child usage, by root task tree.
    tree_total_tokens_used: HashMap<String, u64>,
    /// Incomplete usage closes a task tree because accepting another child
    /// would make its accounting and any configured ceiling untrustworthy.
    tree_usage_incomplete_roots: HashSet<String>,
    exhausted_tree_token_budgets: HashSet<String>,
    tree_tool_calls_used: HashMap<String, u64>,
    exhausted_tree_tool_call_budgets: HashSet<String>,
    usage_not_applied_prompts: HashSet<PromptScope>,
    pending_completions: Vec<BufferedCompletion>,
    runs: FuturesUnordered<
        TaggedFuture<futures::future::CatchUnwind<std::panic::AssertUnwindSafe<R::RunFuture>>>,
    >,
    validations: FuturesUnordered<ReplyFuture<R::ValidateFuture, SubagentValidateTypeOutcome>>,
    descriptions: FuturesUnordered<ReplyFuture<R::DescribeFuture, SubagentDescribeOutcome>>,
    progress: FuturesUnordered<ProgressFuture<<R::Control as ChildControl>::ProgressFuture>>,
    list_requests: HashMap<u64, ListRequest>,
    next_list_request_id: u64,
    /// SessionActor-owned durable operation stores, keyed by root task tree id.
    /// Not a second runtime: the coordinator is the host-side durable owner
    /// for create/claim/cancel-cascade of child work under each root.
    /// `Arc` so tests can share a probe handle without a second writer path.
    tree_operations: Arc<Mutex<HashMap<String, GovernedOperationStore>>>,
    /// Per-root atomic budget ledgers (NG-03B). Structural ceilings are
    /// check-and-reserve here; usage settle/release happens on terminal.
    /// Shared so tests can probe reservations without a second writer path.
    tree_budgets: Arc<Mutex<HashMap<String, BudgetLedger>>>,
    /// Per-root in-process authority event log (NG-03C wiring). Fail-closed
    /// no-revival trail for spawn/settle/cancel; disk JSONL journal remains
    /// the memory-crate LifecycleJournal for offline compose evidence.
    tree_authority_logs: Arc<Mutex<HashMap<String, TreeAuthorityLog>>>,
    /// Optional host-owned durable location.  Test-only and legacy callers
    /// leave this unset; the shell host supplies its task-tree memory root.
    operation_store_dir: Option<PathBuf>,
}

#[derive(Debug, Clone)]
struct RecoveredTerminal {
    parent_session_id: String,
    snapshot: super::types::SubagentSnapshot,
}

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
struct PromptScope {
    parent_session_id: String,
    prompt_id: String,
}

impl PromptScope {
    fn new(parent_session_id: String, prompt_id: String) -> Self {
        Self {
            parent_session_id,
            prompt_id,
        }
    }
}

impl<R: ChildRunner> SubagentCoordinator<R> {
    pub fn new(
        commands: mpsc::UnboundedReceiver<SubagentEvent>,
        runner: R,
        config: CoordinatorConfig,
    ) -> Self {
        Self::new_with_control(commands, None, runner, config)
    }

    /// Construct with a physically separate host control ingress. The legacy
    /// constructor intentionally has no control receiver so focused callers
    /// retain their one-channel behavior without a permanently-ready closed
    /// select branch.
    pub fn new_with_control(
        commands: mpsc::UnboundedReceiver<SubagentEvent>,
        control_commands: Option<mpsc::UnboundedReceiver<SubagentEvent>>,
        runner: R,
        config: CoordinatorConfig,
    ) -> Self {
        Self::with_tree_operations(
            commands,
            control_commands,
            runner,
            config,
            Arc::new(Mutex::new(HashMap::new())),
            Arc::new(Mutex::new(HashMap::new())),
            Arc::new(Mutex::new(HashMap::new())),
        )
    }

    /// Construct with shared durable maps (production uses empty private maps;
    /// tests inject probe handles for operations, budgets, and authority logs).
    pub fn with_tree_operations(
        commands: mpsc::UnboundedReceiver<SubagentEvent>,
        control_commands: Option<mpsc::UnboundedReceiver<SubagentEvent>>,
        runner: R,
        config: CoordinatorConfig,
        tree_operations: Arc<Mutex<HashMap<String, GovernedOperationStore>>>,
        tree_budgets: Arc<Mutex<HashMap<String, BudgetLedger>>>,
        tree_authority_logs: Arc<Mutex<HashMap<String, TreeAuthorityLog>>>,
    ) -> Self {
        let (internal_tx, internal_rx) = mpsc::channel(MAX_INTERNAL_LIFECYCLE_EVENTS);
        Self {
            commands,
            control_commands,
            internal_tx,
            internal_rx,
            runner,
            config,
            pending: HashMap::new(),
            active: HashMap::new(),
            completed: HashMap::new(),
            completed_order: VecDeque::new(),
            recovered_terminals: HashMap::new(),
            waiters: HashMap::new(),
            workflow_cancel_waiters: HashMap::new(),
            spawn_blocked_sessions: HashSet::new(),
            expired_tree_roots: HashSet::new(),
            tree_started_at: HashMap::new(),
            tree_total_tokens_used: HashMap::new(),
            tree_usage_incomplete_roots: HashSet::new(),
            exhausted_tree_token_budgets: HashSet::new(),
            tree_tool_calls_used: HashMap::new(),
            exhausted_tree_tool_call_budgets: HashSet::new(),
            usage_not_applied_prompts: HashSet::new(),
            pending_completions: Vec::new(),
            runs: FuturesUnordered::new(),
            validations: FuturesUnordered::new(),
            descriptions: FuturesUnordered::new(),
            progress: FuturesUnordered::new(),
            list_requests: HashMap::new(),
            next_list_request_id: 0,
            tree_operations,
            tree_budgets,
            tree_authority_logs,
            operation_store_dir: None,
        }
    }

    /// Construct a coordinator whose per-tree operation journals survive a
    /// SessionActor process restart.  The host owns the directory; the child
    /// never chooses it from model-controlled input.
    pub fn with_operation_store_dir(
        commands: mpsc::UnboundedReceiver<SubagentEvent>,
        runner: R,
        config: CoordinatorConfig,
        operation_store_dir: PathBuf,
    ) -> Self {
        let mut coordinator = Self::new(commands, runner, config);
        coordinator.operation_store_dir = Some(operation_store_dir);
        coordinator
    }

    /// Equivalent to [`Self::with_operation_store_dir`] with an independent
    /// control ingress supplied by the SessionActor host.
    pub fn with_operation_store_dir_and_control(
        commands: mpsc::UnboundedReceiver<SubagentEvent>,
        control_commands: mpsc::UnboundedReceiver<SubagentEvent>,
        runner: R,
        config: CoordinatorConfig,
        operation_store_dir: PathBuf,
    ) -> Self {
        let mut coordinator =
            Self::new_with_control(commands, Some(control_commands), runner, config);
        coordinator.operation_store_dir = Some(operation_store_dir);
        coordinator
    }

    /// Build the per-tree budget contract from live coordinator config.
    fn tree_budget_contract(&self) -> TreeBudgetV1 {
        let max_children = u8::try_from(self.config.max_live_children_per_parent.min(255))
            .unwrap_or(u8::MAX);
        let max_live = u16::try_from(MAX_LIVE_SUBAGENTS_PER_TREE.min(u16::MAX as usize))
            .unwrap_or(u16::MAX);
        TreeBudgetV1 {
            max_depth: u8::try_from(HARD_MAX_SUBAGENT_DEPTH.min(u32::from(u8::MAX)))
                .unwrap_or(u8::MAX),
            max_children_per_node: max_children.max(1),
            max_live_nodes: max_live.max(1),
            max_background_nodes: max_live.max(1),
            token_reservation_limit: self.config.tree_total_token_budget,
            tool_call_limit: self
                .config
                .tree_tool_call_budget
                .and_then(|n| u32::try_from(n).ok()),
            wall_time_limit: self.config.tree_wall_time_budget,
            daily_cost_limit: None,
            artifact_byte_limit: None,
        }
    }

    fn parse_ledger_reservation(raw: &str) -> Option<ReservationId> {
        raw.strip_prefix("ledger:")
            .and_then(|n| n.parse::<u64>().ok())
            .map(ReservationId)
    }

    /// Atomic check-and-reserve on the tree budget ledger. Fail-closed.
    fn reserve_tree_budget(&self, request: &SubagentRequest) -> Result<ReservationId, String> {
        let root = request.lineage.root_session_id.as_str();
        let depth = u8::try_from(request.lineage.depth.min(u32::from(u8::MAX))).unwrap_or(u8::MAX);
        let parent = request.lineage.immediate_parent_session_id.as_str();
        let mut map = self
            .tree_budgets
            .lock()
            .map_err(|_| "tree budget ledger lock poisoned".to_owned())?;
        let contract = self.tree_budget_contract();
        let ledger = map
            .entry(root.to_owned())
            .or_insert_with(|| BudgetLedger::new(contract));
        // Existing trees keep the contract they were created with; only new
        // roots pick up the latest host config via or_insert_with above.
        ledger
            .reserve_spawn(
                request.id.as_str(),
                Some(parent),
                depth,
                request.run_in_background,
                0,
                0,
            )
            .map_err(|denial: BudgetDenial| format!("tree budget reserve denied: {denial}"))
    }

    fn release_tree_budget(&self, root: &str, reservation: ReservationId) {
        let Ok(mut map) = self.tree_budgets.lock() else {
            tracing::error!(root, "tree budget ledger lock poisoned during release");
            return;
        };
        let Some(ledger) = map.get_mut(root) else {
            return;
        };
        match ledger.release(reservation) {
            ReleaseOutcome::Released | ReleaseOutcome::AlreadyReleased => {}
            ReleaseOutcome::NotFound => {
                tracing::warn!(
                    root,
                    reservation = reservation.0,
                    "budget release for unknown reservation (already gone)"
                );
            }
        }
    }

    fn settle_tree_budget(
        &self,
        root: &str,
        reservation: ReservationId,
        result: &SubagentResult,
    ) {
        let Ok(mut map) = self.tree_budgets.lock() else {
            tracing::error!(root, "tree budget ledger lock poisoned during settle");
            return;
        };
        let Some(ledger) = map.get_mut(root) else {
            return;
        };
        let tokens = if result.output_usage_incomplete {
            None
        } else {
            Some(result.total_tokens_used)
        };
        let tools = if result.output_usage_incomplete {
            None
        } else {
            Some(result.tool_calls)
        };
        match ledger.settle_usage(reservation, tokens, tools) {
            UsageSettlement::Applied
            | UsageSettlement::AlreadySettled
            | UsageSettlement::UnknownUsageNotDebited => {}
            UsageSettlement::NotFound => {
                tracing::warn!(
                    root,
                    reservation = reservation.0,
                    "budget settle for unknown/released reservation"
                );
            }
        }
        // Release after settle so the live node detaches.
        let _ = ledger.release(reservation);
    }

    fn append_authority_event(
        &self,
        root: &str,
        node_id: &str,
        operation_id: &str,
        kind: AuthorityEventKind,
        reservation_id: Option<String>,
    ) {
        let Ok(mut map) = self.tree_authority_logs.lock() else {
            tracing::error!(root, "authority log lock poisoned");
            return;
        };
        let operation_store_dir = self.operation_store_dir.clone();
        let log = map.entry(root.to_owned()).or_insert_with(|| {
            let Some(dir) = operation_store_dir else {
                return TreeAuthorityLog::in_memory();
            };
            let encoded_root: String = root
                .as_bytes()
                .iter()
                .map(|byte| format!("{byte:02x}"))
                .collect();
            TreeAuthorityLog::at_path(
                dir.join("task-tree-authority")
                    .join(format!("{encoded_root}.jsonl")),
            )
        });
        // Cascade cancel + finish_child both try to terminalize; the second is
        // an expected no-op, not an authority breach.
        if log.is_operation_terminal(operation_id) {
            return;
        }
        if let Err(error) = log.append(node_id, operation_id, kind, reservation_id) {
            // Never invent revival; log loudly. Terminal child result handling
            // still goes through durable store fail-closed paths.
            tracing::error!(
                root,
                node_id,
                operation_id,
                %error,
                "authority log refused event"
            );
        }
    }

    /// Record a governed child spawn as a durable operation (create + claim)
    /// after an atomic budget reservation. Fail-closed: spawn must not
    /// proceed without both a ledger reservation and a durable op lease.
    fn record_spawn_operation(&self, request: &SubagentRequest) -> Result<(), String> {
        let root = request.lineage.root_session_id.clone();
        let parent_op = if request.lineage.depth > 1 {
            Some(format!(
                "spawn:{}",
                request.lineage.immediate_parent_session_id
            ))
        } else {
            None
        };
        let op_id = format!("spawn:{}", request.id);
        // 1) Atomic budget reserve first so structural ceilings cannot race.
        let reservation_id = self.reserve_tree_budget(request)?;
        let reservation = format!("ledger:{}", reservation_id.0);
        self.append_authority_event(
            &root,
            &request.id,
            &op_id,
            AuthorityEventKind::SpawnReserved,
            Some(reservation.clone()),
        );

        let mut map = self
            .tree_operations
            .lock()
            .map_err(|_| "tree operation store lock poisoned".to_owned())?;
        let operation_store_dir = self.operation_store_dir.clone();
        let store = map.entry(root.clone()).or_insert_with(|| {
            let Some(dir) = operation_store_dir else {
                return GovernedOperationStore::for_tree(root.clone());
            };
            // Hex preserves every byte of the root id and cannot escape the
            // host-selected directory, unlike using a session id as a path.
            let encoded_root: String = root
                .as_bytes()
                .iter()
                .map(|byte| format!("{byte:02x}"))
                .collect();
            GovernedOperationStore::with_path(
                root.clone(),
                dir.join("task-tree-operations")
                    .join(format!("{encoded_root}.json")),
            )
        });
        if let Err(e) = store.create(
            op_id.clone(),
            request.lineage.immediate_parent_session_id.clone(),
            "child_spawn",
            format!("idem:spawn:{}", request.id),
            Some(reservation.clone()),
            parent_op,
            300,
        ) {
            // Roll back the reservation so a failed durable create cannot
            // permanently consume a live-node slot.
            self.release_tree_budget(&root, reservation_id);
            return Err(format!("durable spawn create failed: {e}"));
        }
        let lease = format!("lease:{}", request.id);
        if let Err(e) = store.claim(
            &op_id,
            &request.lineage.immediate_parent_session_id,
            lease,
            300,
        ) {
            self.release_tree_budget(&root, reservation_id);
            return Err(format!("durable spawn claim failed: {e}"));
        }
        self.append_authority_event(
            &root,
            &request.id,
            &op_id,
            AuthorityEventKind::SpawnClaimed,
            Some(reservation),
        );
        Ok(())
    }

    /// Terminal-settle the durable op for one child (exactly-once budget release).
    /// Fail-closed: missing store/lock or complete/fail Err is returned so the
    /// coordinator can mark the child result failed rather than presenting
    /// success with an unsettled lease/budget.
    fn settle_spawn_operation(
        &self,
        request: &SubagentRequest,
        result: &SubagentResult,
    ) -> Result<(), String> {
        let root = request.lineage.root_session_id.as_str();
        let op_id = format!("spawn:{}", request.id);
        let lease = format!("lease:{}", request.id);
        let owner = request.lineage.immediate_parent_session_id.as_str();
        let map = self
            .tree_operations
            .lock()
            .map_err(|_| "tree operation store lock poisoned during settle".to_owned())?;
        let store = map.get(root).ok_or_else(|| {
            format!("durable settle missing store for root {root} (op {op_id}); fail-closed")
        })?;
        let reservation_raw = store
            .get(&op_id)
            .ok()
            .and_then(|op| op.reservation_id.clone());
        let ledger_res = reservation_raw
            .as_deref()
            .and_then(Self::parse_ledger_reservation);
        // Cascade cancel may already have terminalized the op. Treat Cancelled
        // as an expected settle for a cancelled child; other terminal states on
        // a successful child are a durable authority failure.
        let settle_result = if result.success && !result.cancelled {
            match store.complete(
                &op_id,
                owner,
                &lease,
                format!("receipt://child_complete:{}", request.id),
            ) {
                Ok(_) => Ok(AuthorityEventKind::TerminalSucceeded),
                Err(e) => {
                    // Already cancelled by cascade while child reported success:
                    // still fail-closed so host does not treat work as durable-ok.
                    Err(format!("durable complete failed for {op_id}: {e}"))
                }
            }
        } else {
            match store.fail(
                &op_id,
                owner,
                &lease,
                format!(
                    "receipt://child_terminal:{}:cancelled={}",
                    request.id, result.cancelled
                ),
            ) {
                Ok(_) => Ok(if result.cancelled {
                    AuthorityEventKind::Cancelled
                } else {
                    AuthorityEventKind::TerminalFailed
                }),
                Err(e)
                    if e.code() == "op.cancelled" || e.code() == "op.late_event_after_terminal" =>
                {
                    // Cascade already terminalized; budget released once there.
                    tracing::info!(
                        op_id = %op_id,
                        reason = %e,
                        "durable fail settle observed post-cascade terminal; ok"
                    );
                    Ok(AuthorityEventKind::Cancelled)
                }
                Err(e) => Err(format!("durable fail settle failed for {op_id}: {e}")),
            }
        };
        // Drop the operations lock before touching budget/authority maps.
        drop(map);
        match settle_result {
            Ok(kind) => {
                if let Some(res) = ledger_res {
                    self.settle_tree_budget(root, res, result);
                }
                self.append_authority_event(
                    root,
                    &request.id,
                    &op_id,
                    kind,
                    reservation_raw,
                );
                Ok(())
            }
            Err(e) => {
                // Even on durable failure, release the reservation so the tree
                // does not leak live-node slots forever.
                if let Some(res) = ledger_res {
                    self.release_tree_budget(root, res);
                }
                Err(e)
            }
        }
    }

    /// Record delivery only after a real foreground reply or waiter reply was
    /// accepted by its channel. A buffered completion is intentionally not a
    /// delivery receipt: it still has to be consumed by the parent later.
    fn observe_spawn_delivery(
        &self,
        request: &SubagentRequest,
        observation: SpawnDeliveryObservation,
    ) {
        if matches!(observation, SpawnDeliveryObservation::Undelivered) {
            return;
        }
        let root = request.lineage.root_session_id.as_str();
        let operation_id = format!("spawn:{}", request.id);
        let result = self
            .tree_operations
            .lock()
            .map_err(|_| {
                "tree operation store lock poisoned during delivery observation".to_owned()
            })
            .and_then(|stores| {
                let store = stores.get(root).ok_or_else(|| {
                    format!("durable delivery missing store for root {root} (op {operation_id})")
                })?;
                (match observation {
                    SpawnDeliveryObservation::Delivered => {
                        store.mark_outbox_delivered(&operation_id)
                    }
                    SpawnDeliveryObservation::Uncertain => {
                        store.mark_outbox_uncertain(&operation_id)
                    }
                    SpawnDeliveryObservation::Undelivered => unreachable!("handled above"),
                })
                .map(|_| ())
                .map_err(|error| {
                    format!("durable delivery observation failed for {operation_id}: {error}")
                })
            });
        if let Err(error) = result {
            // The caller already received the child result. Do not rewrite
            // history as a failed child; leave a durable, operator-visible
            // error rather than inventing a delivery receipt.
            tracing::error!(
                subagent_id = %request.id,
                %error,
                "child result delivery observation could not be persisted"
            );
        }
    }

    pub async fn run(mut self) {
        let mut commands_open = true;
        loop {
            if !commands_open
                && self.runs.is_empty()
                && self.validations.is_empty()
                && self.descriptions.is_empty()
                && self.progress.is_empty()
            {
                break;
            }

            let deadline = self.next_deadline();
            tokio::select! {
                biased;
                command = async {
                    match self.control_commands.as_mut() {
                        Some(receiver) => receiver.recv().await,
                        // The legacy constructor has no physical control
                        // lane. Its branch must be genuinely pending, not a
                        // closed receiver that spins or an `expect` evaluated
                        // before `select!` applies its guard.
                        None => std::future::pending::<Option<SubagentEvent>>().await,
                    }
                } => {
                    match command {
                        Some(command) => {
                            self.reap_abandoned_callers();
                            self.handle_command(command);
                        }
                        None => self.control_commands = None,
                    }
                }
                Some(event) = self.internal_rx.recv() => self.handle_internal(event),
                Some((id, output)) = self.runs.next(), if !self.runs.is_empty() => {
                    match output {
                        Ok(output) => self.finish_child(&id, output),
                        Err(_) => self.finish_panicked_child(&id),
                    }
                }
                Some((respond_to, outcome)) = self.validations.next(), if !self.validations.is_empty() => {
                    let _ = respond_to.send(outcome);
                }
                Some((respond_to, outcome)) = self.descriptions.next(), if !self.descriptions.is_empty() => {
                    let _ = respond_to.send(outcome);
                }
                Some((seed, target, progress)) = self.progress.next(), if !self.progress.is_empty() => {
                    self.finish_progress(seed, target, progress);
                }
                command = self.commands.recv(), if commands_open => {
                    match command {
                        Some(command) => {
                            self.reap_abandoned_callers();
                            self.handle_command(command);
                        }
                        None => commands_open = false,
                    }
                }
                _ = sleep_until(deadline), if deadline.is_some() => self.process_deadlines(),
            }
            while self.completed.len() > MAX_COMPLETED_ENTRIES {
                let Some(id) = self.completed_order.pop_front() else {
                    break;
                };
                self.completed.remove(&id);
            }
        }

        self.cancel_all_children();
    }

    fn handle_command(&mut self, command: SubagentEvent) {
        match command {
            SubagentEvent::Spawn(command) => {
                let mut request = *command.request;
                if let Some((parent_lineage, loop_task_id, spawner_cancelled, spawner_owner)) = self
                    .active
                    .values()
                    .find(|child| child.child_session_id == request.parent_session_id)
                    .map(|child| {
                        (
                            child.request.lineage.clone(),
                            child.request.runtime_overrides.loop_task_id.clone(),
                            child.cancellation.is_cancelled(),
                            child.request.owner.clone(),
                        )
                    })
                {
                    if spawner_cancelled {
                        // The parent subagent is being torn down, so its late
                        // child would be orphaned against the closed scope.
                        let id = request.id.clone();
                        let _ = command.result_tx.send(SubagentResult {
                            success: false,
                            cancelled: true,
                            error: Some("parent subagent is being torn down".to_owned()),
                            subagent_id: id.clone(),
                            child_session_id: id,
                            ..Default::default()
                        });
                        return;
                    }
                    // Preserve the direct parent (the child's actual session)
                    // and carry root responsibility separately.  The old
                    // behaviour rewrote parent_session_id to the root here,
                    // flattening every nested tree and making both UI and
                    // shared working-memory attribution ambiguous.
                    request.lineage = SubagentLineage::child_of(
                        &parent_lineage,
                        request.parent_session_id.clone(),
                    );
                    request.runtime_overrides.spawn_depth = Some(request.lineage.depth);
                    request.surface_completion = false;
                    // Nested children keep workflow lineage without losing
                    // their immediate-parent relationship.
                    if !request.owner.is_workflow()
                        && let Some(run_id) = spawner_owner.workflow_run_id()
                    {
                        request.owner = SubagentOwner::workflow(run_id);
                    }
                    if request.runtime_overrides.loop_task_id.is_none() {
                        request.runtime_overrides.loop_task_id = loop_task_id;
                    }
                } else if let Err(reason) = request
                    .lineage
                    .validate_direct_for(&request.parent_session_id)
                {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        error: Some(format!("invalid direct task-tree lineage: {reason}")),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                // `lineage` is the coordinator-validated task-tree identity.
                // Do not let a caller-provided runtime hint make a direct
                // child look like depth zero and thereby bypass depth-based
                // tool and capability ceilings in the shell runner.
                request.runtime_overrides.spawn_depth = Some(request.lineage.depth);
                // Governed-tree children must carry a host-issued manifest
                // identity before they enter the runner.  The model-facing
                // task tool never sets `harness_agent_type`, so this gate is
                // only reachable through an internal host admission path.
                if request.runtime_overrides.harness_agent_type.as_deref() == Some("governed_tree")
                    && request
                        .runtime_overrides
                        .governed_admission
                        .as_ref()
                        .is_none_or(|admission| {
                            admission
                                .validate_for(&request.lineage, &request.id)
                                .is_err()
                        })
                {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        error: Some(
                            "governed-tree spawn requires a valid host-issued admission receipt"
                                .to_owned(),
                        ),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                // Tool-side depth checks are the normal admission path, but
                // this coordinator owns the shared mailbox and must retain a
                // final hard ceiling.  Otherwise a caller that can construct
                // an event directly could grow an unbounded task tree.
                if request.lineage.depth > HARD_MAX_SUBAGENT_DEPTH {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        error: Some(format!(
                            "subagent depth limit exceeded (depth: {}, max: {HARD_MAX_SUBAGENT_DEPTH})",
                            request.lineage.depth
                        )),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                if let Some(conflicting_child_id) = self.governed_write_scope_conflict(&request) {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        error: Some(format!(
                            "governed write scope conflicts with live child '{conflicting_child_id}'"
                        )),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                // Late Task spawn after user Stop (detached TaskTool background).
                if !request.owner.is_workflow()
                    && self
                        .spawn_blocked_sessions
                        .contains(&request.lineage.root_session_id)
                    && !self
                        .expired_tree_roots
                        .contains(&request.lineage.root_session_id)
                {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        cancelled: true,
                        error: Some("parent session is stopped".to_owned()),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                let root_session_id = request.lineage.root_session_id.clone();
                if self.exhausted_tree_token_budgets.contains(&root_session_id) {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        cancelled: true,
                        error: Some("subagent tree total-token budget exhausted".to_owned()),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                if self
                    .exhausted_tree_tool_call_budgets
                    .contains(&root_session_id)
                {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        cancelled: true,
                        error: Some("subagent tree tool-call budget exhausted".to_owned()),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                if self.tree_usage_incomplete_roots.contains(&root_session_id) {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        cancelled: true,
                        error: Some(
                            "subagent tree token usage is incomplete; admission closed".to_owned(),
                        ),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                if self.expired_tree_roots.contains(&root_session_id) {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        cancelled: true,
                        error: Some("subagent tree wall-time budget exhausted".to_owned()),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                let started_at = *self
                    .tree_started_at
                    .entry(root_session_id.clone())
                    .or_insert_with(tokio::time::Instant::now);
                if tokio::time::Instant::now() >= started_at + self.config.tree_wall_time_budget {
                    self.expire_tree(&root_session_id);
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        cancelled: true,
                        error: Some("subagent tree wall-time budget exhausted".to_owned()),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                let live_in_tree = self.live_children_in_tree(&request.lineage.root_session_id);
                if live_in_tree >= MAX_LIVE_SUBAGENTS_PER_TREE {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        error: Some(format!(
                            "subagent tree concurrency limit reached (max: {MAX_LIVE_SUBAGENTS_PER_TREE})"
                        )),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                let live_for_parent = self.live_children_for_parent(&request.parent_session_id);
                if live_for_parent >= self.config.max_live_children_per_parent {
                    let id = request.id.clone();
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        error: Some(format!(
                            "subagent parent fan-out limit reached (max: {})",
                            self.config.max_live_children_per_parent
                        )),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                let id = request.id.clone();
                if self.pending.contains_key(&id)
                    || self.active.contains_key(&id)
                    || self.completed.contains_key(&id)
                {
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        error: Some(format!("Subagent id '{id}' already exists")),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                let cancellation = request.cancel_token.clone();
                let handle_only = request.run_in_background;
                let foreground_deadline = (!request.run_in_background
                    && !request.await_to_completion)
                    .then(|| tokio::time::Instant::now() + self.config.foreground_budget);
                // Durable op first: never run a child without a lease record.
                if let Err(err) = self.record_spawn_operation(&request) {
                    let _ = command.result_tx.send(SubagentResult {
                        success: false,
                        error: Some(err),
                        subagent_id: id.clone(),
                        child_session_id: id,
                        ..Default::default()
                    });
                    return;
                }
                self.pending.insert(
                    id.clone(),
                    PendingChild {
                        request: request.clone(),
                        started_at: std::time::Instant::now(),
                        cancellation: cancellation.clone(),
                        spawn_reply: Some(command.result_tx),
                        foreground_deadline,
                        handle_only,
                        foreground_delivery_uncertain: false,
                        explicitly_killed: false,
                    },
                );
                self.running_count_changed();
                let reporter = ChildReporter {
                    subagent_id: id.clone(),
                    tx: self.internal_tx.clone(),
                };
                self.runs.push(TaggedFuture {
                    subagent_id: id,
                    future: Box::pin(
                        std::panic::AssertUnwindSafe(self.runner.run(ChildRunRequest {
                            request,
                            cancellation,
                            reporter,
                        }))
                        .catch_unwind(),
                    ),
                });
            }
            SubagentEvent::Query(query) => {
                self.handle_query(
                    query.subagent_id,
                    query.parent_session_id,
                    query.block,
                    query.timeout_ms,
                    query.respond_to,
                );
            }
            SubagentEvent::RegisterRecoveredTerminal(request) => {
                self.register_recovered_terminal(request);
            }
            SubagentEvent::Cancel(request) => match request.target {
                SubagentCancelTarget::SubagentId(id) => {
                    let outcome = self.cancel_one(&id, request.parent_session_id.as_deref(), true);
                    let _ = request.respond_to.send(outcome);
                }
                SubagentCancelTarget::ParentPromptId(prompt_id) => {
                    self.cancel_parent_prompt(&prompt_id, request.parent_session_id.as_deref());
                    let _ = request.respond_to.send(SubagentCancelOutcome::Cancelled);
                }
                SubagentCancelTarget::ParentSession => {
                    let outcome = self.cancel_parent_session(request.parent_session_id.as_deref());
                    let _ = request.respond_to.send(outcome);
                }
                SubagentCancelTarget::WorkflowRunId(run_id) => {
                    self.cancel_workflow_children(&run_id, request.parent_session_id.as_deref());
                    if workflow_outstanding(&self.pending, &self.active, &run_id) == 0 {
                        let _ = request.respond_to.send(SubagentCancelOutcome::Cancelled);
                    } else {
                        self.workflow_cancel_waiters
                            .entry(run_id)
                            .or_default()
                            .push(request.respond_to);
                    }
                }
            },
            SubagentEvent::ListActive(request) => {
                let summaries = self
                    .active
                    .values()
                    .filter(|child| {
                        child.request.parent_session_id == request.parent_session_id
                            && !child.request.owner.is_workflow()
                    })
                    .map(active_summary)
                    .collect();
                let _ = request.respond_to.send(summaries);
            }
            SubagentEvent::ListRunning(request) => {
                self.handle_list_running(request.parent_session_id, request.respond_to);
            }
            SubagentEvent::Completions(request) => {
                let (owned, foreign): (Vec<_>, Vec<_>) =
                    std::mem::take(&mut self.pending_completions)
                        .into_iter()
                        .partition(|completion| {
                            request
                                .parent_session_id
                                .as_ref()
                                .is_none_or(|id| completion.parent_session_id == *id)
                        });
                self.pending_completions = foreign;
                let completions = owned
                    .into_iter()
                    .map(|completion| completion.summary)
                    .filter(|summary| !request.suppress_ids.contains(&summary.subagent_id))
                    .collect();
                let _ = request.respond_to.send(completions);
            }
            SubagentEvent::TeardownSession { parent_session_id } => {
                self.recovered_terminals
                    .retain(|_, terminal| terminal.parent_session_id != parent_session_id);
                self.tree_started_at.remove(&parent_session_id);
                self.expired_tree_roots.remove(&parent_session_id);
                self.tree_total_tokens_used.remove(&parent_session_id);
                self.tree_usage_incomplete_roots.remove(&parent_session_id);
                self.exhausted_tree_token_budgets.remove(&parent_session_id);
                self.tree_tool_calls_used.remove(&parent_session_id);
                self.exhausted_tree_tool_call_budgets
                    .remove(&parent_session_id);
                self.pending_completions.retain(|completion| {
                    completion.parent_session_id != parent_session_id
                        && completion.root_session_id != parent_session_id
                        && !completion
                            .lineage_path
                            .iter()
                            .any(|id| id == &parent_session_id)
                });
                self.spawn_blocked_sessions.remove(&parent_session_id);
                self.teardown_session_children(&parent_session_id);
            }
            SubagentEvent::OpenSpawnAdmission { parent_session_id } => {
                self.spawn_blocked_sessions.remove(&parent_session_id);
            }
            SubagentEvent::Outstanding(request) => {
                // Reap again here so turn-freeze / Outstanding polls see
                // ParentGone even if no other command woke the actor first.
                self.reap_abandoned_callers();
                let mut live_ids: Vec<_> = self
                    .pending
                    .values()
                    .filter(|child| {
                        child.request.parent_session_id == request.parent_session_id
                            && child.request.parent_prompt_id.as_deref() == Some(&request.prompt_id)
                            && !child.request.owner.is_workflow()
                            && !child.handle_only
                    })
                    .map(|child| child.request.id.clone())
                    .chain(
                        self.active
                            .values()
                            .filter(|child| {
                                child.request.parent_session_id == request.parent_session_id
                                    && child.request.parent_prompt_id.as_deref()
                                        == Some(&request.prompt_id)
                                    && !child.request.owner.is_workflow()
                                    // Definition-declared background children are
                                    // background for accounting even while the
                                    // spawning tool block-awaits them.
                                    && !child.handle_only
                                    && !child.definition_background
                            })
                            .map(|child| child.request.id.clone()),
                    )
                    .collect();
                live_ids.sort();
                let background_live = self.pending.values().any(|child| {
                    child.request.parent_session_id == request.parent_session_id
                        && child.request.parent_prompt_id.as_deref() == Some(&request.prompt_id)
                        && !child.request.owner.is_workflow()
                        && child.handle_only
                }) || self.active.values().any(|child| {
                    child.request.parent_session_id == request.parent_session_id
                        && child.request.parent_prompt_id.as_deref() == Some(&request.prompt_id)
                        && !child.request.owner.is_workflow()
                        && (child.handle_only || child.definition_background)
                });
                let scope =
                    PromptScope::new(request.parent_session_id.clone(), request.prompt_id.clone());
                let _ = request.respond_to.send(SubagentOutstandingReply {
                    live_ids,
                    background_live,
                    subagent_usage_not_applied: self.usage_not_applied_prompts.contains(&scope),
                });
            }
            SubagentEvent::ClearUsageNotApplied(request) => {
                self.usage_not_applied_prompts.remove(&PromptScope::new(
                    request.parent_session_id,
                    request.prompt_id,
                ));
            }
            SubagentEvent::MarkUsageNotApplied(request) => {
                self.usage_not_applied_prompts.insert(PromptScope::new(
                    request.parent_session_id,
                    request.prompt_id,
                ));
                let _ = request.respond_to.send(());
            }
            SubagentEvent::RegistryCounts(request) => {
                let _ = request.respond_to.send(SubagentRegistryCounts {
                    pending: self.pending.len(),
                    active: self.active.len(),
                    completed: self.completed.len(),
                });
            }
            SubagentEvent::Inspect(request) => {
                self.handle_inspect(
                    request.subagent_id,
                    request.parent_session_id,
                    request.respond_to,
                );
            }
            SubagentEvent::SpawnedRefs(request) => {
                let mut refs: Vec<_> = self
                    .active
                    .values()
                    .filter(|child| {
                        child.request.parent_session_id == request.parent_session_id
                            && child.request.parent_prompt_id.as_deref() == Some(&request.prompt_id)
                    })
                    .map(|child| SpawnedSubagentRef {
                        subagent_id: child.request.id.clone(),
                        child_session_id: child.child_session_id.clone(),
                        subagent_type: child.request.subagent_type.clone(),
                        description: child.request.description.clone(),
                        persona: child.persona.clone(),
                        resumed_from: child.resumed_from.clone(),
                    })
                    .chain(
                        self.completed
                            .values()
                            .filter(|child| {
                                child.request.parent_session_id == request.parent_session_id
                                    && child.request.parent_prompt_id.as_deref()
                                        == Some(&request.prompt_id)
                            })
                            .map(|child| SpawnedSubagentRef {
                                subagent_id: child.request.id.clone(),
                                child_session_id: child.child_session_id.clone(),
                                subagent_type: child.request.subagent_type.clone(),
                                description: child.request.description.clone(),
                                persona: child.persona.clone(),
                                resumed_from: child.resumed_from.clone(),
                            }),
                    )
                    .collect();
                refs.sort_by(|a, b| a.subagent_id.cmp(&b.subagent_id));
                let _ = request.respond_to.send(refs);
            }
            SubagentEvent::ValidateType(request) => {
                self.validations.push(ReplyFuture {
                    future: Box::pin(
                        self.runner
                            .validate_type(request.subagent_type, request.parent_session_id),
                    ),
                    respond_to: Some(request.respond_to),
                });
            }
            SubagentEvent::DescribeType(request) => {
                self.descriptions.push(ReplyFuture {
                    future: Box::pin(self.runner.describe_type(
                        request.subagent_type,
                        request.harness_agent_type,
                        request.parent_session_id,
                    )),
                    respond_to: Some(request.respond_to),
                });
            }
            SubagentEvent::LoopUnitActive(request) => {
                let is_active = self.pending.values().any(|child| {
                    child.request.runtime_overrides.loop_task_id.as_deref()
                        == Some(&request.task_id)
                }) || self.active.values().any(|child| {
                    child.request.runtime_overrides.loop_task_id.as_deref()
                        == Some(&request.task_id)
                });
                let _ = request.respond_to.send(is_active);
            }
        }
    }

    fn register_recovered_terminal(&mut self, request: SubagentRecoveredTerminalRequest) {
        let subagent_id = request.snapshot.subagent_id.clone();
        if request.snapshot.is_running() {
            tracing::warn!(%subagent_id, "Ignoring non-terminal recovered subagent snapshot");
            return;
        }
        if self.active.contains_key(&subagent_id)
            || self.pending.contains_key(&subagent_id)
            || self.completed.contains_key(&subagent_id)
        {
            tracing::warn!(%subagent_id, "Ignoring recovered terminal that conflicts with a live subagent");
            return;
        }
        self.recovered_terminals.insert(
            subagent_id,
            RecoveredTerminal {
                parent_session_id: request.parent_session_id,
                snapshot: request.snapshot,
            },
        );
    }

    fn live_children_in_tree(&self, root_session_id: &str) -> usize {
        self.pending
            .values()
            .filter(|child| child.request.lineage.root_session_id == root_session_id)
            .count()
            + self
                .active
                .values()
                .filter(|child| child.request.lineage.root_session_id == root_session_id)
                .count()
    }

    fn live_children_for_parent(&self, parent_session_id: &str) -> usize {
        self.pending
            .values()
            .filter(|child| child.request.parent_session_id == parent_session_id)
            .count()
            + self
                .active
                .values()
                .filter(|child| child.request.parent_session_id == parent_session_id)
                .count()
    }

    /// A host-issued non-empty write scope is exclusive while its child is
    /// pending or active.  We use lexical path containment here deliberately:
    /// receipt validation rejects parent-directory escapes, and resolving
    /// symlinks at this layer would make the admission result depend on a
    /// mutable filesystem after the host signed it.
    ///
    /// Legacy children and governed receipts with an empty scope retain their
    /// existing workspace policy; only a host that explicitly narrows both
    /// children opts into this conflict gate.
    fn governed_write_scope_conflict(&self, request: &SubagentRequest) -> Option<String> {
        let candidate = request.runtime_overrides.governed_admission.as_ref()?;
        if candidate.write_scope_roots.is_empty() {
            return None;
        }

        self.pending
            .values()
            .map(|child| &child.request)
            .chain(self.active.values().map(|child| &child.request))
            .find(|existing| {
                existing.lineage.root_session_id == request.lineage.root_session_id
                    && existing
                        .runtime_overrides
                        .governed_admission
                        .as_ref()
                        .is_some_and(|admission| {
                            !admission.write_scope_roots.is_empty()
                                && candidate.write_scope_roots.iter().any(|candidate_root| {
                                    admission.write_scope_roots.iter().any(|existing_root| {
                                        candidate_root.starts_with(existing_root)
                                            || existing_root.starts_with(candidate_root)
                                    })
                                })
                        })
            })
            .map(|existing| existing.id.clone())
    }

    fn handle_internal(&mut self, event: InternalEvent<R::Control>) {
        match event {
            InternalEvent::Started {
                subagent_id,
                child,
                respond_to,
            } => {
                let Some(pending) = self.pending.remove(&subagent_id) else {
                    let _ = respond_to.send(false);
                    return;
                };
                if pending.cancellation.is_cancelled() {
                    self.pending.insert(subagent_id, pending);
                    let _ = respond_to.send(false);
                    return;
                }
                self.active.insert(
                    subagent_id,
                    ActiveChild {
                        request: pending.request,
                        started_at: pending.started_at,
                        cancellation: pending.cancellation,
                        spawn_reply: pending.spawn_reply,
                        foreground_deadline: pending.foreground_deadline,
                        handle_only: pending.handle_only,
                        foreground_delivery_uncertain: pending.foreground_delivery_uncertain,
                        definition_background: child.definition_background,
                        explicitly_killed: pending.explicitly_killed,
                        child_session_id: child.child_session_id,
                        persona: child.persona,
                        resumed_from: child.resumed_from,
                        child_cwd: child.child_cwd,
                        worktree_path: child.worktree_path,
                        effective_model_id: child.effective_model_id,
                        control: child.control,
                    },
                );
                let _ = respond_to.send(true);
            }
            InternalEvent::ResumeSource {
                source_id,
                parent_session_id,
                respond_to,
            } => {
                let source_is_active =
                    self.pending
                        .get(&source_id)
                        .is_some_and(|child| child.request.parent_session_id == parent_session_id)
                        || self.active.get(&source_id).is_some_and(|child| {
                            child.request.parent_session_id == parent_session_id
                        });
                let lookup = if source_is_active {
                    SubagentResumeLookup::Active
                } else if let Some(child) = self.completed.get(&source_id)
                    && child.request.parent_session_id == parent_session_id
                {
                    SubagentResumeLookup::Completed(SubagentResumeSource {
                        subagent_id: child.request.id.clone(),
                        child_session_id: child.child_session_id.clone(),
                        child_cwd: child.child_cwd.clone(),
                        worktree_path: child.worktree_path.clone(),
                        snapshot_ref: child.snapshot_ref.clone(),
                        subagent_type: child.request.subagent_type.clone(),
                        persona: child.persona.clone(),
                        model_id: Some(child.effective_model_id.clone()),
                        context_manifest_hash: child
                            .request
                            .runtime_overrides
                            .context_manifest_hash
                            .clone(),
                    })
                } else {
                    SubagentResumeLookup::Missing
                };
                let _ = respond_to.send(lookup);
            }
        }
    }

    fn finish_child(&mut self, id: &str, output: ChildRunOutput<R::CompletionData>) {
        let record = if let Some(child) = self.active.remove(id) {
            ChildRecord::Active(child)
        } else if let Some(child) = self.pending.remove(id) {
            ChildRecord::Pending(child)
        } else {
            return;
        };

        let request = record.request().clone();
        // Settle durable lease/budget before host presentation so cancel
        // cascade and terminal completion share one authority boundary.
        let mut result = output.result;
        if let Err(err) = self.settle_spawn_operation(&request, &result) {
            tracing::error!(
                subagent_id = %request.id,
                error = %err,
                "durable operation settle failed; failing child result"
            );
            // Fail-closed: never present success when durable settle did not hold.
            result.success = false;
            result.error = Some(err);
        }
        // Rebuild output shell around the (possibly failed) settled result.
        let output = ChildRunOutput {
            result: result.clone(),
            completion_data: output.completion_data,
            snapshot_ref: output.snapshot_ref,
        };
        self.record_tree_token_usage(&request.lineage.root_session_id, &output.result);
        self.record_tree_tool_call_usage(&request.lineage.root_session_id, &output.result);
        let explicitly_killed = record.explicitly_killed();
        let (
            started_at,
            child_session_id,
            persona,
            resumed_from,
            child_cwd,
            worktree_path,
            effective_model_id,
            mut spawn_reply,
            mut handle_only,
            foreground_delivery_uncertain,
        ) = match record {
            ChildRecord::Pending(child) => (
                child.started_at,
                output.result.child_session_id.clone(),
                child.request.runtime_overrides.persona.clone(),
                child.request.resume_from.clone(),
                child.request.cwd.clone().unwrap_or_default(),
                output.result.worktree_path.clone(),
                String::new(),
                child.spawn_reply,
                child.handle_only,
                child.foreground_delivery_uncertain,
            ),
            ChildRecord::Active(child) => (
                child.started_at,
                child.child_session_id,
                child.persona,
                child.resumed_from,
                child.child_cwd,
                child.worktree_path,
                child.effective_model_id,
                child.spawn_reply,
                child.handle_only,
                child.foreground_delivery_uncertain,
            ),
        };

        let persisted_output_ref = self.runner.persisted_output_ref(&output.completion_data);
        let mut completed = CompletedChild {
            request: request.clone(),
            started_at,
            child_session_id,
            persona,
            resumed_from,
            child_cwd,
            worktree_path,
            snapshot_ref: output.snapshot_ref,
            persisted_output_ref,
            effective_model_id,
            result: output.result.clone(),
        };
        let snapshot = completed_snapshot(&completed, None);

        let mut waiter_delivered = false;
        for waiter in self.waiters.remove(id).unwrap_or_default() {
            waiter_delivered |= waiter.respond_to.send(Some(snapshot.clone())).is_ok();
        }

        let mut foreground_delivered = false;
        let mut direct_reply_delivered = false;
        let mut direct_reply_receiver_closed = false;
        if let Some(respond_to) = spawn_reply.take() {
            let sent = respond_to.send(output.result.clone()).is_ok();
            direct_reply_delivered = sent;
            direct_reply_receiver_closed = !sent;
            if !handle_only {
                foreground_delivered = sent;
                handle_only = !sent;
            }
        } else if !handle_only {
            handle_only = true;
        }

        if self.config.buffer_completions
            && request.surface_completion
            && !request.owner.is_workflow()
        {
            let mut summary = completion_summary(&request, &output.result);
            if let Some(cap) = self.config.buffered_completion_output_cap {
                summary.output = super::cap_completion_output(&summary.output, cap);
            }
            self.pending_completions.push(BufferedCompletion {
                parent_session_id: request.parent_session_id.clone(),
                root_session_id: request.lineage.root_session_id.clone(),
                lineage_path: request.lineage.lineage_path.clone(),
                summary,
            });
            // Bound the buffer (drop oldest): sessions unloaded without a
            // TeardownSession cannot grow it unboundedly.
            const MAX_PENDING_COMPLETIONS: usize = 256;
            if self.pending_completions.len() > MAX_PENDING_COMPLETIONS {
                let excess = self.pending_completions.len() - MAX_PENDING_COMPLETIONS;
                self.pending_completions.drain(..excess);
            }
        }
        if completed.persisted_output_ref.is_some() {
            completed.result.output = Arc::from("");
        }

        let should_surface = request.surface_completion
            && handle_only
            && !output.result.cancelled
            && !waiter_delivered
            && !explicitly_killed;
        let disposition = CompletionDisposition {
            foreground_delivered,
            backgrounded: handle_only,
            waiter_delivered,
            explicitly_killed,
            should_surface,
        };
        let delivery_observation =
            if direct_reply_delivered || foreground_delivered || waiter_delivered {
                SpawnDeliveryObservation::Delivered
            } else if direct_reply_receiver_closed || foreground_delivery_uncertain {
                // The foreground operation completed, but its only direct receipt
                // channel was closed. A later UI surface may show a summary, but
                // that is not evidence that this terminal result was delivered.
                SpawnDeliveryObservation::Uncertain
            } else {
                SpawnDeliveryObservation::Undelivered
            };
        self.observe_spawn_delivery(&request, delivery_observation);
        self.completed.insert(id.to_owned(), completed);
        self.completed_order.push_back(id.to_owned());
        self.running_count_changed();
        let workflow_run_id = request.owner.workflow_run_id().map(str::to_owned);
        self.runner.on_completed(ChildCompletion {
            request,
            result: output.result,
            completion_data: output.completion_data,
            disposition,
        });
        if let Some(run_id) = workflow_run_id {
            self.resolve_workflow_cancel_waiters(&run_id);
        }
    }

    fn finish_panicked_child(&mut self, id: &str) {
        let request = self
            .active
            .get(id)
            .map(|child| child.request.clone())
            .or_else(|| self.pending.get(id).map(|child| child.request.clone()));
        let Some(request) = request else {
            return;
        };
        tracing::error!(subagent_id = id, "subagent child runner panicked");
        self.finish_child(
            id,
            ChildRunOutput {
                result: SubagentResult {
                    success: false,
                    error: Some("Subagent runtime panicked".to_owned()),
                    subagent_id: request.id.clone(),
                    child_session_id: request.id,
                    ..Default::default()
                },
                completion_data: R::CompletionData::default(),
                snapshot_ref: None,
            },
        );
    }

    fn cancel_one(
        &mut self,
        id: &str,
        parent_session_id: Option<&str>,
        explicit: bool,
    ) -> SubagentCancelOutcome {
        if let Some(child) = self.active.get_mut(id)
            && belongs_to_session(&child.request, parent_session_id)
        {
            child.explicitly_killed |= explicit;
            child.cancellation.cancel();
            child.control.cancel();
            return SubagentCancelOutcome::Cancelled;
        }
        if let Some(child) = self.pending.get_mut(id)
            && belongs_to_session(&child.request, parent_session_id)
        {
            child.explicitly_killed |= explicit;
            child.cancellation.cancel();
            return SubagentCancelOutcome::Cancelled;
        }
        if let Some(child) = self.completed.get(id)
            && belongs_to_session(&child.request, parent_session_id)
        {
            return SubagentCancelOutcome::AlreadyFinished {
                status: child.result.status().to_owned(),
            };
        }
        SubagentCancelOutcome::NotFound
    }

    fn cancel_parent_prompt(&mut self, parent_prompt_id: &str, parent_session_id: Option<&str>) {
        for child in self.active.values() {
            if child.request.parent_prompt_id.as_deref() == Some(parent_prompt_id)
                && belongs_to_session(&child.request, parent_session_id)
            {
                child.cancellation.cancel();
                child.control.cancel();
            }
        }
        for child in self.pending.values() {
            if child.request.parent_prompt_id.as_deref() == Some(parent_prompt_id)
                && belongs_to_session(&child.request, parent_session_id)
            {
                child.cancellation.cancel();
            }
        }
    }

    fn teardown_session_children(&mut self, parent_session_id: &str) {
        let mut cancelled = 0;
        for child in self.active.values_mut() {
            if belongs_to_teardown_tree(&child.request, parent_session_id) {
                // Parent is gone: do not rebuffer this completion for a later
                // resume of the same session id.
                child.request.surface_completion = false;
                child.cancellation.cancel();
                child.control.cancel();
                cancelled += 1;
            }
        }
        for child in self.pending.values_mut() {
            if belongs_to_teardown_tree(&child.request, parent_session_id) {
                child.request.surface_completion = false;
                child.cancellation.cancel();
                cancelled += 1;
            }
        }
        if cancelled > 0 {
            tracing::info!(
                parent_session_id,
                cancelled,
                "cancelled subagents on session teardown"
            );
        }
    }

    /// All non-workflow children for the parent session (user Stop / Esc).
    ///
    /// Requires a concrete session id — unbound (`None`) is rejected so a
    /// wildcard cannot cancel every session on a shared coordinator.
    fn cancel_parent_session(&mut self, parent_session_id: Option<&str>) -> SubagentCancelOutcome {
        let Some(parent_session_id) = parent_session_id else {
            return SubagentCancelOutcome::NotFound;
        };
        self.spawn_blocked_sessions
            .insert(parent_session_id.to_owned());
        for child in self.active.values() {
            if child.request.lineage.root_session_id == parent_session_id
                && !child.request.owner.is_workflow()
            {
                child.cancellation.cancel();
                child.control.cancel();
            }
        }
        for child in self.pending.values() {
            if child.request.lineage.root_session_id == parent_session_id
                && !child.request.owner.is_workflow()
            {
                child.cancellation.cancel();
            }
        }
        SubagentCancelOutcome::Cancelled
    }

    fn cancel_workflow_children(&mut self, run_id: &str, parent_session_id: Option<&str>) {
        for child in self.active.values() {
            if child.request.owner.workflow_run_id() == Some(run_id)
                && belongs_to_session(&child.request, parent_session_id)
            {
                child.cancellation.cancel();
                child.control.cancel();
            }
        }
        for child in self.pending.values() {
            if child.request.owner.workflow_run_id() == Some(run_id)
                && belongs_to_session(&child.request, parent_session_id)
            {
                child.cancellation.cancel();
            }
        }
    }

    fn resolve_workflow_cancel_waiters(&mut self, run_id: &str) {
        if workflow_outstanding(&self.pending, &self.active, run_id) != 0 {
            return;
        }
        for respond_to in self
            .workflow_cancel_waiters
            .remove(run_id)
            .unwrap_or_default()
        {
            let _ = respond_to.send(SubagentCancelOutcome::Cancelled);
        }
    }

    fn next_deadline(&self) -> Option<tokio::time::Instant> {
        self.pending
            .values()
            .filter_map(|child| child.foreground_deadline)
            .chain(
                self.active
                    .values()
                    .filter_map(|child| child.foreground_deadline),
            )
            .chain(
                self.waiters
                    .values()
                    .flatten()
                    .map(|waiter| waiter.deadline),
            )
            .chain(
                self.tree_started_at
                    .iter()
                    .filter(|(root, _)| !self.expired_tree_roots.contains(*root))
                    .map(|(_, started_at)| *started_at + self.config.tree_wall_time_budget),
            )
            .min()
    }

    fn reap_abandoned_callers(&mut self) {
        for child in self.pending.values_mut() {
            background_if_caller_gone(child);
        }
        for child in self.active.values_mut() {
            background_if_caller_gone(child);
        }
    }

    fn process_deadlines(&mut self) {
        self.reap_abandoned_callers();
        let now = tokio::time::Instant::now();
        for child in self.pending.values_mut() {
            background_at_deadline(child, now, self.config.foreground_budget);
        }
        for child in self.active.values_mut() {
            background_at_deadline(child, now, self.config.foreground_budget);
        }

        let expired_roots: Vec<_> = self
            .tree_started_at
            .iter()
            .filter_map(|(root, started_at)| {
                (*started_at + self.config.tree_wall_time_budget <= now).then(|| root.clone())
            })
            .collect();
        for root in expired_roots {
            self.expire_tree(&root);
        }

        let ids: Vec<_> = self.waiters.keys().cloned().collect();
        for id in ids {
            let waiters = self.waiters.remove(&id).unwrap_or_default();
            let (due, live): (Vec<_>, Vec<_>) = waiters
                .into_iter()
                .partition(|waiter| waiter.deadline <= now);
            if !live.is_empty() {
                self.waiters.insert(id.clone(), live);
            }
            for waiter in due {
                if waiter.respond_to.is_closed() {
                    continue;
                }
                if self.active.contains_key(&id) {
                    self.queue_active_progress(&id, ProgressTarget::Query(waiter.respond_to));
                } else {
                    let _ = waiter.respond_to.send(self.ready_snapshot(&id));
                }
            }
        }
    }

    fn running_count_changed(&self) {
        self.runner
            .running_count_changed(self.pending.len() + self.active.len());
    }

    fn cancel_all_children(&self) {
        for child in self.active.values() {
            child.cancellation.cancel();
            child.control.cancel();
        }
        for child in self.pending.values() {
            child.cancellation.cancel();
        }
    }

    /// The wall-time limit is a whole-tree stop, so it includes workflow
    /// children that user Stop deliberately leaves to their owner policy.
    fn expire_tree(&mut self, root_session_id: &str) {
        if !self.expired_tree_roots.insert(root_session_id.to_owned()) {
            return;
        }
        self.spawn_blocked_sessions
            .insert(root_session_id.to_owned());
        if let Ok(mut map) = self.tree_budgets.lock()
            && let Some(ledger) = map.get_mut(root_session_id)
        {
            ledger.expire_tree();
        }
        for child in self.active.values() {
            if child.request.lineage.root_session_id == root_session_id {
                child.cancellation.cancel();
                child.control.cancel();
            }
        }
        for child in self.pending.values() {
            if child.request.lineage.root_session_id == root_session_id {
                child.cancellation.cancel();
            }
        }
        tracing::warn!(
            root_session_id,
            "subagent tree wall-time budget exhausted; cancelled tree"
        );
    }

    fn record_tree_token_usage(&mut self, root_session_id: &str, result: &SubagentResult) {
        if result.output_usage_incomplete {
            self.tree_usage_incomplete_roots
                .insert(root_session_id.to_owned());
            self.cancel_tree(root_session_id);
            tracing::warn!(
                root_session_id,
                "subagent tree token usage incomplete; closed further admission"
            );
            return;
        }
        let Some(limit) = self.config.tree_total_token_budget else {
            return;
        };
        let used = {
            let used = self
                .tree_total_tokens_used
                .entry(root_session_id.to_owned())
                .or_default();
            *used = used.saturating_add(result.total_tokens_used);
            *used
        };
        if used >= limit {
            self.exhausted_tree_token_budgets
                .insert(root_session_id.to_owned());
            self.cancel_tree(root_session_id);
            tracing::warn!(
                root_session_id,
                used,
                limit,
                "subagent tree token budget exhausted"
            );
        }
    }

    fn cancel_tree(&mut self, root_session_id: &str) {
        for child in self.active.values() {
            if child.request.lineage.root_session_id == root_session_id {
                child.cancellation.cancel();
                child.control.cancel();
            }
        }
        for child in self.pending.values() {
            if child.request.lineage.root_session_id == root_session_id {
                child.cancellation.cancel();
            }
        }
        // Cascade durable operation cancel: revoke leases and release budgets
        // exactly once for every recorded spawn under this root.
        let map = match self.tree_operations.lock() {
            Ok(map) => map,
            Err(e) => {
                tracing::error!(
                    root_session_id,
                    error = %e,
                    "tree operation store lock poisoned during cancel_tree cascade"
                );
                return;
            }
        };
        let root_ops: Vec<String> = map
            .get(root_session_id)
            .map(|store| {
                store
                    .list()
                    .into_iter()
                    .filter(|op| op.parent_operation_id.is_none())
                    .map(|op| op.operation_id)
                    .collect()
            })
            .unwrap_or_default();
        let mut released_reservations: Vec<(String, Option<String>)> = Vec::new();
        if let Some(store) = map.get(root_session_id) {
            let targets = if root_ops.is_empty() {
                store
                    .list()
                    .into_iter()
                    .map(|op| op.operation_id)
                    .collect::<Vec<_>>()
            } else {
                root_ops
            };
            for op_id in targets {
                match store.cancel_cascade_from_root(root_session_id, &op_id) {
                    Ok(ops) => {
                        for op in ops {
                            released_reservations
                                .push((op.operation_id.clone(), op.reservation_id.clone()));
                        }
                    }
                    Err(e) => {
                        // Do not hide authority failures: operator/host must see
                        // lease/budget cascade denials in logs.
                        tracing::error!(
                            root_session_id,
                            op_id = %op_id,
                            error = %e,
                            "durable cancel_cascade_from_root failed"
                        );
                    }
                }
            }
        }
        drop(map);
        // Mirror store budget_released into the atomic ledger + authority log.
        for (op_id, reservation_raw) in released_reservations {
            if let Some(res) = reservation_raw
                .as_deref()
                .and_then(Self::parse_ledger_reservation)
            {
                self.release_tree_budget(root_session_id, res);
            }
            let node_id = op_id
                .strip_prefix("spawn:")
                .unwrap_or(op_id.as_str())
                .to_owned();
            self.append_authority_event(
                root_session_id,
                &node_id,
                &op_id,
                AuthorityEventKind::Cancelled,
                reservation_raw,
            );
        }
    }

    fn record_tree_tool_call_usage(&mut self, root_session_id: &str, result: &SubagentResult) {
        let Some(limit) = self.config.tree_tool_call_budget else {
            return;
        };
        let used = {
            let used = self
                .tree_tool_calls_used
                .entry(root_session_id.to_owned())
                .or_default();
            *used = used.saturating_add(u64::from(result.tool_calls));
            *used
        };
        if used >= limit {
            self.exhausted_tree_tool_call_budgets
                .insert(root_session_id.to_owned());
            self.cancel_tree(root_session_id);
            tracing::warn!(
                root_session_id,
                used,
                limit,
                "subagent tree tool-call budget exhausted"
            );
        }
    }
}

fn belongs_to_session(request: &SubagentRequest, parent_session_id: Option<&str>) -> bool {
    parent_session_id
        .is_none_or(|id| request.parent_session_id == id || request.lineage.root_session_id == id)
}

/// Whether `session_id` owns this child or is an ancestor of its direct
/// parent. Teardown is stronger than a normal parent query: once a session is
/// gone, every descendant must lose its runtime and buffered completion.
fn belongs_to_teardown_tree(request: &SubagentRequest, session_id: &str) -> bool {
    request.parent_session_id == session_id
        || request.lineage.root_session_id == session_id
        || request
            .lineage
            .lineage_path
            .iter()
            .any(|id| id == session_id)
}

impl<R: ChildRunner> Drop for SubagentCoordinator<R> {
    fn drop(&mut self) {
        self.cancel_all_children();
    }
}

#[cfg(test)]
#[path = "coordinator_tests.rs"]
mod tests;
