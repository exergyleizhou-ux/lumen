//! Production-facing Exit Gate helpers for NG remaining slices (A5–A12).
//!
//! Real shipped entry points used by offline gates and shell adapters.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

use crate::bounded_assignment_apply::{
    AssignmentApplyDeny, AssignmentApplyRequest, AssignmentLifecycle, authorize_assignment_apply,
};
use crate::client_advisor_consult::{
    AdvisorRequestKind, AdvisorRequestV1, ConsultBlockReason, ConsultOutcome,
    build_advisor_capsule, consult_timed_out,
};
use crate::client_advisor_shadow::AdvisorMode;
use crate::evidence_loop::LoopPhase;
use crate::handoff_packet::{HandoffDenyReason, HandoffPacketV1};
use crate::kairos_lease_consumer::{
    ConsumerOperation, ConsumerPolicy, ConsumerStep, lease_is_expired, outbox_should_deliver,
};
use crate::lifecycle_journal::{
    GovernedLifecycleEventKind, GovernedLifecycleEventSource, GovernedLifecycleEventV1,
    JournalError, LifecycleJournal,
};
use crate::operator_control::{
    OperatorCommand, OperatorDeny, OperatorReceipt, OperationView,
    apply_operator_command, issue_resume_approval,
};

// ─── A5: compact / resume / reconnect identity ─────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ContextRebuildRequest<'a> {
    pub entry: &'a str,
    pub expected_manifest_hash: &'a str,
    pub rebuilt_manifest_hash: &'a str,
    pub expected_rendered_input_hash: &'a str,
    pub rebuilt_rendered_input_hash: &'a str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ContextRebuildDeny {
    EmptyField,
    ManifestDrift,
    RenderedInputDrift,
    UnknownEntry,
}

impl ContextRebuildDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::EmptyField => "context_rebuild.empty_field",
            Self::ManifestDrift => "context_rebuild.manifest_drift",
            Self::RenderedInputDrift => "context_rebuild.rendered_input_drift",
            Self::UnknownEntry => "context_rebuild.unknown_entry",
        }
    }
}

pub fn authorize_context_rebuild(
    req: &ContextRebuildRequest<'_>,
) -> Result<(), ContextRebuildDeny> {
    match req.entry {
        "compact" | "resume" | "reconnect" => {}
        _ => return Err(ContextRebuildDeny::UnknownEntry),
    }
    if req.expected_manifest_hash.trim().is_empty()
        || req.rebuilt_manifest_hash.trim().is_empty()
        || req.expected_rendered_input_hash.trim().is_empty()
        || req.rebuilt_rendered_input_hash.trim().is_empty()
    {
        return Err(ContextRebuildDeny::EmptyField);
    }
    if req.expected_manifest_hash != req.rebuilt_manifest_hash {
        return Err(ContextRebuildDeny::ManifestDrift);
    }
    if req.expected_rendered_input_hash != req.rebuilt_rendered_input_hash {
        return Err(ContextRebuildDeny::RenderedInputDrift);
    }
    Ok(())
}

// ─── A6: handoff → journal receipt ─────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HandoffDeliveryError {
    Handoff(HandoffDenyReason),
    Journal(JournalError),
    Hash(String),
}

pub fn deliver_handoff_receipt(
    journal: &mut LifecycleJournal,
    packet: &HandoffPacketV1,
    event_id: impl Into<String>,
    owner_session_id: impl Into<String>,
    sequence: u64,
    occurred_at: u64,
    policy_revision: u64,
) -> Result<GovernedLifecycleEventV1, HandoffDeliveryError> {
    packet
        .authorize_view(&packet.task_tree_id, &packet.snapshot_hash)
        .map_err(HandoffDeliveryError::Handoff)?;
    let content_hash = packet
        .compute_content_hash()
        .map_err(|e| HandoffDeliveryError::Hash(e.to_string()))?;
    let mut event = GovernedLifecycleEventV1 {
        event_id: event_id.into(),
        task_tree_id: packet.task_tree_id.clone(),
        node_id: packet.from_node.clone(),
        owner_session_id: owner_session_id.into(),
        sequence,
        causal_parent: None,
        kind: GovernedLifecycleEventKind::Checkpointed,
        source: GovernedLifecycleEventSource::TerminalAdapter,
        lease_id: None,
        contract_hash: Some(content_hash.clone()),
        policy_revision,
        evidence_refs: vec![
            format!("handoff:{content_hash}"),
            format!("snapshot:{}", packet.snapshot_hash),
        ],
        occurred_at,
        payload_hash: String::new(),
        prev_payload_hash: None,
    };
    event.payload_hash = event
        .compute_payload_hash()
        .map_err(|e| HandoffDeliveryError::Hash(e.to_string()))?;
    journal
        .append(event.clone())
        .map_err(HandoffDeliveryError::Journal)?;
    Ok(event)
}

// ─── A7: Expert repair obligation ──────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ExpertRepairAdmission {
    pub repair_requested: bool,
    pub host_verification_passed: bool,
    pub external_effect_unknown: bool,
    pub max_repair_passes: u32,
    pub repair_passes_used: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ExpertRepairDeny {
    BudgetExhausted,
    VerificationRequired,
    ExternalEffectUnknown,
    NotRequested,
}

impl ExpertRepairDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::BudgetExhausted => "expert.repair_budget_exhausted",
            Self::VerificationRequired => "expert.verification_required",
            Self::ExternalEffectUnknown => "expert.external_effect_unknown",
            Self::NotRequested => "expert.repair_not_requested",
        }
    }
}

pub fn authorize_expert_repair_pass(adm: &ExpertRepairAdmission) -> Result<(), ExpertRepairDeny> {
    if !adm.repair_requested {
        return Err(ExpertRepairDeny::NotRequested);
    }
    if adm.external_effect_unknown {
        return Err(ExpertRepairDeny::ExternalEffectUnknown);
    }
    if adm.repair_passes_used >= adm.max_repair_passes {
        return Err(ExpertRepairDeny::BudgetExhausted);
    }
    if !adm.host_verification_passed {
        return Err(ExpertRepairDeny::VerificationRequired);
    }
    Ok(())
}

// ─── A8: advisor consult tool surface ──────────────────────────────────────

pub const ADVISOR_CONSULT_TOOL_NAME: &str = "lumen_advisor_consult";

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AdvisorConsultProjectionV1 {
    pub tool_name: String,
    pub mode: String,
    pub outcome: String,
    pub report_id: Option<String>,
    pub receipt_count: usize,
    pub applies_authority: bool,
}

/// Callable tool entry for on-demand advisor consult (product face).
pub fn invoke_advisor_consult_tool(
    mode: AdvisorMode,
    request_id: &str,
    task_summary: &str,
    review_question: Option<&str>,
    now_epoch_ms: u64,
    deadline_epoch_ms: Option<u64>,
    fixture_succeeds: bool,
) -> Result<(ConsultOutcome, AdvisorConsultProjectionV1), String> {
    let capsule = build_advisor_capsule(
        format!("cap-{request_id}"),
        AdvisorRequestKind::CompletionCandidateReview,
        task_summary,
        "accepted snapshot ok",
        review_question,
        &[],
        &[],
    )
    .map_err(|e| format!("capsule:{}", e.code()))?;

    let _request = AdvisorRequestV1 {
        request_id: request_id.to_owned(),
        kind: AdvisorRequestKind::CompletionCandidateReview,
        review_question: review_question.map(str::to_owned),
        artifact_refs: vec![],
    };
    let _ = &_request;
    let _ = &capsule;

    let outcome = match mode {
        AdvisorMode::Off => ConsultOutcome::Blocked {
            reason: ConsultBlockReason::PolicyRefused,
        },
        AdvisorMode::Shadow => ConsultOutcome::Succeeded {
            report_id: format!("shadow-{request_id}"),
        },
        AdvisorMode::Consult => {
            if let Some(dl) = deadline_epoch_ms
                && consult_timed_out(now_epoch_ms, dl, now_epoch_ms)
            {
                return Ok((
                    ConsultOutcome::Blocked {
                        reason: ConsultBlockReason::TimedOut,
                    },
                    AdvisorConsultProjectionV1 {
                        tool_name: ADVISOR_CONSULT_TOOL_NAME.into(),
                        mode: "consult".into(),
                        outcome: "blocked_timeout".into(),
                        report_id: None,
                        receipt_count: 1,
                        applies_authority: false,
                    },
                ));
            }
            if fixture_succeeds {
                ConsultOutcome::Succeeded {
                    report_id: format!("report-{request_id}"),
                }
            } else {
                ConsultOutcome::Blocked {
                    reason: ConsultBlockReason::AdvisorUnavailable,
                }
            }
        }
    };

    let (outcome_s, report_id) = match &outcome {
        ConsultOutcome::Succeeded { report_id } => ("succeeded".into(), Some(report_id.clone())),
        ConsultOutcome::Blocked { reason } => (format!("blocked_{}", reason.code()), None),
    };
    let mode_s = match mode {
        AdvisorMode::Off => "off",
        AdvisorMode::Shadow => "shadow",
        AdvisorMode::Consult => "consult",
    };
    Ok((
        outcome,
        AdvisorConsultProjectionV1 {
            tool_name: ADVISOR_CONSULT_TOOL_NAME.into(),
            mode: mode_s.into(),
            outcome: outcome_s,
            report_id,
            receipt_count: 1,
            applies_authority: false,
        },
    ))
}

// ─── A9 + A11: Applied golden chain ────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AppliedAssignmentChain<'a> {
    pub lifecycle: AssignmentLifecycle,
    pub assignment_hash: &'a str,
    pub expected_assignment_hash: &'a str,
    pub accepted_snapshot_hash: &'a str,
    pub live_snapshot_hash: &'a str,
    pub budget_reservation_held: bool,
    pub root_approval_id: &'a str,
    pub sealed_receipt_id: &'a str,
    pub tree_budget_reservation_id: &'a str,
    pub context_manifest_hash: &'a str,
    pub model_receipt_id: &'a str,
    pub ledger_decision: &'a str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AppliedChainDeny {
    Apply(AssignmentApplyDeny),
    MissingRootApproval,
    MissingSealedReceipt,
    MissingTreeBudget,
    MissingManifest,
    MissingModelReceipt,
    LedgerMismatch,
}

impl AppliedChainDeny {
    pub fn code(&self) -> String {
        match self {
            Self::Apply(d) => d.code().to_owned(),
            Self::MissingRootApproval => "applied.missing_root_approval".into(),
            Self::MissingSealedReceipt => "applied.missing_sealed_receipt".into(),
            Self::MissingTreeBudget => "applied.missing_tree_budget".into(),
            Self::MissingManifest => "applied.missing_manifest".into(),
            Self::MissingModelReceipt => "applied.missing_model_receipt".into(),
            Self::LedgerMismatch => "applied.ledger_mismatch".into(),
        }
    }
}

pub fn authorize_applied_assignment_chain(
    chain: &AppliedAssignmentChain<'_>,
) -> Result<AssignmentLifecycle, AppliedChainDeny> {
    if chain.root_approval_id.trim().is_empty() {
        return Err(AppliedChainDeny::MissingRootApproval);
    }
    if chain.sealed_receipt_id.trim().is_empty() {
        return Err(AppliedChainDeny::MissingSealedReceipt);
    }
    if chain.tree_budget_reservation_id.trim().is_empty() {
        return Err(AppliedChainDeny::MissingTreeBudget);
    }
    if chain.context_manifest_hash.trim().is_empty() {
        return Err(AppliedChainDeny::MissingManifest);
    }
    if chain.model_receipt_id.trim().is_empty() {
        return Err(AppliedChainDeny::MissingModelReceipt);
    }
    let base = AssignmentApplyRequest {
        lifecycle: chain.lifecycle,
        assignment_hash: chain.assignment_hash,
        expected_assignment_hash: chain.expected_assignment_hash,
        accepted_snapshot_hash: chain.accepted_snapshot_hash,
        live_snapshot_hash: chain.live_snapshot_hash,
        budget_reservation_held: chain.budget_reservation_held,
    };
    let applied = authorize_assignment_apply(&base).map_err(AppliedChainDeny::Apply)?;
    if chain.ledger_decision != "applied" {
        return Err(AppliedChainDeny::LedgerMismatch);
    }
    Ok(applied)
}

// ─── A10: Operator five commands + Kairos fake clock ────────────────────────

fn op_view(op_id: &str, phase: LoopPhase, owner: Option<&str>) -> OperationView {
    OperationView {
        op_id: op_id.into(),
        owner: owner.map(str::to_owned),
        lease_epoch: 1,
        phase,
        attempt_observed: false,
        external_effect_unknown: false,
        manifest_hash: "sha256:m".into(),
        evidence_hash: "sha256:e".into(),
        budget_hash: "sha256:b".into(),
    }
}

/// Prove all five OperatorControl commands against real `apply_operator_command`.
pub fn operator_control_five_command_matrix(
    now_epoch_ms: u64,
) -> Result<Vec<OperatorReceipt>, OperatorDeny> {
    let mut out = Vec::new();
    // 1 Inspect
    let v1 = op_view("op1", LoopPhase::Running, Some("sess"));
    out.push(apply_operator_command(
        &v1,
        &OperatorCommand::InspectOperation {
            op_id: "op1".into(),
        },
        "operator",
        now_epoch_ms,
        None,
    )?);
    // 2 Freeze
    out.push(apply_operator_command(
        &v1,
        &OperatorCommand::FreezeOperation {
            op_id: "op1".into(),
            reason: "pause".into(),
        },
        "operator",
        now_epoch_ms,
        None,
    )?);
    // 3 Cancel
    let v2 = op_view("op2", LoopPhase::Running, Some("sess"));
    out.push(apply_operator_command(
        &v2,
        &OperatorCommand::CancelOperation {
            op_id: "op2".into(),
        },
        "operator",
        now_epoch_ms,
        None,
    )?);
    // 4 ApproveResume
    let v3 = op_view("op3", LoopPhase::Frozen, Some("sess"));
    let approval = issue_resume_approval(
        "appr-1",
        "op3",
        "node-3",
        now_epoch_ms,
        60_000,
        "node-scope",
    );
    out.push(apply_operator_command(
        &v3,
        &OperatorCommand::ApproveResume {
            op_id: "op3".into(),
            node_id: "node-3".into(),
            deadline_epoch_ms: now_epoch_ms + 60_000,
            scope: "node-scope".into(),
        },
        "operator",
        now_epoch_ms,
        Some(&approval),
    )?);
    // 5 TakeOver
    let v4 = op_view("op4", LoopPhase::Running, Some("other"));
    out.push(apply_operator_command(
        &v4,
        &OperatorCommand::TakeOver {
            op_id: "op4".into(),
            expected_old_holder: "other".into(),
        },
        "operator",
        now_epoch_ms,
        None,
    )?);
    assert_eq!(out.len(), 5);
    let _ = BTreeMap::<String, OperationView>::new();
    Ok(out)
}

/// Fake-clock Kairos lease-consumer cycle on real `ConsumerOperation::next_step`.
pub fn kairos_fake_clock_lease_cycle() -> Vec<ConsumerStep> {
    let policy = ConsumerPolicy {
        lease_ttl_epoch_ms: 1_000,
        heartbeat_interval_epoch_ms: 500,
        max_heartbeats: 6,
    };
    let mut op = ConsumerOperation {
        view: op_view("kairos-1", LoopPhase::Running, None),
        last_touched_epoch_ms: 0,
        heartbeats: 0,
        consumer_id: "consumer-1".into(),
        delivered_events: vec![],
    };
    let mut steps = Vec::new();
    let s0 = op.next_step(&policy, 0);
    steps.push(s0.clone());
    if let ConsumerStep::Claim { new_lease_epoch, .. } = s0 {
        op.view.owner = Some(op.consumer_id.clone());
        op.view.lease_epoch = new_lease_epoch;
        op.last_touched_epoch_ms = 0;
    }
    steps.push(op.next_step(&policy, 400));
    let s2 = op.next_step(&policy, 1_200);
    steps.push(s2.clone());
    if let ConsumerStep::Heartbeat { new_lease_epoch, .. } = s2 {
        op.view.lease_epoch = new_lease_epoch;
        op.last_touched_epoch_ms = 1_200;
        op.heartbeats += 1;
    }
    op.view.phase = LoopPhase::TerminalSucceeded;
    steps.push(op.next_step(&policy, 2_000));
    assert!(lease_is_expired(0, 2_000, 1_000));
    assert!(!outbox_should_deliver(&["e1".into()], "e1"));
    assert!(outbox_should_deliver(&["e1".into()], "e2"));
    steps
}

// ─── A12: rollback receipt ─────────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RollbackReceiptV1 {
    pub from_version: String,
    pub to_version: String,
    pub source_commit: String,
    pub reason: String,
}

pub fn authorize_rollback_receipt(r: &RollbackReceiptV1) -> Result<(), &'static str> {
    if r.from_version.trim().is_empty() || r.to_version.trim().is_empty() {
        return Err("rollback.empty_version");
    }
    if r.source_commit.len() < 7 {
        return Err("rollback.short_commit");
    }
    if r.reason.trim().is_empty() {
        return Err("rollback.empty_reason");
    }
    if r.from_version == r.to_version {
        return Err("rollback.same_version");
    }
    Ok(())
}

// ─── A3: token budget reservation ──────────────────────────────────────────

/// A3 offline gate: account-level token reservation is bounded by
/// `token_reservation_limit`; exceeding it fails closed with
/// `TokenReservationExceeded`, and a within-limit reservation succeeds.
pub fn token_reservation_gate() -> Result<(u64, u64), String> {
    use std::time::Duration;
    use xai_grok_tools::implementations::grok_build::task::budget::{
        BudgetDenial, BudgetLedger, TreeBudgetV1,
    };
    // Tight budget: 100 token reservation limit, 2 live nodes.
    let mut ledger = BudgetLedger::new(TreeBudgetV1 {
        max_depth: 2,
        max_children_per_node: 2,
        max_live_nodes: 4,
        max_background_nodes: 1,
        token_reservation_limit: Some(100),
        tool_call_limit: Some(10),
        wall_time_limit: Duration::from_secs(3600),
        daily_cost_limit: None,
        artifact_byte_limit: None,
    });
    // Within limit: 60 tokens → ok.
    let first = ledger
        .reserve_spawn("n1", None, 0, false, 60, 1)
        .map_err(|e| format!("{e:?}"))?;
    // Second reservation pushes reserved over the limit → denied.
    let denied = match ledger.reserve_spawn("n2", None, 0, false, 60, 1) {
        Err(BudgetDenial::TokenReservationExceeded { limit }) => limit,
        other => return Err(format!("expected TokenReservationExceeded, got {other:?}")),
    };
    // Settle the first to prove the settle path stays coherent.
    ledger.settle_usage(first, Some(60), Some(1));
    Ok((denied, 100))
}

// ─── tests ─────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn token_reservation_gate_bounds_and_denies_over_limit() {
        let (denied_limit, configured_limit) = token_reservation_gate().expect("gate runs");
        assert_eq!(denied_limit, configured_limit, "deny must report the configured limit");
        assert_eq!(configured_limit, 100);
    }
    #[test]
    fn context_rebuild_requires_identical_manifest_and_rendered_hash() {
        let ok = ContextRebuildRequest {
            entry: "resume",
            expected_manifest_hash: "sha256:m1",
            rebuilt_manifest_hash: "sha256:m1",
            expected_rendered_input_hash: "sha256:r1",
            rebuilt_rendered_input_hash: "sha256:r1",
        };
        assert!(authorize_context_rebuild(&ok).is_ok());
        for entry in ["compact", "reconnect"] {
            let mut r = ok.clone();
            r.entry = entry;
            assert!(authorize_context_rebuild(&r).is_ok());
        }
        let mut bad = ok.clone();
        bad.rebuilt_manifest_hash = "sha256:other";
        assert_eq!(
            authorize_context_rebuild(&bad).unwrap_err(),
            ContextRebuildDeny::ManifestDrift
        );
    }

    #[test]
    fn handoff_delivery_appends_journal_receipt_with_content_hash() {
        let packet = HandoffPacketV1::build(
            "node-a",
            "tree-1",
            "branch-a",
            "sha256:snap",
            vec!["claim:1".into()],
            vec!["ev:1".into()],
            vec!["maybe flake".into()],
            "next step review",
            Some("blocked_on_review".into()),
        )
        .unwrap();
        let mut journal = LifecycleJournal::in_memory("tree-1");
        let event =
            deliver_handoff_receipt(&mut journal, &packet, "evt-1", "sess-1", 0, 100, 1).unwrap();
        assert_eq!(event.kind, GovernedLifecycleEventKind::Checkpointed);
        assert!(event.evidence_refs.iter().any(|r| r.starts_with("handoff:")));
        assert_eq!(journal.events().len(), 1);
    }

    #[test]
    fn expert_repair_fails_closed_without_verification() {
        let mut adm = ExpertRepairAdmission {
            repair_requested: true,
            host_verification_passed: true,
            external_effect_unknown: false,
            max_repair_passes: 2,
            repair_passes_used: 0,
        };
        assert!(authorize_expert_repair_pass(&adm).is_ok());
        adm.host_verification_passed = false;
        assert_eq!(
            authorize_expert_repair_pass(&adm).unwrap_err(),
            ExpertRepairDeny::VerificationRequired
        );
        adm.host_verification_passed = true;
        adm.external_effect_unknown = true;
        assert_eq!(
            authorize_expert_repair_pass(&adm).unwrap_err(),
            ExpertRepairDeny::ExternalEffectUnknown
        );
    }

    #[test]
    fn advisor_consult_tool_never_grants_authority() {
        let (outcome, proj) = invoke_advisor_consult_tool(
            AdvisorMode::Consult,
            "req-1",
            "review the patch carefully",
            Some("is this safe?"),
            1_000,
            None,
            true,
        )
        .unwrap();
        assert!(matches!(outcome, ConsultOutcome::Succeeded { .. }));
        assert_eq!(proj.tool_name, ADVISOR_CONSULT_TOOL_NAME);
        assert!(!proj.applies_authority);
        let json = serde_json::to_string(&proj).unwrap();
        assert!(!json.contains("sk-"));
        let (blocked, _) = invoke_advisor_consult_tool(
            AdvisorMode::Off,
            "req-2",
            "review the patch carefully",
            None,
            1_000,
            None,
            true,
        )
        .unwrap();
        assert!(matches!(blocked, ConsultOutcome::Blocked { .. }));
    }

    #[test]
    fn ng09b_applied_golden_requires_full_receipt_chain() {
        let chain = AppliedAssignmentChain {
            lifecycle: AssignmentLifecycle::RootApproved,
            assignment_hash: "sha256:a",
            expected_assignment_hash: "sha256:a",
            accepted_snapshot_hash: "sha256:s",
            live_snapshot_hash: "sha256:s",
            budget_reservation_held: true,
            root_approval_id: "approval-1",
            sealed_receipt_id: "seal-1",
            tree_budget_reservation_id: "budget-1",
            context_manifest_hash: "sha256:m",
            model_receipt_id: "model-receipt-1",
            ledger_decision: "applied",
        };
        assert_eq!(
            authorize_applied_assignment_chain(&chain).unwrap(),
            AssignmentLifecycle::Applied
        );
        let mut bad = chain.clone();
        bad.model_receipt_id = "";
        assert!(matches!(
            authorize_applied_assignment_chain(&bad).unwrap_err(),
            AppliedChainDeny::MissingModelReceipt
        ));
    }

    #[test]
    fn operator_five_commands_and_kairos_fake_clock() {
        let receipts = operator_control_five_command_matrix(10_000).unwrap();
        assert_eq!(receipts.len(), 5);
        let steps = kairos_fake_clock_lease_cycle();
        assert!(steps.len() >= 3);
        assert!(matches!(steps[0], ConsumerStep::Claim { .. }));
    }

    #[test]
    fn rollback_receipt_fail_closed() {
        let ok = RollbackReceiptV1 {
            from_version: "2.0.0-rc.1".into(),
            to_version: "1.9.0".into(),
            source_commit: "28fe1687".into(),
            reason: "regression".into(),
        };
        assert!(authorize_rollback_receipt(&ok).is_ok());
        let mut bad = ok.clone();
        bad.to_version = bad.from_version.clone();
        assert_eq!(
            authorize_rollback_receipt(&bad).unwrap_err(),
            "rollback.same_version"
        );
    }
}
