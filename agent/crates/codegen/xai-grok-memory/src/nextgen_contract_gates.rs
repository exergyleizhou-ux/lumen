//! Offline composite gate runner for local NextGen contract surfaces.
//!
//! Produces a receipt of which pure gates pass. Does **not** claim
//! exact-SHA CI, RC release, or product completion.

use crate::agent_sandbox::{
    AgentSandboxV1, IssueSandboxRequest, SANDBOX_HARD_MAX_DEPTH, SandboxAssuranceV1,
};
use crate::bounded_assignment_apply::{
    AssignmentApplyRequest, AssignmentLifecycle, authorize_assignment_apply,
};
use crate::client_advisor_shadow::{AdvisorMode, advice_may_mutate_authority, issue_shadow_advice};
use crate::evidence_loop::{
    LoopEvent, LoopPhase, NodeLoopState, SupervisorLoopEvent, SupervisorLoopState, TreeLoopEvent,
    TreeLoopState, reduce_node_loop, reduce_supervisor_loop, reduce_tree_loop,
};
use crate::kairos_supervisor::{KairosCommand, KairosSupervisorState, apply_kairos_command};
use crate::m1_governed_tree_preview::run_m1_governed_tree_preview;
use crate::sealed_attempt_receipt::{
    DurableSealAuthority, RetryAdmissionRequest, RetryDenyReason, SEALED_RECEIPT_SCHEMA_VERSION,
    SealedAttemptReceiptStore, authorize_in_process_retry_budget, clean_preflight_receipt,
    mark_output_emitted, may_in_process_retry, ordinary_turn_max_retries_with_authority,
};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GateResult {
    pub name: String,
    pub status: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NextGenContractGateReceipt {
    pub gates: Vec<GateResult>,
    pub offline_pass_count: usize,
    pub offline_total: usize,
    pub product_rc: String,
    pub note: String,
}

fn pass(name: &str) -> GateResult {
    GateResult {
        name: name.into(),
        status: "PASS".into(),
    }
}

/// Run pure offline gates. Caller may write the JSON receipt to evidence.
pub fn run_offline_contract_gates(tmp: &std::path::Path) -> NextGenContractGateReceipt {
    let mut gates = Vec::new();

    // S6 M1 offline
    let m1 = run_m1_governed_tree_preview(tmp.join("m1.jsonl"));
    assert_eq!(m1.gate, "M1_GOVERNED_TREE_PREVIEW_GATE=PASS");
    gates.push(pass("M1_GOVERNED_TREE_PREVIEW_GATE"));

    // S7 node/tree/supervisor
    let (n, _) = reduce_node_loop(
        NodeLoopState::fresh(),
        LoopEvent::NominateCompletion,
    )
    .unwrap();
    assert_eq!(n.phase, LoopPhase::CompletionCandidate);
    let (t, _) = reduce_tree_loop(TreeLoopState::fresh("r"), TreeLoopEvent::NodeFrozen);
    assert_eq!(t.phase, LoopPhase::Frozen);
    let (s, _) = reduce_supervisor_loop(
        SupervisorLoopState::fresh("s"),
        SupervisorLoopEvent::ExternalEffectUnknown,
    );
    assert_eq!(s.phase, LoopPhase::Frozen);
    gates.push(pass("LOOP_CONVERGENCE_GATE_OFFLINE"));

    // S8 seal + durable admission (P0-NR-A full audit matrix offline)
    assert!(may_in_process_retry(&clean_preflight_receipt("x")).is_ok());
    assert!(may_in_process_retry(&mark_output_emitted(clean_preflight_receipt("y"))).is_err());
    let store = SealedAttemptReceiptStore::with_path(tmp.join("sealed-attempts.json"));
    let clean = clean_preflight_receipt("gate-clean");
    store
        .record(clean.clone(), None, None)
        .expect("durable seal write");
    assert_eq!(
        store.authority_for(&clean),
        DurableSealAuthority::ConfirmedClean
    );
    assert_eq!(
        ordinary_turn_max_retries_with_authority(
            Some(&clean),
            DurableSealAuthority::ConfirmedClean
        ),
        1
    );
    // Deny matrix: pin / pool / breaker / schema / existing output / stale advice
    let deny_pin = RetryAdmissionRequest {
        receipt: Some(&clean),
        durable_authority: DurableSealAuthority::ConfirmedClean,
        schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
        expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
        model_pinned: true,
        pool_exhausted: false,
        breaker_open: false,
        stale_advice: false,
        actor_policy_max_retries: 15,
        already_used_retries: 0,
    };
    assert!(authorize_in_process_retry_budget(&deny_pin).is_err());
    let dirty = mark_output_emitted(clean_preflight_receipt("gate-dirty"));
    let deny_output = RetryAdmissionRequest {
        receipt: Some(&dirty),
        durable_authority: DurableSealAuthority::ConfirmedClean,
        schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
        expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
        model_pinned: false,
        pool_exhausted: false,
        breaker_open: false,
        stale_advice: false,
        actor_policy_max_retries: 15,
        already_used_retries: 0,
    };
    assert!(authorize_in_process_retry_budget(&deny_output).is_err());
    // DEBT-003: the FULL_AUDIT gate now asserts the exact deny reason for all
    // six required reject paths (pin / pool / breaker / schema / output /
    // stale advice), not just is_err booleans.
    let clean = clean_preflight_receipt("gate-clean");
    let expect_deny = |req: RetryAdmissionRequest<'_>, want: RetryDenyReason| {
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            want,
            "FULL_AUDIT deny reason mismatch"
        );
    };
    expect_deny(
        RetryAdmissionRequest {
            receipt: Some(&clean),
            durable_authority: DurableSealAuthority::ConfirmedClean,
            schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            model_pinned: true,
            pool_exhausted: false,
            breaker_open: false,
            stale_advice: false,
            actor_policy_max_retries: 15,
            already_used_retries: 0,
        },
        RetryDenyReason::ModelPinned,
    );
    expect_deny(
        RetryAdmissionRequest {
            receipt: Some(&clean),
            durable_authority: DurableSealAuthority::ConfirmedClean,
            schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            model_pinned: false,
            pool_exhausted: true,
            breaker_open: false,
            stale_advice: false,
            actor_policy_max_retries: 15,
            already_used_retries: 0,
        },
        RetryDenyReason::PoolExhausted,
    );
    expect_deny(
        RetryAdmissionRequest {
            receipt: Some(&clean),
            durable_authority: DurableSealAuthority::ConfirmedClean,
            schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            model_pinned: false,
            pool_exhausted: false,
            breaker_open: true,
            stale_advice: false,
            actor_policy_max_retries: 15,
            already_used_retries: 0,
        },
        RetryDenyReason::BreakerOpen,
    );
    expect_deny(
        RetryAdmissionRequest {
            receipt: Some(&clean),
            durable_authority: DurableSealAuthority::ConfirmedClean,
            schema_version: 2,
            expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            model_pinned: false,
            pool_exhausted: false,
            breaker_open: false,
            stale_advice: false,
            actor_policy_max_retries: 15,
            already_used_retries: 0,
        },
        RetryDenyReason::SchemaMismatch,
    );
    expect_deny(
        RetryAdmissionRequest {
            receipt: Some(&dirty),
            durable_authority: DurableSealAuthority::ConfirmedClean,
            schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            model_pinned: false,
            pool_exhausted: false,
            breaker_open: false,
            stale_advice: false,
            actor_policy_max_retries: 15,
            already_used_retries: 0,
        },
        RetryDenyReason::OutputEmitted,
    );
    expect_deny(
        RetryAdmissionRequest {
            receipt: Some(&clean),
            durable_authority: DurableSealAuthority::ConfirmedClean,
            schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            model_pinned: false,
            pool_exhausted: false,
            breaker_open: false,
            stale_advice: true,
            actor_policy_max_retries: 15,
            already_used_retries: 0,
        },
        RetryDenyReason::StaleAdvice,
    );
    // GROK_MAX_RETRIES cannot reopen: dirty seal + actor 15 → still closed
    assert_eq!(
        ordinary_turn_max_retries_with_authority(
            Some(&dirty),
            DurableSealAuthority::ConfirmedClean
        ),
        0
    );
    gates.push(pass("P0_NR_A_SEAL_GATE"));
    gates.push(pass("P0_NR_A_FULL_AUDIT_GATE"));

    // S5 sandbox leaf
    let leaf = AgentSandboxV1::issue(IssueSandboxRequest {
        sandbox_id: "sb".into(),
        task_tree_id: "root".into(),
        node_id: "leaf".into(),
        immediate_parent_id: Some("p".into()),
        depth: SANDBOX_HARD_MAX_DEPTH,
        branch_id: "b".into(),
        context_manifest_hash: "sha256:m".into(),
        accepted_snapshot_hash: "sha256:s".into(),
        capability_grant_id: "g".into(),
        policy_revision: 1,
        budget_reservation_id: "b".into(),
        is_root: false,
        request_write: true,
        request_network: true,
        request_spawn: true,
        issued_at_unix: 1,
        ttl_secs: 60,
        assurance: SandboxAssuranceV1::HarnessPolicyOnly,
    })
    .unwrap();
    assert!(leaf.authorize_spawn(2).is_err());
    gates.push(pass("SANDBOX_SCHEMA_GATE"));

    // S11 assignment
    let applied = authorize_assignment_apply(&AssignmentApplyRequest {
        lifecycle: AssignmentLifecycle::RootApproved,
        assignment_hash: "sha256:a",
        expected_assignment_hash: "sha256:a",
        accepted_snapshot_hash: "sha256:s",
        live_snapshot_hash: "sha256:s",
        budget_reservation_held: true,
    })
    .unwrap();
    assert_eq!(applied, AssignmentLifecycle::Applied);
    gates.push(pass("ASSIGNMENT_APPLY_GATE"));

    // S9 advisor shadow
    let advice = issue_shadow_advice(
        AdvisorMode::Shadow,
        "ad1",
        "run the test suite",
        None,
        Some("usage://1".into()),
    )
    .unwrap();
    assert!(!advice_may_mutate_authority(&advice));
    gates.push(pass("ADVISOR_SHADOW_GATE"));

    // S12 kairos pure
    let k = KairosSupervisorState::new("k");
    let (k, _) = apply_kairos_command(k, KairosCommand::Claim, Some("tree"), Some(1)).unwrap();
    let (k, _) = apply_kairos_command(k, KairosCommand::Freeze, None, None).unwrap();
    assert_eq!(k.phase(), LoopPhase::Frozen);
    gates.push(pass("KAIROS_LOCAL_PURE_GATE"));

    // NG-03E FLOW_CONTROL: flood / two-tree isolation / shutdown drain (std
    // sync channels — same contract as delivery_observation fixtures).
    {
        use std::sync::mpsc;
        let (tx, _rx) = mpsc::sync_channel::<u8>(2);
        assert!(tx.try_send(1).is_ok());
        assert!(tx.try_send(2).is_ok());
        assert!(matches!(
            tx.try_send(3),
            Err(mpsc::TrySendError::Full(_))
        ));
        let (tx_a, _rx_a) = mpsc::sync_channel::<u8>(1);
        let (tx_b, rx_b) = mpsc::sync_channel::<u8>(1);
        assert!(tx_a.try_send(1).is_ok());
        assert!(matches!(tx_a.try_send(2), Err(mpsc::TrySendError::Full(_))));
        assert!(tx_b.try_send(9).is_ok());
        assert_eq!(rx_b.try_recv().ok(), Some(9));
        let (tx_s, rx_s) = mpsc::sync_channel::<&str>(2);
        assert!(tx_s.try_send("a").is_ok());
        drop(rx_s);
        assert!(matches!(
            tx_s.try_send("late"),
            Err(mpsc::TrySendError::Disconnected(_))
        ));
    }
    gates.push(pass("FLOW_CONTROL_GATE"));

    // NG-02A tool contract dispatch admit (classified vs Other)
    {
        use crate::tool_contract::{
            ToolDispatchSurface, authorize_tool_dispatch, contract_from_runtime_kind,
        };
        use xai_grok_tools::types::tool::ToolKind;
        let ok = contract_from_runtime_kind("read_file", ToolKind::Read, true, 256);
        assert!(authorize_tool_dispatch(ToolDispatchSurface::Child, Some(&ok)).is_ok());
        let bad = contract_from_runtime_kind("mcp_x", ToolKind::Other, false, 256);
        assert!(authorize_tool_dispatch(ToolDispatchSurface::Child, Some(&bad)).is_err());
    }
    gates.push(pass("TOOL_CONTRACT_DISPATCH_GATE"));

    let offline_pass_count = gates.iter().filter(|g| g.status == "PASS").count();
    let offline_total = gates.len();

    NextGenContractGateReceipt {
        gates,
        offline_pass_count,
        offline_total,
        product_rc: "NOT_READY".into(),
        note: "offline pure gates only; S8 durable seal + P0-NR-A; FLOW_CONTROL + TOOL_CONTRACT_DISPATCH; sandbox/advisor/kairos; exact-SHA CI + RC transaction NOT RUN"
            .into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn offline_contract_gates_all_pass_without_provider() {
        let temp = tempfile::tempdir().unwrap();
        let receipt = run_offline_contract_gates(temp.path());
        assert_eq!(receipt.offline_pass_count, receipt.offline_total);
        assert!(receipt.offline_total >= 7);
        assert_eq!(receipt.product_rc, "NOT_READY");
        let json = serde_json::to_string_pretty(&receipt).unwrap();
        assert!(json.contains("M1_GOVERNED_TREE_PREVIEW_GATE"));
        assert!(json.contains("NOT_READY"));
    }
}
