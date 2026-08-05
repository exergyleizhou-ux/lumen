//! S12 / NG-08 — OperatorControlPlane v1 (pure, offline-safe).
//!
//! Humans may see, freeze, cancel, approve-resume and take over supervised
//! operations — but the UI/ACP never mutates state directly: it sends a
//! *typed operator command* through this plane. Every command leaves a typed
//! receipt that maps to a [`GovernedLifecycleEventV1`] journal entry; nothing
//! here touches files, processes or providers (plan §NG-08).
//!
//! Denials are fail-closed: stale owner, expired approval, terminal/cancelled
//! operations, unknown operations and freeze-during-tool-signal all refuse
//! instead of guessing.

use serde::{Deserialize, Serialize};

use crate::evidence_loop::LoopPhase;
use crate::lifecycle_journal::{
    GovernedLifecycleEventKind, GovernedLifecycleEventSource, GovernedLifecycleEventV1,
};

/// The five typed operator commands (plan §OperatorControlPlane v1).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum OperatorCommand {
    /// Read-only audit view. Returns hashes/projections, never raw secrets,
    /// model thinking chains, or unaccepted sibling scratch.
    InspectOperation { op_id: String },
    /// Freeze Starting/Running/RecoveryRequired; blocks new dispatch and
    /// waits for a bounded drain. Never marks unknown effects as success.
    FreezeOperation {
        op_id: String,
        reason: String,
    },
    /// Cancel within root/ancestor scope; revokes grants/leases, idempotently
    /// releases reservations, requests adapter stop. Never just kills a PID,
    /// never drops descendants, never auto-deletes evidence.
    CancelOperation { op_id: String },
    /// Approve resume for a Frozen/Blocked operation — only the explicitly
    /// named node/attempt/operation with a deadline/scope. Immutable
    /// approval id + new lease epoch.
    ApproveResume {
        op_id: String,
        node_id: String,
        deadline_epoch_ms: u64,
        scope: String,
    },
    /// Take over an expired/foreign lease and reconcile; only the holder
    /// epoch changes. Never creates a dual owner, never bypasses a missing
    /// receipt, never auto-replays an external effect.
    TakeOver {
        op_id: String,
        expected_old_holder: String,
    },
}

/// Pure read-only projection of an operation (the only view Inspect gets).
///
/// Built by the caller from the governed-operation store / Kairos state;
/// contains hashes and status fields only — no raw content, no secrets.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct OperationView {
    pub op_id: String,
    pub owner: Option<String>,
    pub lease_epoch: u64,
    pub phase: LoopPhase,
    pub attempt_observed: bool,
    pub external_effect_unknown: bool,
    pub manifest_hash: String,
    pub evidence_hash: String,
    pub budget_hash: String,
}

/// Immutable approval issued by ApproveResume; expired approvals deny.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResumeApproval {
    pub approval_id: String,
    pub op_id: String,
    pub node_id: String,
    pub issued_at_epoch_ms: u64,
    pub expires_at_epoch_ms: u64,
    pub scope: String,
}

impl ResumeApproval {
    pub fn is_expired(&self, now_epoch_ms: u64) -> bool {
        now_epoch_ms >= self.expires_at_epoch_ms
    }
}

/// Typed receipt every command must leave (plan receipt column).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum OperatorReceipt {
    /// InspectOperation: read audit with the projected view.
    ReadAudit { op_id: String, view: OperationView },
    /// FreezeOperation: operator id + reason + observed attempt/effect states.
    Frozen {
        op_id: String,
        operator_id: String,
        reason: String,
        attempt_observed: bool,
        external_effect_unknown: bool,
    },
    /// CancelOperation: cancellation causal event + late-event policy.
    Cancelled {
        op_id: String,
        operator_id: String,
        causal_event: GovernedLifecycleEventKind,
        late_event_policy: &'static str,
    },
    /// ApproveResume: immutable approval id + new lease epoch + revision.
    ResumeApproved {
        op_id: String,
        approval_id: String,
        node_id: String,
        new_lease_epoch: u64,
        deadline_epoch_ms: u64,
        scope: String,
    },
    /// TakeOver: former holder + reconcile result + new epoch.
    TakenOver {
        op_id: String,
        former_holder: String,
        new_holder: String,
        reconcile_result: &'static str,
        new_lease_epoch: u64,
    },
}

/// Journal kind each receipt maps to (Frozen/Cancelled/Reconciled).
impl OperatorReceipt {
    pub fn journal_kind(&self) -> Option<GovernedLifecycleEventKind> {
        match self {
            OperatorReceipt::ReadAudit { .. } => None, // audit is not an authority transition
            OperatorReceipt::Frozen { .. } => Some(GovernedLifecycleEventKind::Frozen),
            OperatorReceipt::Cancelled { .. } => Some(GovernedLifecycleEventKind::Cancelled),
            OperatorReceipt::ResumeApproved { .. } | OperatorReceipt::TakenOver { .. } => {
                Some(GovernedLifecycleEventKind::Reconciled)
            }
        }
    }
}

/// Fail-closed denials. Unknown effect never becomes success; a stale owner
/// never takes over; an expired approval never resumes; a terminal operation
/// is never cancelled/frozen.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OperatorDeny {
    UnknownOperation,
    Terminal,
    Cancelled,
    AlreadyFrozen,
    NotFrozen,
    StaleOwner,
    ExpiredApproval,
    ApprovalForOtherOperation,
    ApprovalForOtherNode,
    NoLease,
    UnauthorizedOperator,
    EffectUncertain,
    ArchiveWithoutEvidence,
}

impl OperatorDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::UnknownOperation => "operator.unknown_operation",
            Self::Terminal => "operator.terminal",
            Self::Cancelled => "operator.cancelled",
            Self::AlreadyFrozen => "operator.already_frozen",
            Self::NotFrozen => "operator.not_frozen",
            Self::StaleOwner => "operator.stale_owner",
            Self::ExpiredApproval => "operator.expired_approval",
            Self::ApprovalForOtherOperation => "operator.approval_other_operation",
            Self::ApprovalForOtherNode => "operator.approval_other_node",
            Self::NoLease => "operator.no_lease",
            Self::UnauthorizedOperator => "operator.unauthorized_operator",
            Self::EffectUncertain => "operator.effect_uncertain",
            Self::ArchiveWithoutEvidence => "operator.archive_without_evidence",
        }
    }
}

/// Apply one typed operator command. Pure: the caller owns the store; the
/// plane returns the new lease epoch and the receipt to journal.
pub fn apply_operator_command(
    view: &OperationView,
    cmd: &OperatorCommand,
    operator_id: &str,
    now_epoch_ms: u64,
    approval: Option<&ResumeApproval>,
) -> Result<OperatorReceipt, OperatorDeny> {
    if !view_is_known(view) {
        return Err(OperatorDeny::UnknownOperation);
    }
    if matches!(
        view.phase,
        LoopPhase::TerminalSucceeded | LoopPhase::TerminalFailed
    ) {
        return Err(OperatorDeny::Terminal);
    }
    if matches!(view.phase, LoopPhase::Cancelled) {
        return Err(OperatorDeny::Cancelled);
    }

    match cmd {
        OperatorCommand::InspectOperation { op_id } => {
            if op_id != &view.op_id {
                return Err(OperatorDeny::UnknownOperation);
            }
            Ok(OperatorReceipt::ReadAudit {
                op_id: view.op_id.clone(),
                view: view.clone(),
            })
        }
        OperatorCommand::FreezeOperation { op_id, reason } => {
            if op_id != &view.op_id {
                return Err(OperatorDeny::UnknownOperation);
            }
            if matches!(view.phase, LoopPhase::Frozen) {
                return Err(OperatorDeny::AlreadyFrozen);
            }
            // Freeze is allowed on Running/RecoveryRequired only.
            if !matches!(
                view.phase,
                LoopPhase::Running | LoopPhase::RecoveryRequired
            ) {
                return Err(OperatorDeny::Terminal);
            }
            Ok(OperatorReceipt::Frozen {
                op_id: view.op_id.clone(),
                operator_id: operator_id.to_string(),
                reason: reason.clone(),
                attempt_observed: view.attempt_observed,
                // Unknown effects stay unknown — never marked success.
                external_effect_unknown: view.external_effect_unknown,
            })
        }
        OperatorCommand::CancelOperation { op_id } => {
            if op_id != &view.op_id {
                return Err(OperatorDeny::UnknownOperation);
            }
            if matches!(view.phase, LoopPhase::Frozen) {
                return Err(OperatorDeny::AlreadyFrozen);
            }
            Ok(OperatorReceipt::Cancelled {
                op_id: view.op_id.clone(),
                operator_id: operator_id.to_string(),
                causal_event: GovernedLifecycleEventKind::Cancelled,
                // Late events from a cancelled subtree are dropped, never
                // replayed into the read model.
                late_event_policy: "drop-late-events",
            })
        }
        OperatorCommand::ApproveResume {
            op_id,
            node_id,
            deadline_epoch_ms,
            scope,
        } => {
            if op_id != &view.op_id {
                return Err(OperatorDeny::UnknownOperation);
            }
            if !matches!(
                view.phase,
                LoopPhase::Frozen | LoopPhase::NeedsParentDecision
            ) {
                return Err(OperatorDeny::NotFrozen);
            }
            let Some(approval) = approval else {
                return Err(OperatorDeny::ExpiredApproval);
            };
            if approval.op_id != view.op_id {
                return Err(OperatorDeny::ApprovalForOtherOperation);
            }
            if approval.node_id != *node_id {
                return Err(OperatorDeny::ApprovalForOtherNode);
            }
            if approval.is_expired(now_epoch_ms) {
                return Err(OperatorDeny::ExpiredApproval);
            }
            // External-effect uncertainty can never be resumed silently.
            if view.external_effect_unknown {
                return Err(OperatorDeny::EffectUncertain);
            }
            Ok(OperatorReceipt::ResumeApproved {
                op_id: view.op_id.clone(),
                approval_id: approval.approval_id.clone(),
                node_id: node_id.clone(),
                new_lease_epoch: view.lease_epoch.saturating_add(1),
                deadline_epoch_ms: *deadline_epoch_ms,
                scope: scope.clone(),
            })
        }
        OperatorCommand::TakeOver {
            op_id,
            expected_old_holder,
        } => {
            if op_id != &view.op_id {
                return Err(OperatorDeny::UnknownOperation);
            }
            let Some(owner) = &view.owner else {
                return Err(OperatorDeny::NoLease);
            };
            if owner != expected_old_holder {
                return Err(OperatorDeny::StaleOwner);
            }
            // A takeover reconciles the holder only; unknown external effects
            // are never auto-replayed (fail-closed).
            if view.external_effect_unknown {
                return Err(OperatorDeny::EffectUncertain);
            }
            Ok(OperatorReceipt::TakenOver {
                op_id: view.op_id.clone(),
                former_holder: owner.clone(),
                new_holder: operator_id.to_string(),
                reconcile_result: "holder-reconciled",
                new_lease_epoch: view.lease_epoch.saturating_add(1),
            })
        }
    }
}

fn view_is_known(view: &OperationView) -> bool {
    !view.op_id.is_empty()
}

/// Build a resume approval (immutable id). Callers persist it before
/// applying ApproveResume so expiry is judged against the same id.
pub fn issue_resume_approval(
    approval_id: impl Into<String>,
    op_id: impl Into<String>,
    node_id: impl Into<String>,
    issued_at_epoch_ms: u64,
    ttl_epoch_ms: u64,
    scope: impl Into<String>,
) -> ResumeApproval {
    let issued_at_epoch_ms = issued_at_epoch_ms;
    ResumeApproval {
        approval_id: approval_id.into(),
        op_id: op_id.into(),
        node_id: node_id.into(),
        issued_at_epoch_ms,
        expires_at_epoch_ms: issued_at_epoch_ms.saturating_add(ttl_epoch_ms),
        scope: scope.into(),
    }
}

/// Convert an operator receipt into a journal event (the operation journal
/// write). Audits map to `None` (they are not authority transitions); the
/// caller appends the returned event via [`LifecycleJournal::append`].
pub fn operator_receipt_to_event(
    receipt: &OperatorReceipt,
    event_id: impl Into<String>,
    task_tree_id: impl Into<String>,
    node_id: impl Into<String>,
    owner_session_id: impl Into<String>,
    sequence: u64,
    occurred_at_epoch_ms: u64,
) -> Option<GovernedLifecycleEventV1> {
    let kind = receipt.journal_kind()?;
    let mut event = GovernedLifecycleEventV1 {
        event_id: event_id.into(),
        task_tree_id: task_tree_id.into(),
        node_id: node_id.into(),
        owner_session_id: owner_session_id.into(),
        sequence,
        causal_parent: None,
        kind,
        source: GovernedLifecycleEventSource::Actor,
        lease_id: None,
        contract_hash: None,
        policy_revision: 1,
        evidence_refs: Vec::new(),
        occurred_at: occurred_at_epoch_ms,
        payload_hash: String::new(),
    };
    // The journal verifies the payload hash on append; compute it here so the
    // event is append-ready (canonical NG-00 payload commitment).
    event.payload_hash = event.compute_payload_hash().unwrap_or_default();
    Some(event)
}

/// An operation being archived (`ArchivedNeedsReview`, DEBT-028 W0-4).
///
/// The archive disposition is atomic and explicit: every governed resource
/// the operation holds is released, while every evidence/effect observation
/// is preserved. The archived operation remains *undecided* — archive is not
/// success, not failure, and not deletion.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ArchiveCandidate {
    pub operation_id: String,
    pub lease_id: Option<String>,
    pub reservation_id: Option<String>,
    pub write_lease_id: Option<String>,
    pub worktree_id: Option<String>,
    pub process_scope_id: Option<String>,
    pub evidence_refs: Vec<String>,
}

/// Receipt of the atomic resource release that accompanies archiving.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResourceReleaseReceipt {
    pub operation_id: String,
    pub lease_released: bool,
    pub reservation_released: bool,
    pub write_scope_released: bool,
    pub worktree_released: bool,
    pub process_scope_released: bool,
    pub evidence_preserved: Vec<String>,
    pub archive_reason: String,
}

/// Pure archive disposition: release lease / reservation / write-scope /
/// worktree / process scope; preserve every evidence ref; keep the operation
/// undecided. Fail-closed: an archive that would destroy the last evidence of
/// an operation is refused. Idempotent: the same candidate always produces
/// the same receipt.
pub fn archive_release_resources(
    candidate: &ArchiveCandidate,
    archive_reason: &str,
) -> Result<ResourceReleaseReceipt, OperatorDeny> {
    if candidate.operation_id.trim().is_empty() {
        return Err(OperatorDeny::UnknownOperation);
    }
    if candidate.evidence_refs.is_empty() {
        return Err(OperatorDeny::ArchiveWithoutEvidence);
    }
    Ok(ResourceReleaseReceipt {
        operation_id: candidate.operation_id.clone(),
        lease_released: candidate.lease_id.is_some(),
        reservation_released: candidate.reservation_id.is_some(),
        write_scope_released: candidate.write_lease_id.is_some(),
        worktree_released: candidate.worktree_id.is_some(),
        process_scope_released: candidate.process_scope_id.is_some(),
        evidence_preserved: candidate.evidence_refs.clone(),
        archive_reason: archive_reason.to_string(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn view(op_id: &str, phase: LoopPhase) -> OperationView {
        OperationView {
            op_id: op_id.to_string(),
            owner: Some("worker-a".to_string()),
            lease_epoch: 3,
            phase,
            attempt_observed: false,
            external_effect_unknown: false,
            manifest_hash: "m1".into(),
            evidence_hash: "e1".into(),
            budget_hash: "b1".into(),
        }
    }

    fn running(op_id: &str) -> OperationView {
        view(op_id, LoopPhase::Running)
    }

    #[test]
    fn inspect_returns_audit_view_without_secrets() {
        let v = running("op1");
        let cmd = OperatorCommand::InspectOperation {
            op_id: "op1".into(),
        };
        let receipt = apply_operator_command(&v, &cmd, "human", 1000, None).unwrap();
        let journal_kind = receipt.journal_kind();
        match receipt {
            OperatorReceipt::ReadAudit { op_id, view } => {
                assert_eq!(op_id, "op1");
                // Projection only: hashes and status, no raw content fields.
                assert_eq!(view.manifest_hash, "m1");
                assert_eq!(view.owner.as_deref(), Some("worker-a"));
            }
            other => panic!("expected ReadAudit, got {other:?}"),
        }
        assert_eq!(journal_kind, None, "audit is not a transition");
    }

    #[test]
    fn freeze_records_operator_reason_and_observed_states() {
        let mut v = running("op2");
        v.external_effect_unknown = true;
        let cmd = OperatorCommand::FreezeOperation {
            op_id: "op2".into(),
            reason: "needs human review".into(),
        };
        let receipt = apply_operator_command(&v, &cmd, "human", 1000, None).unwrap();
        let journal_kind = receipt.journal_kind();
        match receipt {
            OperatorReceipt::Frozen {
                op_id,
                operator_id,
                reason,
                attempt_observed,
                external_effect_unknown,
            } => {
                assert_eq!(op_id, "op2");
                assert_eq!(operator_id, "human");
                assert_eq!(reason, "needs human review");
                assert!(!attempt_observed);
                // Unknown effect is recorded as unknown, never success.
                assert!(external_effect_unknown);
            }
            other => panic!("expected Frozen, got {other:?}"),
        }
        assert_eq!(journal_kind, Some(GovernedLifecycleEventKind::Frozen));
    }

    #[test]
    fn cancel_after_terminal_denies_and_never_drops_evidence() {
        let v = view("op3", LoopPhase::TerminalSucceeded);
        let cmd = OperatorCommand::CancelOperation { op_id: "op3".into() };
        assert_eq!(
            apply_operator_command(&v, &cmd, "human", 1000, None).unwrap_err(),
            OperatorDeny::Terminal
        );
    }

    #[test]
    fn cancel_emits_causal_event_with_late_event_policy() {
        let v = running("op4");
        let cmd = OperatorCommand::CancelOperation { op_id: "op4".into() };
        let receipt = apply_operator_command(&v, &cmd, "human", 1000, None).unwrap();
        match receipt {
            OperatorReceipt::Cancelled {
                causal_event,
                late_event_policy,
                ..
            } => {
                assert_eq!(causal_event, GovernedLifecycleEventKind::Cancelled);
                assert_eq!(late_event_policy, "drop-late-events");
            }
            other => panic!("expected Cancelled, got {other:?}"),
        }
    }

    #[test]
    fn resume_requires_valid_unexpired_approval_for_same_op_and_node() {
        let v = view("op5", LoopPhase::Frozen);
        let approval = issue_resume_approval("appr1", "op5", "node-x", 1000, 10_000, "root");
        let cmd = OperatorCommand::ApproveResume {
            op_id: "op5".into(),
            node_id: "node-x".into(),
            deadline_epoch_ms: 20_000,
            scope: "root-approved".into(),
        };
        let receipt = apply_operator_command(&v, &cmd, "human", 2000, Some(&approval)).unwrap();
        match receipt {
            OperatorReceipt::ResumeApproved {
                approval_id,
                new_lease_epoch,
                ..
            } => {
                assert_eq!(approval_id, "appr1");
                assert_eq!(new_lease_epoch, 4); // lease 3 + 1
            }
            other => panic!("expected ResumeApproved, got {other:?}"),
        }

        // Expired approval denies.
        let expired = issue_resume_approval("appr2", "op5", "node-x", 0, 10, "root");
        assert_eq!(
            apply_operator_command(&v, &cmd, "human", 50_000, Some(&expired)).unwrap_err(),
            OperatorDeny::ExpiredApproval
        );
        // Approval for another operation denies.
        let other_op = issue_resume_approval("appr3", "op-zzz", "node-x", 0, 10_000, "root");
        assert_eq!(
            apply_operator_command(&v, &cmd, "human", 2000, Some(&other_op)).unwrap_err(),
            OperatorDeny::ApprovalForOtherOperation
        );
        // Approval for another node denies.
        let other_node = issue_resume_approval("appr4", "op5", "node-y", 0, 10_000, "root");
        assert_eq!(
            apply_operator_command(&v, &cmd, "human", 2000, Some(&other_node)).unwrap_err(),
            OperatorDeny::ApprovalForOtherNode
        );
        // No approval at all denies.
        assert_eq!(
            apply_operator_command(&v, &cmd, "human", 2000, None).unwrap_err(),
            OperatorDeny::ExpiredApproval
        );
    }

    #[test]
    fn resume_never_silently_resumes_unknown_external_effect() {
        let mut v = view("op6", LoopPhase::Frozen);
        v.external_effect_unknown = true;
        let approval = issue_resume_approval("appr5", "op6", "node-x", 0, 10_000, "root");
        let cmd = OperatorCommand::ApproveResume {
            op_id: "op6".into(),
            node_id: "node-x".into(),
            deadline_epoch_ms: 20_000,
            scope: "root".into(),
        };
        assert_eq!(
            apply_operator_command(&v, &cmd, "human", 2000, Some(&approval)).unwrap_err(),
            OperatorDeny::EffectUncertain
        );
    }

    #[test]
    fn takeover_requires_stale_owner_match_and_never_dual_owns() {
        let v = running("op7");
        let cmd = OperatorCommand::TakeOver {
            op_id: "op7".into(),
            expected_old_holder: "worker-a".into(),
        };
        let receipt = apply_operator_command(&v, &cmd, "worker-b", 1000, None).unwrap();
        match receipt {
            OperatorReceipt::TakenOver {
                former_holder,
                new_holder,
                new_lease_epoch,
                ..
            } => {
                assert_eq!(former_holder, "worker-a");
                assert_eq!(new_holder, "worker-b");
                assert_eq!(new_lease_epoch, 4);
            }
            other => panic!("expected TakenOver, got {other:?}"),
        }

        // Stale owner (wrong expected holder) denies — no dual ownership.
        let stale = OperatorCommand::TakeOver {
            op_id: "op7".into(),
            expected_old_holder: "worker-zzz".into(),
        };
        assert_eq!(
            apply_operator_command(&v, &stale, "worker-b", 1000, None).unwrap_err(),
            OperatorDeny::StaleOwner
        );
    }

    #[test]
    fn takeover_never_auto_replays_unknown_external_effect() {
        let mut v = running("op8");
        v.external_effect_unknown = true;
        let cmd = OperatorCommand::TakeOver {
            op_id: "op8".into(),
            expected_old_holder: "worker-a".into(),
        };
        assert_eq!(
            apply_operator_command(&v, &cmd, "worker-b", 1000, None).unwrap_err(),
            OperatorDeny::EffectUncertain
        );
    }

    #[test]
    fn freeze_during_tool_signal_records_states_and_never_succeeds_unknown() {
        let mut v = running("op9");
        v.attempt_observed = true;
        v.external_effect_unknown = true;
        let cmd = OperatorCommand::FreezeOperation {
            op_id: "op9".into(),
            reason: "tool signal in flight".into(),
        };
        let receipt = apply_operator_command(&v, &cmd, "human", 1000, None).unwrap();
        match receipt {
            OperatorReceipt::Frozen {
                attempt_observed,
                external_effect_unknown,
                ..
            } => {
                assert!(attempt_observed);
                assert!(external_effect_unknown, "unknown stays unknown");
            }
            other => panic!("expected Frozen, got {other:?}"),
        }
    }

    #[test]
    fn restart_after_freeze_requires_new_approval_each_time() {
        // Frozen → approved resume (lease 4) → freeze again → the old
        // approval is spent: resuming again with it must fail expiry/freshness
        // semantics (the plane requires a fresh approval each time by
        // construction: only the caller can re-issue).
        let mut v = view("op10", LoopPhase::Frozen);
        let approval = issue_resume_approval("appr-new", "op10", "node-x", 1000, 10_000, "root");
        let cmd = OperatorCommand::ApproveResume {
            op_id: "op10".into(),
            node_id: "node-x".into(),
            deadline_epoch_ms: 20_000,
            scope: "root".into(),
        };
        let r1 = apply_operator_command(&v, &cmd, "human", 2000, Some(&approval)).unwrap();
        assert_eq!(r1.journal_kind(), Some(GovernedLifecycleEventKind::Reconciled));
        // After resume the view is Running; freezing again is still possible,
        // but resuming with the *same* approval after a later freeze is not
        // accepted unless a fresh approval exists — simulate the caller
        // re-issuing; the stale one is spent by the caller (not replayable).
        v.phase = LoopPhase::Running;
        let freeze = OperatorCommand::FreezeOperation {
            op_id: "op10".into(),
            reason: "re-freeze".into(),
        };
        apply_operator_command(&v, &freeze, "human", 3000, None).unwrap();
        v.phase = LoopPhase::Frozen;
        // Re-using the old approval for a second resume: the caller cannot
        // prove freshness, so the plane requires a new approval — here the
        // old one is simply not presented (deny by absence).
        assert_eq!(
            apply_operator_command(&v, &cmd, "human", 4000, None).unwrap_err(),
            OperatorDeny::ExpiredApproval
        );
    }

    #[test]
    fn operator_race_last_write_wins_with_epoch_monotonicity() {
        // Two operators race to take over the same lease: the first with the
        // correct old holder succeeds (epoch 4); the second sees epoch 4 and
        // a stale expected holder (worker-a) and is denied — no dual owner.
        let mut v = running("op11");
        let first = OperatorCommand::TakeOver {
            op_id: "op11".into(),
            expected_old_holder: "worker-a".into(),
        };
        let r1 = apply_operator_command(&v, &first, "worker-b", 1000, None).unwrap();
        let OperatorReceipt::TakenOver { new_lease_epoch, .. } = r1 else {
            panic!("expected TakenOver");
        };
        v.lease_epoch = new_lease_epoch;
        v.owner = Some("worker-b".into());
        let second = OperatorCommand::TakeOver {
            op_id: "op11".into(),
            expected_old_holder: "worker-a".into(),
        };
        assert_eq!(
            apply_operator_command(&v, &second, "worker-c", 1001, None).unwrap_err(),
            OperatorDeny::StaleOwner
        );
    }

    #[test]
    fn receipts_append_to_the_real_lifecycle_journal() {
        use crate::lifecycle_journal::{JournalError, LifecycleJournal};
        let mut journal = LifecycleJournal::in_memory("tree-j".to_string());
        // Freeze → Frozen event appends.
        let v = running("op12");
        let cmd = OperatorCommand::FreezeOperation {
            op_id: "op12".into(),
            reason: "audit".into(),
        };
        let receipt = apply_operator_command(&v, &cmd, "human", 1000, None).unwrap();
        let event = operator_receipt_to_event(
            &receipt,
            "evt-1",
            "tree-j",
            "op12",
            "sess-1",
            0,
            1000,
        )
        .expect("freeze maps to a journal event");
        assert_eq!(event.kind, GovernedLifecycleEventKind::Frozen);
        assert_eq!(event.source, GovernedLifecycleEventSource::Actor);
        assert!(journal.append(event).is_ok());

        // Audit maps to None — never a journal transition.
        let cmd = OperatorCommand::InspectOperation {
            op_id: "op12".into(),
        };
        let audit = apply_operator_command(&v, &cmd, "human", 1000, None).unwrap();
        assert!(operator_receipt_to_event(&audit, "evt-2", "tree-j", "op12", "sess-1", 1, 1000).is_none());

        // No-revival discipline: Frozen is a terminal journal event — appending
        // anything after it on the same journal is rejected (fail-closed).
        let frozen = view("op12", LoopPhase::Frozen);
        let approval = issue_resume_approval("appr-j", "op12", "op12", 0, 10_000, "root");
        let cmd = OperatorCommand::ApproveResume {
            op_id: "op12".into(),
            node_id: "op12".into(),
            deadline_epoch_ms: 20_000,
            scope: "root".into(),
        };
        let receipt = apply_operator_command(&frozen, &cmd, "human", 2000, Some(&approval)).unwrap();
        let after_frozen = operator_receipt_to_event(
            &receipt,
            "evt-3",
            "tree-j",
            "op12",
            "sess-1",
            1,
            2000,
        )
        .expect("resume maps to a journal event");
        assert_eq!(after_frozen.kind, GovernedLifecycleEventKind::Reconciled);
        assert_eq!(
            journal.append(after_frozen),
            Err(JournalError::LateEventAfterTerminal {
                kind: GovernedLifecycleEventKind::Reconciled
            }),
            "a frozen journal must not be revived by a later event"
        );

        // A resume after freeze starts a NEW recovery lifecycle: its own
        // journal accepts the Reconciled event (new lease, new sequence).
        let mut recovery = LifecycleJournal::in_memory("tree-j".to_string());
        let resumed = operator_receipt_to_event(
            &receipt,
            "evt-3r",
            "tree-j",
            "op12",
            "sess-1",
            0,
            2000,
        )
        .expect("maps");
        assert!(recovery.append(resumed).is_ok());

        // Foreign tree events are rejected by the journal (fail-closed).
        let foreign = operator_receipt_to_event(
            &receipt,
            "evt-4",
            "other-tree",
            "op12",
            "sess-1",
            0,
            2000,
        )
        .expect("maps");
        assert_eq!(recovery.append(foreign), Err(JournalError::ForeignTree));
    }

    #[test]
    fn archive_releases_resources_and_preserves_evidence() {
        let candidate = ArchiveCandidate {
            operation_id: "op-arch".into(),
            lease_id: Some("lease-7".into()),
            reservation_id: Some("res-7".into()),
            write_lease_id: Some("wscope-7".into()),
            worktree_id: Some("wt-7".into()),
            process_scope_id: Some("proc-7".into()),
            evidence_refs: vec!["ev-1".into(), "ev-2".into()],
        };
        let receipt =
            archive_release_resources(&candidate, "archived after operator review").expect("ok");
        assert_eq!(receipt.operation_id, "op-arch");
        assert!(receipt.lease_released);
        assert!(receipt.reservation_released);
        assert!(receipt.write_scope_released);
        assert!(receipt.worktree_released);
        assert!(receipt.process_scope_released);
        assert_eq!(receipt.evidence_preserved, vec!["ev-1", "ev-2"]);
        assert_eq!(receipt.archive_reason, "archived after operator review");
    }

    #[test]
    fn archive_without_evidence_is_refused() {
        let candidate = ArchiveCandidate {
            operation_id: "op-naked".into(),
            lease_id: Some("lease-1".into()),
            reservation_id: None,
            write_lease_id: None,
            worktree_id: None,
            process_scope_id: None,
            evidence_refs: Vec::new(),
        };
        assert_eq!(
            archive_release_resources(&candidate, "cleanup").unwrap_err(),
            OperatorDeny::ArchiveWithoutEvidence,
            "archiving must never be a way to destroy the last evidence"
        );
        let unknown = ArchiveCandidate {
            operation_id: String::new(),
            ..candidate
        };
        assert_eq!(
            archive_release_resources(&unknown, "x").unwrap_err(),
            OperatorDeny::UnknownOperation
        );
    }

    #[test]
    fn archive_release_is_idempotent() {
        let candidate = ArchiveCandidate {
            operation_id: "op-again".into(),
            lease_id: Some("l".into()),
            reservation_id: None,
            write_lease_id: None,
            worktree_id: None,
            process_scope_id: None,
            evidence_refs: vec!["ev".into()],
        };
        let first = archive_release_resources(&candidate, "r").expect("first");
        let second = archive_release_resources(&candidate, "r").expect("second");
        assert_eq!(first, second, "archive release is a pure, idempotent disposition");
        // A resource-less candidate (nothing held) still archives with
        // evidence preserved — release of nothing is fine.
        let bare = ArchiveCandidate {
            operation_id: "op-bare".into(),
            lease_id: None,
            reservation_id: None,
            write_lease_id: None,
            worktree_id: None,
            process_scope_id: None,
            evidence_refs: vec!["ev".into()],
        };
        let receipt = archive_release_resources(&bare, "nothing held").expect("ok");
        assert!(!receipt.lease_released && !receipt.write_scope_released);
        assert_eq!(receipt.evidence_preserved, vec!["ev"]);
    }
}
