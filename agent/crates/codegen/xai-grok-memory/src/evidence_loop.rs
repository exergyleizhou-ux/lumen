//! NG-04E / S7 — pure node evidence-loop reducer (no provider).
//!
//! Progress requires evidence yield or obligation discharge fingerprint change.
//! Repair is capped and must cite a failure receipt. Completion is only a
//! candidate until host/root layers (not modeled here as success).

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
}
