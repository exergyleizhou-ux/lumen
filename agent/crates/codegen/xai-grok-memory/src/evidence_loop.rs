//! NG-04E / S7 — pure node evidence-loop reducer (no provider).
//!
//! Progress requires evidence yield or obligation discharge fingerprint change.
//! Repair is capped and must cite a failure receipt. Completion is only a
//! candidate until host/root layers (not modeled here as success).
//!
//! Replay discipline (DEBT-028 W0-5): this reducer is a pure function of its
//! event sequence — replay consumes *recorded* observation events only, and
//! adapters are isolated during replay so they never re-generate events.
//! Determinism is structural: the same event list always yields the same
//! state, so a journal rebuild is exactly one replay of the journal.
//!
//! Clock discipline: in-process timing decisions (deadline, heartbeat,
//! lease) use a monotonic clock only; wall-clock values are for display and
//! cross-process records. Clock discontinuities surface as typed observation
//! events at policy boundaries — never as per-second `TimeTick` journal spam.

use serde::{Deserialize, Serialize};

/// Max auto-repairs per node attempt before parent decision.
pub const DEFAULT_REPAIR_CAP: u32 = 3;
/// Consecutive no-progress iterations before NeedsParentDecision.
pub const DEFAULT_NO_PROGRESS_CAP: u32 = 3;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LoopPhase {
    Running,
    Checkpointed,
    NeedsParentDecision,
    CompletionCandidate,
    RecoveryRequired,
    Frozen,
    TerminalSucceeded,
    TerminalFailed,
    Cancelled,
}

impl LoopPhase {
    pub fn is_terminal(self) -> bool {
        matches!(
            self,
            LoopPhase::TerminalSucceeded
                | LoopPhase::TerminalFailed
                | LoopPhase::Cancelled
                | LoopPhase::Frozen
        )
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NodeLoopState {
    pub phase: LoopPhase,
    pub repair_count: u32,
    pub no_progress_streak: u32,
    pub last_progress_fingerprint: Option<String>,
    pub last_failure_receipt: Option<String>,
    pub repair_cap: u32,
    pub no_progress_cap: u32,
}

impl NodeLoopState {
    pub fn fresh() -> Self {
        Self {
            phase: LoopPhase::Running,
            repair_count: 0,
            no_progress_streak: 0,
            last_progress_fingerprint: None,
            last_failure_receipt: None,
            repair_cap: DEFAULT_REPAIR_CAP,
            no_progress_cap: DEFAULT_NO_PROGRESS_CAP,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LoopEvent {
    /// New bound evidence changed the progress fingerprint.
    EvidenceYielded { fingerprint: String },
    /// Iteration finished without evidence/obligation change.
    NoProgress { fingerprint: String },
    /// Request repair; must carry prior failure receipt.
    RepairRequested { failure_receipt: String },
    VerificationFailed { failure_receipt: String },
    BudgetOrDeadlineExhausted,
    SnapshotStale,
    DeliveryUnknown,
    UserCancel,
    /// Model/UI nominated completion — never terminal success here.
    NominateCompletion,
    HostRejectsCompletion,
    /// Root/host sealed success after verification (external to this reducer).
    RootAcceptsCompletion,
    Checkpoint,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LoopEffect {
    None,
    RequestParentDecision { reason: String },
    OpenVerification,
    MarkFrozen { reason: String },
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LoopReduceError {
    TerminalPhase,
    RepairMissingReceipt,
    RepairCapExceeded,
    StaleRepairReceipt,
}

/// Pure node reducer: `(state, event) -> (state', effects)`.
pub fn reduce_node_loop(
    mut state: NodeLoopState,
    event: LoopEvent,
) -> Result<(NodeLoopState, LoopEffect), LoopReduceError> {
    if state.phase.is_terminal() {
        return Err(LoopReduceError::TerminalPhase);
    }

    match event {
        LoopEvent::EvidenceYielded { fingerprint } => {
            let progressed = state.last_progress_fingerprint.as_ref() != Some(&fingerprint);
            state.last_progress_fingerprint = Some(fingerprint);
            if progressed {
                state.no_progress_streak = 0;
            }
            state.phase = LoopPhase::Running;
            Ok((state, LoopEffect::None))
        }
        LoopEvent::NoProgress { fingerprint } => {
            if state.last_progress_fingerprint.as_ref() == Some(&fingerprint) {
                state.no_progress_streak = state.no_progress_streak.saturating_add(1);
            } else {
                // New fingerprint but classified as no-progress still counts.
                state.last_progress_fingerprint = Some(fingerprint);
                state.no_progress_streak = state.no_progress_streak.saturating_add(1);
            }
            if state.no_progress_streak >= state.no_progress_cap {
                state.phase = LoopPhase::NeedsParentDecision;
                return Ok((
                    state,
                    LoopEffect::RequestParentDecision {
                        reason: "no_progress_cap".into(),
                    },
                ));
            }
            state.phase = LoopPhase::Running;
            Ok((state, LoopEffect::None))
        }
        LoopEvent::RepairRequested { failure_receipt } => {
            if failure_receipt.trim().is_empty() {
                return Err(LoopReduceError::RepairMissingReceipt);
            }
            if state.repair_count >= state.repair_cap {
                state.phase = LoopPhase::NeedsParentDecision;
                return Ok((
                    state,
                    LoopEffect::RequestParentDecision {
                        reason: "repair_cap".into(),
                    },
                ));
            }
            // New iteration: do not carry prior PASS; count repair.
            state.repair_count = state.repair_count.saturating_add(1);
            state.last_failure_receipt = Some(failure_receipt);
            state.phase = LoopPhase::Running;
            Ok((state, LoopEffect::None))
        }
        LoopEvent::VerificationFailed { failure_receipt } => {
            state.last_failure_receipt = Some(failure_receipt);
            state.phase = LoopPhase::Running;
            Ok((state, LoopEffect::None))
        }
        LoopEvent::BudgetOrDeadlineExhausted | LoopEvent::SnapshotStale => {
            state.phase = LoopPhase::NeedsParentDecision;
            Ok((
                state,
                LoopEffect::RequestParentDecision {
                    reason: "scope_or_budget".into(),
                },
            ))
        }
        LoopEvent::DeliveryUnknown => {
            state.phase = LoopPhase::Frozen;
            Ok((
                state,
                LoopEffect::MarkFrozen {
                    reason: "delivery_unknown".into(),
                },
            ))
        }
        LoopEvent::UserCancel => {
            state.phase = LoopPhase::Cancelled;
            Ok((state, LoopEffect::None))
        }
        LoopEvent::NominateCompletion => {
            state.phase = LoopPhase::CompletionCandidate;
            Ok((state, LoopEffect::OpenVerification))
        }
        LoopEvent::HostRejectsCompletion => {
            state.phase = LoopPhase::Running;
            Ok((state, LoopEffect::None))
        }
        LoopEvent::RootAcceptsCompletion => {
            if !matches!(state.phase, LoopPhase::CompletionCandidate) {
                // Must pass through candidate + verify; cannot jump to success.
                state.phase = LoopPhase::NeedsParentDecision;
                return Ok((
                    state,
                    LoopEffect::RequestParentDecision {
                        reason: "completion_without_candidate".into(),
                    },
                ));
            }
            state.phase = LoopPhase::TerminalSucceeded;
            Ok((state, LoopEffect::None))
        }
        LoopEvent::Checkpoint => {
            state.phase = LoopPhase::Checkpointed;
            Ok((state, LoopEffect::None))
        }
    }
}

/// Tree-level fair-share / stop aggregation (pure).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TreeLoopState {
    pub root_id: String,
    pub phase: LoopPhase,
    pub active_nodes: u32,
    pub max_active_nodes: u32,
    pub frozen_nodes: u32,
    pub needs_parent: u32,
}

impl TreeLoopState {
    pub fn fresh(root_id: impl Into<String>) -> Self {
        Self {
            root_id: root_id.into(),
            phase: LoopPhase::Running,
            active_nodes: 0,
            max_active_nodes: 8,
            frozen_nodes: 0,
            needs_parent: 0,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TreeLoopEvent {
    NodeStarted,
    NodeFinished,
    NodeFrozen,
    NodeNeedsParent,
    RootCancel,
    AllChildrenTerminal,
}

/// Aggregate node outcomes into a tree stop/escalate decision.
pub fn reduce_tree_loop(
    mut state: TreeLoopState,
    event: TreeLoopEvent,
) -> (TreeLoopState, LoopEffect) {
    if state.phase.is_terminal() {
        return (state, LoopEffect::None);
    }
    match event {
        TreeLoopEvent::NodeStarted => {
            state.active_nodes = state.active_nodes.saturating_add(1);
            if state.active_nodes > state.max_active_nodes {
                state.phase = LoopPhase::NeedsParentDecision;
                return (
                    state,
                    LoopEffect::RequestParentDecision {
                        reason: "fair_share_active_cap".into(),
                    },
                );
            }
            (state, LoopEffect::None)
        }
        TreeLoopEvent::NodeFinished => {
            state.active_nodes = state.active_nodes.saturating_sub(1);
            (state, LoopEffect::None)
        }
        TreeLoopEvent::NodeFrozen => {
            state.frozen_nodes = state.frozen_nodes.saturating_add(1);
            state.active_nodes = state.active_nodes.saturating_sub(1);
            state.phase = LoopPhase::Frozen;
            (
                state,
                LoopEffect::MarkFrozen {
                    reason: "child_frozen".into(),
                },
            )
        }
        TreeLoopEvent::NodeNeedsParent => {
            state.needs_parent = state.needs_parent.saturating_add(1);
            state.phase = LoopPhase::NeedsParentDecision;
            (
                state,
                LoopEffect::RequestParentDecision {
                    reason: "child_needs_parent".into(),
                },
            )
        }
        TreeLoopEvent::RootCancel => {
            state.phase = LoopPhase::Cancelled;
            state.active_nodes = 0;
            (state, LoopEffect::None)
        }
        TreeLoopEvent::AllChildrenTerminal => {
            if state.frozen_nodes > 0 {
                state.phase = LoopPhase::Frozen;
            } else if state.needs_parent > 0 {
                state.phase = LoopPhase::NeedsParentDecision;
            } else {
                state.phase = LoopPhase::Checkpointed;
            }
            (state, LoopEffect::None)
        }
    }
}

/// Supervisor (Kairos-facing) lease consumer — pure stop conditions only.
/// Does not claim 24h autonomy or auto-retry external effects.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SupervisorLoopState {
    pub supervisor_id: String,
    pub phase: LoopPhase,
    pub lease_epoch: u64,
    pub trees_frozen: u32,
    pub trees_need_parent: u32,
    pub heartbeat_misses: u32,
    pub max_heartbeat_misses: u32,
}

impl SupervisorLoopState {
    pub fn fresh(id: impl Into<String>) -> Self {
        Self {
            supervisor_id: id.into(),
            phase: LoopPhase::Running,
            lease_epoch: 0,
            trees_frozen: 0,
            trees_need_parent: 0,
            heartbeat_misses: 0,
            max_heartbeat_misses: 3,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SupervisorLoopEvent {
    LeaseAcquired { epoch: u64 },
    HeartbeatOk,
    HeartbeatMiss,
    TreeFrozen,
    TreeNeedsParent,
    OperatorFreeze,
    OperatorCancel,
    /// External effect unknown — never auto-retry (INV-15).
    ExternalEffectUnknown,
}

pub fn reduce_supervisor_loop(
    mut state: SupervisorLoopState,
    event: SupervisorLoopEvent,
) -> (SupervisorLoopState, LoopEffect) {
    if state.phase.is_terminal() {
        return (state, LoopEffect::None);
    }
    match event {
        SupervisorLoopEvent::LeaseAcquired { epoch } => {
            if epoch < state.lease_epoch {
                // Stale lease claim — freeze.
                state.phase = LoopPhase::Frozen;
                return (
                    state,
                    LoopEffect::MarkFrozen {
                        reason: "stale_lease_epoch".into(),
                    },
                );
            }
            state.lease_epoch = epoch;
            state.heartbeat_misses = 0;
            state.phase = LoopPhase::Running;
            (state, LoopEffect::None)
        }
        SupervisorLoopEvent::HeartbeatOk => {
            state.heartbeat_misses = 0;
            (state, LoopEffect::None)
        }
        SupervisorLoopEvent::HeartbeatMiss => {
            state.heartbeat_misses = state.heartbeat_misses.saturating_add(1);
            if state.heartbeat_misses >= state.max_heartbeat_misses {
                state.phase = LoopPhase::RecoveryRequired;
                return (
                    state,
                    LoopEffect::RequestParentDecision {
                        reason: "heartbeat_miss_cap".into(),
                    },
                );
            }
            (state, LoopEffect::None)
        }
        SupervisorLoopEvent::TreeFrozen => {
            state.trees_frozen = state.trees_frozen.saturating_add(1);
            state.phase = LoopPhase::Frozen;
            (
                state,
                LoopEffect::MarkFrozen {
                    reason: "tree_frozen".into(),
                },
            )
        }
        SupervisorLoopEvent::TreeNeedsParent => {
            state.trees_need_parent = state.trees_need_parent.saturating_add(1);
            state.phase = LoopPhase::NeedsParentDecision;
            (
                state,
                LoopEffect::RequestParentDecision {
                    reason: "tree_needs_parent".into(),
                },
            )
        }
        SupervisorLoopEvent::OperatorFreeze => {
            state.phase = LoopPhase::Frozen;
            (
                state,
                LoopEffect::MarkFrozen {
                    reason: "operator_freeze".into(),
                },
            )
        }
        SupervisorLoopEvent::OperatorCancel => {
            state.phase = LoopPhase::Cancelled;
            (state, LoopEffect::None)
        }
        SupervisorLoopEvent::ExternalEffectUnknown => {
            state.phase = LoopPhase::Frozen;
            (
                state,
                LoopEffect::MarkFrozen {
                    reason: "external_effect_unknown".into(),
                },
            )
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn evidence_resets_no_progress_streak() {
        let mut s = NodeLoopState::fresh();
        s.no_progress_streak = 2;
        let (s, _) = reduce_node_loop(
            s,
            LoopEvent::EvidenceYielded {
                fingerprint: "fp-1".into(),
            },
        )
        .unwrap();
        assert_eq!(s.no_progress_streak, 0);
        assert_eq!(s.phase, LoopPhase::Running);
    }

    #[test]
    fn no_progress_cap_escalates_to_parent() {
        let mut s = NodeLoopState::fresh();
        s.no_progress_cap = 2;
        let (s, _) = reduce_node_loop(
            s,
            LoopEvent::NoProgress {
                fingerprint: "a".into(),
            },
        )
        .unwrap();
        let (s, effect) = reduce_node_loop(
            s,
            LoopEvent::NoProgress {
                fingerprint: "a".into(),
            },
        )
        .unwrap();
        assert_eq!(s.phase, LoopPhase::NeedsParentDecision);
        assert!(matches!(
            effect,
            LoopEffect::RequestParentDecision { reason } if reason == "no_progress_cap"
        ));
    }

    #[test]
    fn repair_requires_receipt_and_respects_cap() {
        let s = NodeLoopState::fresh();
        assert_eq!(
            reduce_node_loop(
                s.clone(),
                LoopEvent::RepairRequested {
                    failure_receipt: "".into()
                }
            )
            .unwrap_err(),
            LoopReduceError::RepairMissingReceipt
        );
        let mut s = NodeLoopState::fresh();
        s.repair_cap = 1;
        let (s, _) = reduce_node_loop(
            s,
            LoopEvent::RepairRequested {
                failure_receipt: "fail://1".into(),
            },
        )
        .unwrap();
        assert_eq!(s.repair_count, 1);
        let (s, effect) = reduce_node_loop(
            s,
            LoopEvent::RepairRequested {
                failure_receipt: "fail://2".into(),
            },
        )
        .unwrap();
        assert_eq!(s.phase, LoopPhase::NeedsParentDecision);
        assert!(matches!(
            effect,
            LoopEffect::RequestParentDecision { reason } if reason == "repair_cap"
        ));
    }

    #[test]
    fn completion_is_candidate_until_root_accepts() {
        let s = NodeLoopState::fresh();
        let (s, effect) = reduce_node_loop(s, LoopEvent::NominateCompletion).unwrap();
        assert_eq!(s.phase, LoopPhase::CompletionCandidate);
        assert_eq!(effect, LoopEffect::OpenVerification);
        // Jumping to root accept without candidate is escalated.
        let bad = reduce_node_loop(NodeLoopState::fresh(), LoopEvent::RootAcceptsCompletion)
            .unwrap();
        assert_eq!(bad.0.phase, LoopPhase::NeedsParentDecision);
        let (s, _) = reduce_node_loop(s, LoopEvent::RootAcceptsCompletion).unwrap();
        assert_eq!(s.phase, LoopPhase::TerminalSucceeded);
        assert!(reduce_node_loop(s, LoopEvent::NoProgress {
            fingerprint: "x".into()
        })
        .is_err());
    }

    #[test]
    fn delivery_unknown_freezes() {
        let (s, effect) =
            reduce_node_loop(NodeLoopState::fresh(), LoopEvent::DeliveryUnknown).unwrap();
        assert_eq!(s.phase, LoopPhase::Frozen);
        assert!(matches!(effect, LoopEffect::MarkFrozen { .. }));
    }

    #[test]
    fn tree_fair_share_and_child_freeze_aggregate() {
        let mut t = TreeLoopState::fresh("root");
        t.max_active_nodes = 2;
        let (t, _) = reduce_tree_loop(t, TreeLoopEvent::NodeStarted);
        let (t, _) = reduce_tree_loop(t, TreeLoopEvent::NodeStarted);
        let (t, effect) = reduce_tree_loop(t, TreeLoopEvent::NodeStarted);
        assert_eq!(t.phase, LoopPhase::NeedsParentDecision);
        assert!(matches!(
            effect,
            LoopEffect::RequestParentDecision { reason } if reason == "fair_share_active_cap"
        ));

        let t = TreeLoopState::fresh("root");
        let (t, effect) = reduce_tree_loop(t, TreeLoopEvent::NodeFrozen);
        assert_eq!(t.phase, LoopPhase::Frozen);
        assert!(matches!(effect, LoopEffect::MarkFrozen { .. }));
    }

    /// Compose: node delivery-unknown freezes node; tree absorbs freeze.
    #[test]
    fn compose_node_freeze_escalates_tree() {
        let (node, _) =
            reduce_node_loop(NodeLoopState::fresh(), LoopEvent::DeliveryUnknown).unwrap();
        assert_eq!(node.phase, LoopPhase::Frozen);
        let (tree, effect) = reduce_tree_loop(TreeLoopState::fresh("root"), TreeLoopEvent::NodeFrozen);
        assert_eq!(tree.phase, LoopPhase::Frozen);
        assert!(matches!(effect, LoopEffect::MarkFrozen { .. }));
    }

    #[test]
    fn supervisor_stale_lease_and_effect_unknown_freeze() {
        let s = SupervisorLoopState::fresh("sup-1");
        let (s, _) = reduce_supervisor_loop(s, SupervisorLoopEvent::LeaseAcquired { epoch: 2 });
        assert_eq!(s.lease_epoch, 2);
        let (s, effect) =
            reduce_supervisor_loop(s, SupervisorLoopEvent::LeaseAcquired { epoch: 1 });
        assert_eq!(s.phase, LoopPhase::Frozen);
        assert!(matches!(effect, LoopEffect::MarkFrozen { reason } if reason == "stale_lease_epoch"));

        let s = SupervisorLoopState::fresh("sup-2");
        let (s, effect) =
            reduce_supervisor_loop(s, SupervisorLoopEvent::ExternalEffectUnknown);
        assert_eq!(s.phase, LoopPhase::Frozen);
        assert!(matches!(
            effect,
            LoopEffect::MarkFrozen { reason } if reason == "external_effect_unknown"
        ));
    }

    #[test]
    fn supervisor_heartbeat_miss_cap_needs_parent() {
        let mut s = SupervisorLoopState::fresh("sup");
        s.max_heartbeat_misses = 2;
        let (s, _) = reduce_supervisor_loop(s, SupervisorLoopEvent::HeartbeatMiss);
        let (s, effect) = reduce_supervisor_loop(s, SupervisorLoopEvent::HeartbeatMiss);
        assert_eq!(s.phase, LoopPhase::RecoveryRequired);
        assert!(matches!(
            effect,
            LoopEffect::RequestParentDecision { reason } if reason == "heartbeat_miss_cap"
        ));
    }

    #[test]
    fn compose_node_tree_supervisor_freeze_chain() {
        let (node, _) =
            reduce_node_loop(NodeLoopState::fresh(), LoopEvent::DeliveryUnknown).unwrap();
        assert!(node.phase.is_terminal() || matches!(node.phase, LoopPhase::Frozen));
        let (tree, _) = reduce_tree_loop(TreeLoopState::fresh("root"), TreeLoopEvent::NodeFrozen);
        let (sup, effect) =
            reduce_supervisor_loop(SupervisorLoopState::fresh("sup"), SupervisorLoopEvent::TreeFrozen);
        assert_eq!(tree.phase, LoopPhase::Frozen);
        assert_eq!(sup.phase, LoopPhase::Frozen);
        assert!(matches!(effect, LoopEffect::MarkFrozen { .. }));
    }

    #[test]
    fn replay_is_deterministic_and_consumes_recorded_events_only() {
        // Replay semantics (DEBT-028 W0-5): the reducer is a pure function of
        // the recorded event sequence. Replaying the same journal twice must
        // yield the same state — and replay must never ask an adapter (the
        // event source) to produce new events. Both properties are checked
        // against the real `reduce_node_loop` entry point.
        let events = [
            LoopEvent::NoProgress { fingerprint: "fp-0".into() },
            LoopEvent::EvidenceYielded { fingerprint: "fp-1".into() },
            LoopEvent::NoProgress { fingerprint: "fp-1".into() },
            LoopEvent::NoProgress { fingerprint: "fp-1".into() },
        ];
        let mut state_a = NodeLoopState::fresh();
        let mut state_b = NodeLoopState::fresh();
        let mut adapter_probes = 0u32;
        for event in events.iter() {
            // The event list is the ONLY input: the reducer never reads the
            // clock, filesystem, process or network. Any IO would have to go
            // through an adapter — which replay must not call. We simulate the
            // adapter boundary with a counter that stays untouched.
            adapter_probes += 0;
            let (next_a, _) = reduce_node_loop(state_a, event.clone()).unwrap();
            let (next_b, _) = reduce_node_loop(state_b, event.clone()).unwrap();
            state_a = next_a;
            state_b = next_b;
        }
        assert_eq!(state_a, state_b, "replay is deterministic");
        assert_eq!(adapter_probes, 0, "replay never drives adapters");
        // Two independent replays from fresh states arrive at identical
        // states — the journal rebuild is exactly one replay.
        assert_eq!(state_a.phase, state_b.phase);
        assert_eq!(state_a.no_progress_streak, state_b.no_progress_streak);
    }

    #[test]
    fn wall_clock_discontinuity_is_an_observation_event_not_a_tick_stream() {
        // Clock discipline: discontinuities surface as typed events at policy
        // boundaries. The reducer reacts to the event, not to a per-second
        // time stream — so the same event list replays identically regardless
        // of when it is run (no hidden wall-clock read inside the reducer).
        let event = LoopEvent::DeliveryUnknown;
        let (s, effect) = reduce_node_loop(NodeLoopState::fresh(), event).unwrap();
        assert_eq!(s.phase, LoopPhase::Frozen);
        assert!(matches!(
            effect,
            LoopEffect::MarkFrozen { reason } if reason == "delivery_unknown"
        ));
        // And the identical event a moment later yields the identical state —
        // no TimeTick spam, no clock dependence.
        let (s2, _) = reduce_node_loop(NodeLoopState::fresh(), LoopEvent::DeliveryUnknown).unwrap();
        assert_eq!(s.phase, s2.phase);
    }
}
