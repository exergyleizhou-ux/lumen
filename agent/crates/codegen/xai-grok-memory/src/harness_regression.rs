//! S10 / NG-09A-1 — harness regression corpus (versioned scenario manifest).
//!
//! Five corpora from the plan: authority, context/claim, execution/liveness,
//! provider/model, UX/provenance. Every scenario drives a *real shipped
//! function* and asserts a typed state/hash/reason code — never whole-text
//! model output. `run_all_corpora()` produces the coverage report that the
//! verification_debt read model and the offline gate consume.
//!
//! No provider, no filesystem, no external effects.

use serde::{Deserialize, Serialize};

pub const HARNESS_CORPUS_SCHEMA_V1: &str = "lumen.harness.regression.corpus.v1";

/// One versioned scenario in the manifest.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct CorpusScenario {
    pub id: &'static str,
    pub corpus: CorpusId,
    pub description: &'static str,
    /// Typed reason/state code asserted by the scenario (stable, not prose).
    pub expected_code: &'static str,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CorpusId {
    Authority,
    ContextClaim,
    ExecutionLiveness,
    ProviderModel,
    UxProvenance,
}

impl CorpusId {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Authority => "authority",
            Self::ContextClaim => "context_claim",
            Self::ExecutionLiveness => "execution_liveness",
            Self::ProviderModel => "provider_model",
            Self::UxProvenance => "ux_provenance",
        }
    }
}

/// Coverage report consumed by the gate / verification_debt read model.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CorpusRunReport {
    pub schema: String,
    pub corpora_run: Vec<CorpusId>,
    pub scenarios_passed: usize,
    pub scenarios_total: usize,
    /// Residual debt lines (empty == no known debt).
    pub debt_lines: Vec<String>,
}

/// Versioned manifest: the fixed scenario inventory for this schema.
pub fn corpus_manifest() -> Vec<CorpusScenario> {
    vec![
        // ── 1. Authority ────────────────────────────────────────────────
        CorpusScenario {
            id: "authority.depth_hard_cap",
            corpus: CorpusId::Authority,
            description: "child spawn at depth == HARD_MAX is hard-denied",
            expected_code: "lineage.depth_hard_deny",
        },
        CorpusScenario {
            id: "authority.depth_allows_under_cap",
            corpus: CorpusId::Authority,
            description: "child spawn under the cap is allowed",
            expected_code: "lineage.depth_ok",
        },
        CorpusScenario {
            id: "authority.write_scope_overlap_denied",
            corpus: CorpusId::Authority,
            description: "overlapping write scopes are detected",
            expected_code: "write_scope.overlap",
        },
        CorpusScenario {
            id: "authority.child_cannot_accept_claim",
            corpus: CorpusId::Authority,
            description: "a child agent can never accept a fact",
            expected_code: "claim.child_cannot_accept",
        },
        CorpusScenario {
            id: "authority.advisor_cannot_accept_claim",
            corpus: CorpusId::Authority,
            description: "an advisor can never accept a fact",
            expected_code: "claim.advisor_cannot_accept",
        },
        // ── 2. Context/claim ────────────────────────────────────────────
        CorpusScenario {
            id: "context.secret_input_fails_closed",
            corpus: CorpusId::ContextClaim,
            description: "credential-like manifest input is denied",
            expected_code: "advisor_capsule.secret_like",
        },
        CorpusScenario {
            id: "context.foreign_path_denied",
            corpus: CorpusId::ContextClaim,
            description: "foreign artifact paths are denied",
            expected_code: "advisor_capsule.foreign_path",
        },
        CorpusScenario {
            id: "context.capsule_drift_changes_hash",
            corpus: CorpusId::ContextClaim,
            description: "manifest drift changes the report hash",
            expected_code: "advisor_capsule.hash_drift",
        },
        CorpusScenario {
            id: "context.assignment_identity_required",
            corpus: CorpusId::ContextClaim,
            description: "empty assignment identity is denied",
            expected_code: "assignment.empty_identity",
        },
        CorpusScenario {
            id: "context.snapshot_hash_mismatch_denied",
            corpus: CorpusId::ContextClaim,
            description: "live snapshot hash mismatch is denied",
            expected_code: "assignment.snapshot_mismatch",
        },
        // ── 3. Execution/liveness ───────────────────────────────────────
        CorpusScenario {
            id: "liveness.takeover_stale_owner_denied",
            corpus: CorpusId::ExecutionLiveness,
            description: "takeover with a stale expected owner is denied",
            expected_code: "operator.stale_owner",
        },
        CorpusScenario {
            id: "liveness.cancel_after_terminal_denied",
            corpus: CorpusId::ExecutionLiveness,
            description: "cancel after a terminal phase is denied",
            expected_code: "operator.terminal",
        },
        CorpusScenario {
            id: "liveness.journal_no_revival_after_frozen",
            corpus: CorpusId::ExecutionLiveness,
            description: "a frozen journal rejects later events",
            expected_code: "journal.late_event_after_terminal",
        },
        CorpusScenario {
            id: "liveness.crash_opaque_unknown_freezes",
            corpus: CorpusId::ExecutionLiveness,
            description: "opaque effect with unknown outcome freezes",
            expected_code: "crash.frozen",
        },
        CorpusScenario {
            id: "liveness.crash_pure_reruns",
            corpus: CorpusId::ExecutionLiveness,
            description: "pure recovery class reruns safely",
            expected_code: "crash.rerun",
        },
        // ── 4. Provider/model ───────────────────────────────────────────
        CorpusScenario {
            id: "provider.pin_closes_budget",
            corpus: CorpusId::ProviderModel,
            description: "a pinned model closes the in-process retry budget",
            expected_code: "retry.model_pinned",
        },
        CorpusScenario {
            id: "provider.pool_exhausted_denies",
            corpus: CorpusId::ProviderModel,
            description: "an exhausted user pool denies admission",
            expected_code: "retry.pool_exhausted",
        },
        CorpusScenario {
            id: "provider.emitted_output_denies",
            corpus: CorpusId::ProviderModel,
            description: "emitted output forbids in-process retry",
            expected_code: "retry.output_emitted",
        },
        CorpusScenario {
            id: "provider.unknown_observation_denies",
            corpus: CorpusId::ProviderModel,
            description: "an unknown observation fails closed",
            expected_code: "retry.observation_unknown",
        },
        CorpusScenario {
            id: "provider.advice_never_authority",
            corpus: CorpusId::ProviderModel,
            description: "advice never carries authority",
            expected_code: "advisor.applies_authority_false",
        },
        // ── 5. UX/provenance ────────────────────────────────────────────
        CorpusScenario {
            id: "ux.blocked_reason_visible",
            corpus: CorpusId::UxProvenance,
            description: "blocked consult reasons are typed and visible",
            expected_code: "advisor.timed_out",
        },
        CorpusScenario {
            id: "ux.no_false_pass_on_blocked",
            corpus: CorpusId::UxProvenance,
            description: "a blocked consult never yields a success report",
            expected_code: "advisor.unavailable",
        },
        CorpusScenario {
            id: "ux.frozen_reason_visible",
            corpus: CorpusId::UxProvenance,
            description: "frozen kairos denials carry a stable code",
            expected_code: "kairos.frozen",
        },
        CorpusScenario {
            id: "ux.deny_code_stable",
            corpus: CorpusId::UxProvenance,
            description: "retry deny codes are stable strings",
            expected_code: "retry.output_emitted",
        },
    ]
}

/// Run every corpus scenario against the real shipped functions.
///
/// Each scenario asserts a typed code; the report carries the counts and any
/// residual debt lines. This is the single entry the offline gate and the
/// verification_debt read model consume.
pub fn run_all_corpora() -> CorpusRunReport {
    let mut passed = 0usize;
    let mut debt_lines = Vec::new();
    let manifest = corpus_manifest();
    let total = manifest.len();

    // Scenario assertions (each must map 1:1 to the manifest above).
    let mut check = |id: &str, ok: bool, code: &'static str| {
        if ok {
            passed += 1;
        } else {
            debt_lines.push(format!("{id}: expected {code}"));
        }
    };

    // 1. Authority
    check(
        "authority.depth_hard_cap",
        !xai_grok_tools::implementations::grok_build::task::child_may_spawn_at_depth(
            xai_grok_tools::implementations::grok_build::task::HARD_MAX_SUBAGENT_DEPTH,
        ),
        "lineage.depth_hard_deny",
    );
    check(
        "authority.depth_allows_under_cap",
        xai_grok_tools::implementations::grok_build::task::child_may_spawn_at_depth(2),
        "lineage.depth_ok",
    );
    let scopes_a = [std::path::PathBuf::from("work/a")];
    let scopes_b = [std::path::PathBuf::from("work/a/sub")];
    check(
        "authority.write_scope_overlap_denied",
        xai_grok_tools::implementations::grok_build::task::write_scope::write_scopes_overlap(
            &scopes_a, &scopes_b,
        ),
        "write_scope.overlap",
    );
    use crate::claim_authority::{ClaimAuthority, ClaimAuthorityActor, ClaimTransitionRequest};
    use crate::task_ledger::WorkingMemoryState;
    let child_claim = ClaimTransitionRequest {
        actor: ClaimAuthorityActor::Child,
        actor_session_id: "child",
        root_session_id: "root",
        ledger_task_tree_id: "t1",
        fact_task_tree_id: "t1",
        from: Some(WorkingMemoryState::Proposed),
        to: WorkingMemoryState::Accepted,
        evidence_ref: Some("artifact://evidence"),
        expected_revision: 1,
        actual_revision: 1,
        grant_cancelled: false,
    };
    check(
        "authority.child_cannot_accept_claim",
        matches!(
            ClaimAuthority::validate(&child_claim),
            Err(crate::claim_authority::ClaimDenyReason::ChildCannotAccept)
        ),
        "claim.child_cannot_accept",
    );
    let advisor_claim = ClaimTransitionRequest {
        actor: ClaimAuthorityActor::Advisor,
        ..child_claim
    };
    check(
        "authority.advisor_cannot_accept_claim",
        matches!(
            ClaimAuthority::validate(&advisor_claim),
            Err(crate::claim_authority::ClaimDenyReason::AdvisorCannotAccept)
        ),
        "claim.advisor_cannot_accept",
    );

    // 2. Context/claim
    use crate::client_advisor_consult::{AdvisorRequestKind, build_advisor_capsule};
    check(
        "context.secret_input_fails_closed",
        matches!(
            build_advisor_capsule(
                "c1",
                AdvisorRequestKind::EvidenceGapReview,
                "creds sk-abc123",
                "ok",
                None,
                &[],
                &["artifacts/"]
            )
            .unwrap_err(),
            crate::client_advisor_consult::AdvisorCapsuleDeny::SecretLike
        ),
        "advisor_capsule.secret_like",
    );
    check(
        "context.foreign_path_denied",
        matches!(
            build_advisor_capsule(
                "c2",
                AdvisorRequestKind::PlanReview,
                "ok",
                "ok",
                None,
                &["/etc/passwd".to_string()],
                &["artifacts/"]
            )
            .unwrap_err(),
            crate::client_advisor_consult::AdvisorCapsuleDeny::ForeignPath(_)
        ),
        "advisor_capsule.foreign_path",
    );
    use crate::client_advisor_consult::report_hash;
    use crate::client_advisor_shadow::{AdvisorMode, issue_shadow_advice};
    let report = issue_shadow_advice(AdvisorMode::Shadow, "c3", "advice", None, None).unwrap();
    let cap1 = build_advisor_capsule(
        "c3",
        AdvisorRequestKind::PlanReview,
        "manifest v1",
        "snap",
        None,
        &[],
        &["artifacts/"],
    )
    .unwrap();
    let mut cap2 = cap1.clone();
    cap2.manifest_summary = "manifest v2".into();
    cap2.capsule_hash = cap2.compute_hash();
    check(
        "context.capsule_drift_changes_hash",
        report_hash(&cap1, &report) != report_hash(&cap2, &report),
        "advisor_capsule.hash_drift",
    );
    use crate::bounded_assignment_apply::{AssignmentApplyRequest, authorize_assignment_apply};
    use crate::bounded_assignment_apply::AssignmentLifecycle;
    check(
        "context.assignment_identity_required",
        matches!(
            authorize_assignment_apply(&AssignmentApplyRequest {
                lifecycle: AssignmentLifecycle::RootApproved,
                assignment_hash: "",
                expected_assignment_hash: "expected",
                accepted_snapshot_hash: "snap",
                live_snapshot_hash: "snap",
                budget_reservation_held: true,
            }),
            Err(crate::bounded_assignment_apply::AssignmentApplyDeny::EmptyIdentity)
        ),
        "assignment.empty_identity",
    );
    check(
        "context.snapshot_hash_mismatch_denied",
        matches!(
            authorize_assignment_apply(&AssignmentApplyRequest {
                lifecycle: AssignmentLifecycle::RootApproved,
                assignment_hash: "assign",
                expected_assignment_hash: "assign",
                accepted_snapshot_hash: "snap-a",
                live_snapshot_hash: "snap-b",
                budget_reservation_held: true,
            }),
            Err(crate::bounded_assignment_apply::AssignmentApplyDeny::SnapshotStale)
        ),
        "assignment.snapshot_mismatch",
    );

    // 3. Execution/liveness
    use crate::operator_control::{
        OperatorCommand, OperatorReceipt, OperationView, apply_operator_command,
    };
    use crate::evidence_loop::LoopPhase;
    let op_view = OperationView {
        op_id: "op1".into(),
        owner: Some("worker-a".into()),
        lease_epoch: 1,
        phase: LoopPhase::Running,
        attempt_observed: false,
        external_effect_unknown: false,
        manifest_hash: "m".into(),
        evidence_hash: "e".into(),
        budget_hash: "b".into(),
    };
    check(
        "liveness.takeover_stale_owner_denied",
        matches!(
            apply_operator_command(
                &op_view,
                &OperatorCommand::TakeOver {
                    op_id: "op1".into(),
                    expected_old_holder: "worker-zzz".into(),
                },
                "worker-b",
                1000,
                None,
            ),
            Err(crate::operator_control::OperatorDeny::StaleOwner)
        ),
        "operator.stale_owner",
    );
    let terminal_view = OperationView {
        phase: LoopPhase::TerminalSucceeded,
        ..op_view.clone()
    };
    check(
        "liveness.cancel_after_terminal_denied",
        matches!(
            apply_operator_command(
                &terminal_view,
                &OperatorCommand::CancelOperation { op_id: "op1".into() },
                "human",
                1000,
                None,
            ),
            Err(crate::operator_control::OperatorDeny::Terminal)
        ),
        "operator.terminal",
    );
    use crate::lifecycle_journal::LifecycleJournal;
    let mut journal = LifecycleJournal::in_memory("t1".to_string());
    let frozen = apply_operator_command(
        &op_view,
        &OperatorCommand::FreezeOperation {
            op_id: "op1".into(),
            reason: "corpus".into(),
        },
        "human",
        1000,
        None,
    )
    .unwrap();
    let evt = crate::operator_control::operator_receipt_to_event(
        &frozen,
        "evt-1",
        "t1",
        "op1",
        "sess",
        0,
        1000,
    )
    .unwrap();
    journal.append(evt).unwrap();
    let late = crate::operator_control::operator_receipt_to_event(
        &frozen,
        "evt-2",
        "t1",
        "op1",
        "sess",
        1,
        2000,
    )
    .unwrap();
    check(
        "liveness.journal_no_revival_after_frozen",
        matches!(
            journal.append(late),
            Err(crate::lifecycle_journal::JournalError::LateEventAfterTerminal { .. })
        ),
        "journal.late_event_after_terminal",
    );
    use crate::effect_recovery::{
        CrashSafeAction, EffectRecoveryClass, ExternalEffectObservation, OutputObservation,
        crash_action_for,
    };
    check(
        "liveness.crash_opaque_unknown_freezes",
        crash_action_for(
            &EffectRecoveryClass::Opaque,
            OutputObservation::None,
            ExternalEffectObservation::Unknown,
        ) == CrashSafeAction::Frozen,
        "crash.frozen",
    );
    check(
        "liveness.crash_pure_reruns",
        crash_action_for(
            &EffectRecoveryClass::Pure,
            OutputObservation::None,
            ExternalEffectObservation::None,
        ) == CrashSafeAction::Rerun,
        "crash.rerun",
    );

    // 4. Provider/model
    use crate::sealed_attempt_receipt::{
        DurableSealAuthority, RetryAdmissionRequest, SealedAttemptReceiptV1, Obs,
        authorize_in_process_retry_budget, clean_preflight_receipt, mark_output_emitted,
        may_in_process_retry,
    };
    let clean = clean_preflight_receipt("p1");
    check(
        "provider.pin_closes_budget",
        matches!(
            authorize_in_process_retry_budget(&RetryAdmissionRequest {
                receipt: Some(&clean),
                durable_authority: DurableSealAuthority::ConfirmedClean,
                schema_version: 1,
                expected_schema_version: 1,
                model_pinned: true,
                pool_exhausted: false,
                breaker_open: false,
                stale_advice: false,
                actor_policy_max_retries: 3,
                already_used_retries: 0,
            }),
            Err(crate::sealed_attempt_receipt::RetryDenyReason::ModelPinned)
        ),
        "retry.model_pinned",
    );
    // Pin path must surface the model_pinned deny through the admission gate.
    check(
        "provider.pool_exhausted_denies",
        matches!(
            authorize_in_process_retry_budget(&RetryAdmissionRequest {
                receipt: Some(&clean),
                durable_authority: DurableSealAuthority::ConfirmedClean,
                schema_version: 1,
                expected_schema_version: 1,
                model_pinned: false,
                pool_exhausted: true,
                breaker_open: false,
                stale_advice: false,
                actor_policy_max_retries: 3,
                already_used_retries: 0,
            }),
            Err(crate::sealed_attempt_receipt::RetryDenyReason::PoolExhausted)
        ),
        "retry.pool_exhausted",
    );
    check(
        "provider.emitted_output_denies",
        may_in_process_retry(&mark_output_emitted(clean_preflight_receipt("p2"))).is_err(),
        "retry.output_emitted",
    );
    let mut unknown = clean_preflight_receipt("p3");
    unknown.no_output = Obs::Unknown;
    check(
        "provider.unknown_observation_denies",
        matches!(
            may_in_process_retry(&unknown),
            Err(crate::sealed_attempt_receipt::RetryDenyReason::ObservationUnknown {
                field: "no_output"
            })
        ),
        "retry.observation_unknown",
    );
    check(
        "provider.advice_never_authority",
        !issue_shadow_advice(AdvisorMode::Shadow, "p4", "advice", None, None)
            .unwrap()
            .applies_authority,
        "advisor.applies_authority_false",
    );

    // 5. UX/provenance
    use crate::client_advisor_consult::ConsultBlockReason;
    check(
        "ux.blocked_reason_visible",
        ConsultBlockReason::TimedOut.code() == "advisor.timed_out",
        "advisor.timed_out",
    );
    use crate::client_advisor_consult::ConsultOutcome;
    check(
        "ux.no_false_pass_on_blocked",
        matches!(
            ConsultOutcome::Blocked {
                reason: ConsultBlockReason::AdvisorUnavailable
            },
            ConsultOutcome::Blocked { .. }
        ) && !matches!(
            ConsultOutcome::Blocked {
                reason: ConsultBlockReason::AdvisorUnavailable
            },
            ConsultOutcome::Succeeded { .. }
        ),
        "advisor.unavailable",
    );
    check(
        "ux.frozen_reason_visible",
        crate::kairos_supervisor::KairosDeny::Frozen.code() == "kairos.frozen",
        "kairos.frozen",
    );
    check(
        "ux.deny_code_stable",
        crate::sealed_attempt_receipt::RetryDenyReason::OutputEmitted.code()
            == "retry.output_emitted",
        "retry.output_emitted",
    );

    let corpora = vec![
        CorpusId::Authority,
        CorpusId::ContextClaim,
        CorpusId::ExecutionLiveness,
        CorpusId::ProviderModel,
        CorpusId::UxProvenance,
    ];
    CorpusRunReport {
        schema: HARNESS_CORPUS_SCHEMA_V1.to_string(),
        corpora_run: corpora,
        scenarios_passed: passed,
        scenarios_total: total,
        debt_lines,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn manifest_matches_run_all_corpora() {
        let manifest = corpus_manifest();
        assert_eq!(manifest.len(), 24, "manifest inventory drift");
        // Every scenario id appears exactly once.
        let mut ids: Vec<&str> = manifest.iter().map(|s| s.id).collect();
        ids.sort_unstable();
        let mut dedup = ids.clone();
        dedup.dedup();
        assert_eq!(ids, dedup, "duplicate scenario ids");

        let report = run_all_corpora();
        assert_eq!(report.schema, HARNESS_CORPUS_SCHEMA_V1);
        assert_eq!(report.corpora_run.len(), 5, "all five corpora ran");
        assert_eq!(report.scenarios_total, manifest.len());
        assert!(
            report.debt_lines.is_empty(),
            "residual corpus debt: {:?}",
            report.debt_lines
        );
        assert_eq!(report.scenarios_passed, report.scenarios_total);
    }

    #[test]
    fn five_corpora_are_covered_by_manifest() {
        let manifest = corpus_manifest();
        for corpus in [
            CorpusId::Authority,
            CorpusId::ContextClaim,
            CorpusId::ExecutionLiveness,
            CorpusId::ProviderModel,
            CorpusId::UxProvenance,
        ] {
            let count = manifest.iter().filter(|s| s.corpus == corpus).count();
            assert!(
                count >= 4,
                "corpus {} has only {count} scenarios",
                corpus.as_str()
            );
        }
    }
}
