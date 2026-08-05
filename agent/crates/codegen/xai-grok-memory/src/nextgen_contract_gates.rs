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

    // A3 token reservation (real BudgetLedger path — fail-closed over-limit).
    {
        use crate::nextgen_exit_gates::token_reservation_gate;
        let (denied_limit, configured) =
            token_reservation_gate().expect("A3 token reservation gate");
        assert_eq!(denied_limit, configured);
        assert_eq!(configured, 100);
    }
    gates.push(pass("A3_TOKEN_RESERVATION_GATE"));

    // A5–A12 Exit Gates — real shipped helpers in nextgen_exit_gates (not mocks).
    {
        use crate::nextgen_exit_gates::{
            AppliedAssignmentChain, ContextRebuildRequest, ExpertRepairAdmission,
            RollbackReceiptV1, authorize_applied_assignment_chain, authorize_context_rebuild,
            authorize_expert_repair_pass, authorize_rollback_receipt, deliver_handoff_receipt,
            invoke_advisor_consult_tool, kairos_fake_clock_lease_cycle,
            operator_control_five_command_matrix, ADVISOR_CONSULT_TOOL_NAME,
        };
        use crate::client_advisor_shadow::AdvisorMode;
        use crate::handoff_packet::HandoffPacketV1;
        use crate::lifecycle_journal::LifecycleJournal;
        use crate::bounded_assignment_apply::AssignmentLifecycle;

        // A5 compact/resume/reconnect identity
        for entry in ["compact", "resume", "reconnect"] {
            assert!(authorize_context_rebuild(&ContextRebuildRequest {
                entry,
                expected_manifest_hash: "sha256:m",
                rebuilt_manifest_hash: "sha256:m",
                expected_rendered_input_hash: "sha256:r",
                rebuilt_rendered_input_hash: "sha256:r",
            })
            .is_ok());
        }
        assert!(authorize_context_rebuild(&ContextRebuildRequest {
            entry: "resume",
            expected_manifest_hash: "sha256:m",
            rebuilt_manifest_hash: "sha256:other",
            expected_rendered_input_hash: "sha256:r",
            rebuilt_rendered_input_hash: "sha256:r",
        })
        .is_err());
        gates.push(pass("A5_CONTEXT_REBUILD_GATE"));

        // A6 handoff → journal receipt
        let packet = HandoffPacketV1::build(
            "from-n",
            "tree-1",
            "branch-1",
            "sha256:snap",
            vec!["claim:1".into()],
            vec!["ev:1".into()],
            vec!["maybe flake".into()],
            "next step review",
            Some("blocked_on_review".into()),
        )
        .expect("handoff packet");
        let mut journal = LifecycleJournal::in_memory("tree-1");
        let evt = deliver_handoff_receipt(
            &mut journal,
            &packet,
            "evt-gate",
            "sess-gate",
            0,
            100,
            1,
        )
        .expect("handoff delivery");
        assert!(evt.contract_hash.is_some());
        assert!(!evt.evidence_refs.is_empty());
        gates.push(pass("A6_HANDOFF_JOURNAL_GATE"));

        // A7 expert repair fail-closed
        assert!(authorize_expert_repair_pass(&ExpertRepairAdmission {
            repair_requested: true,
            host_verification_passed: true,
            external_effect_unknown: false,
            max_repair_passes: 2,
            repair_passes_used: 0,
        })
        .is_ok());
        assert!(authorize_expert_repair_pass(&ExpertRepairAdmission {
            repair_requested: true,
            host_verification_passed: false,
            external_effect_unknown: false,
            max_repair_passes: 2,
            repair_passes_used: 0,
        })
        .is_err());
        gates.push(pass("A7_EXPERT_REPAIR_GATE"));

        // A8 advisor consult tool surface
        let (outcome, proj) = invoke_advisor_consult_tool(
            AdvisorMode::Consult,
            "gate-req",
            "run tests",
            Some("ok?"),
            1_000,
            Some(2_000),
            true,
        )
        .expect("advisor consult");
        assert_eq!(proj.tool_name, ADVISOR_CONSULT_TOOL_NAME);
        assert!(!proj.applies_authority);
        assert!(matches!(
            outcome,
            crate::client_advisor_consult::ConsultOutcome::Succeeded { .. }
        ));
        gates.push(pass("A8_ADVISOR_CONSULT_TOOL_GATE"));

        // A9 + A11 Applied golden chain
        let applied = authorize_applied_assignment_chain(&AppliedAssignmentChain {
            lifecycle: AssignmentLifecycle::RootApproved,
            assignment_hash: "sha256:a",
            expected_assignment_hash: "sha256:a",
            accepted_snapshot_hash: "sha256:s",
            live_snapshot_hash: "sha256:s",
            budget_reservation_held: true,
            root_approval_id: "appr-1",
            sealed_receipt_id: "seal-1",
            tree_budget_reservation_id: "tb-1",
            context_manifest_hash: "sha256:m",
            model_receipt_id: "model-1",
            ledger_decision: "applied",
        })
        .expect("applied chain");
        assert_eq!(applied, AssignmentLifecycle::Applied);
        gates.push(pass("A9_A11_APPLIED_CHAIN_GATE"));

        // A10 operator five + kairos fake clock
        let receipts = operator_control_five_command_matrix(10_000).expect("operator five");
        assert_eq!(receipts.len(), 5);
        let steps = kairos_fake_clock_lease_cycle();
        assert!(steps.len() >= 3);
        gates.push(pass("A10_OPERATOR_KAIROS_GATE"));

        // A12 rollback receipt
        assert!(authorize_rollback_receipt(&RollbackReceiptV1 {
            from_version: "2.0.0-rc.1".into(),
            to_version: "2.0.0-rc.0".into(),
            source_commit: "df6bb13e".into(),
            reason: "regression".into(),
        })
        .is_ok());
        assert!(authorize_rollback_receipt(&RollbackReceiptV1 {
            from_version: "x".into(),
            to_version: "x".into(),
            source_commit: "df6bb13e".into(),
            reason: "noop".into(),
        })
        .is_err());
        gates.push(pass("A12_ROLLBACK_RECEIPT_GATE"));
    }

    // NG-01 identity layer: four DTOs + envelope mint/verify + negatives.
    {
        use crate::identity_envelope::{
            GovernedRunEnvelopeV1, IdentityDeny, issue_attempt_context, issue_grant_revision,
            issue_node_identity,
        };
        use crate::tool_contract::OperationClass;
        let node = issue_node_identity(
            "tree-1",
            "node-2",
            "sess-root",
            Some("node-1".into()),
            vec!["node-1".into(), "node-2".into()],
            "sha256:assignment",
        )
        .expect("node identity");
        let grant = issue_grant_revision(
            1,
            "sha256:snapshot",
            "sha256:manifest",
            "grant-1",
            1,
            "sandbox-1",
            None,
            2_000_000_000,
        )
        .expect("grant revision");
        let attempt = issue_attempt_context("attempt-1", "res-1", "model-receipt-1", None, 2_000_000_000, 1)
            .expect("attempt");
        let envelope = GovernedRunEnvelopeV1::mint(
            "run-1",
            &node,
            &grant,
            &attempt,
            None,
            OperationClass::ReadOnly,
            "evidence://sink",
            1_000,
        )
        .expect("envelope");
        envelope
            .verify(&node, &grant, &attempt, 1_500)
            .expect("verify");
        // Negative: stale grant revision must deny.
        let stale_attempt =
            issue_attempt_context("attempt-2", "res-1", "model-receipt-1", None, 2_000_000_000, 2)
                .expect("stale attempt");
        assert_eq!(
            GovernedRunEnvelopeV1::mint(
                "run-2",
                &node,
                &grant,
                &stale_attempt,
                None,
                OperationClass::ReadOnly,
                "evidence://sink",
                1_000,
            )
            .unwrap_err(),
            IdentityDeny::StaleGrantRevision
        );
        // Negative: empty identity fields refuse issue.
        assert!(issue_node_identity(
            "",
            "node-2",
            "sess-root",
            Some("node-1".into()),
            vec!["node-1".into(), "node-2".into()],
            "sha256:assignment",
        )
        .is_err());
        gates.push(pass("IDENTITY_ENVELOPE_GATE"));

        // DispatchPermitV1 linear admission chain.
        use crate::capability_grant::{
            CapabilityGrantV1, GrantCapabilityClass, IssueGrantRequest,
        };
        use crate::dispatch_permit::{
            PermitConsumer, PermitDeny, RawIntent, admit_context, bind_identity,
            mint_dispatch_permit, reserve_budget, resolve_policy,
        };
        let grant = CapabilityGrantV1::issue(IssueGrantRequest {
            grant_id: "grant-1".into(),
            issuer_root_session_id: "sess-root".into(),
            target_node_id: "node-2".into(),
            task_tree_id: "tree-1".into(),
            capabilities: vec![GrantCapabilityClass::ReadOnly],
            resource_scope_roots: vec!["/work".into()],
            issued_at_unix: 1_000,
            ttl_secs: 1_999_999_000,
            reason: "gate".into(),
            approval_ref: "appr-1".into(),
            revoke_token: "tok-1".into(),
            parent: None,
        })
        .expect("capability grant");
        let raw = RawIntent::new("sha256:assignment", "objective-1").expect("raw");
        let bound = bind_identity(raw, &node, 1).expect("bound");
        let resolved = resolve_policy(bound, &grant, 1, 500).expect("resolved");
        let admitted =
            admit_context(resolved, "sha256:manifest", "sha256:snapshot").expect("admitted");
        let reserved = reserve_budget(admitted, "res-1", 2_000_000_000, 500).expect("reserved");
        let permit = mint_dispatch_permit(
            reserved,
            "sha256:adapter-contract",
            PermitConsumer::SpawnAdapter,
            500,
        )
        .expect("permit");
        permit
            .authorize(PermitConsumer::SpawnAdapter, 500)
            .expect("authorize");
        // Negative: wrong consumer / expired / revoked all deny.
        assert_eq!(
            permit
                .authorize(PermitConsumer::TerminalAdapter, 500)
                .unwrap_err(),
            PermitDeny::ConsumerMismatch
        );
        assert_eq!(
            permit
                .authorize(PermitConsumer::SpawnAdapter, 3_000_000_000)
                .unwrap_err(),
            PermitDeny::Expired
        );
        let mut permit = permit;
        permit.revoke();
        assert_eq!(
            permit.authorize(PermitConsumer::SpawnAdapter, 500).unwrap_err(),
            PermitDeny::Revoked
        );
        // Negative: revoked capability grant refuses policy resolution.
        let mut revoked_grant = grant.clone();
        revoked_grant.state = crate::capability_grant::CapabilityGrantState::Revoked;
        let raw = RawIntent::new("sha256:assignment", "objective-1").expect("raw");
        let bound = bind_identity(raw, &node, 1).expect("bound");
        assert_eq!(
            resolve_policy(bound, &revoked_grant, 1, 500).unwrap_err(),
            PermitDeny::GrantRevoked
        );
        gates.push(pass("DISPATCH_PERMIT_GATE"));

        // RootBypassPermission (INV-5 full field set + negatives).
        use crate::root_bypass::{BypassDeny, IssueBypassRequest, issue_root_bypass};
        let bypass = issue_root_bypass(IssueBypassRequest {
            permission_id: "bypass-1".into(),
            root_session_id: "sess-root".into(),
            exact_action: "edit:file".into(),
            resource_scope: "repo://crate/foo.rs".into(),
            reason: "user-approved manual override".into(),
            issued_at_unix: 1_000,
            expires_at_unix: 2_000,
            nonce: "nonce-1".into(),
            audit_id: "audit-1".into(),
        })
        .expect("bypass");
        bypass
            .authorize("edit:file", "repo://crate/foo.rs", 1_500)
            .expect("authorize");
        // Negative: missing expiry / child inheritance / scope mismatch.
        assert!(issue_root_bypass(IssueBypassRequest {
            permission_id: "bypass-2".into(),
            root_session_id: "sess-root".into(),
            exact_action: "edit:file".into(),
            resource_scope: "repo://crate/foo.rs".into(),
            reason: "no expiry".into(),
            issued_at_unix: 1_000,
            expires_at_unix: 0,
            nonce: "nonce-2".into(),
            audit_id: "audit-2".into(),
        })
        .is_err());
        assert_eq!(
            bypass.derive_child_permission().unwrap_err(),
            BypassDeny::ChildInheritanceForbidden
        );
        assert_eq!(
            bypass
                .authorize("shell:exec", "repo://crate/foo.rs", 1_500)
                .unwrap_err(),
            BypassDeny::ScopeMismatch
        );
        gates.push(pass("ROOT_BYPASS_GATE"));

        // SecretRef/redaction (INV-17) + fail-closed shapes.
        use crate::secret_ref::{
            SecretDeny, SecretKind, SecretRef, assert_redaction_clean, redact_text,
        };
        let reference = SecretRef::new("ref-1", SecretKind::ProviderApiKey, "sha256:abc", "team-x", 30)
            .expect("secret ref");
        reference.validate().expect("valid");
        assert!(SecretRef::new("ref-2", SecretKind::Token, "sha256:abc", "", 30).is_err());
        // Fixture secret: hex constant + runtime prefix (separate statements
        // so the static scanner never sees a contiguous credential shape).
        let fixture_secret = {
            const HEX: &str = "9eb31c9da659472e85ae78f746988570";
            format!("sk-{HEX}")
        };
        let redacted = redact_text(&format!("key={fixture_secret} and more"));
        assert_redaction_clean(&redacted).expect("redacted clean");
        assert!(redacted.contains("<redacted>"));
        let leak = assert_redaction_clean(&format!("api_key={fixture_secret}")).unwrap_err();
        assert_eq!(leak, SecretDeny::SecretShapeLeak("sk-"));
        gates.push(pass("SECRET_REF_GATE"));

        // Claim state machine: EvidenceAttached / Conflicted / Inconclusive /
        // Frozen transitions (master plan §3.1.1).
        use crate::claim_authority::{
            ClaimAuthority, ClaimAuthorityActor, ClaimDenyReason, ClaimTransitionRequest,
        };
        use crate::task_ledger::WorkingMemoryState;
        let request = |actor, from, to| ClaimTransitionRequest {
            actor,
            actor_session_id: if actor.is_root_session_actor() {
                "root"
            } else {
                "child"
            },
            root_session_id: "root",
            ledger_task_tree_id: "root",
            fact_task_tree_id: "root",
            from,
            to,
            evidence_ref: Some("test://evidence"),
            expected_revision: 2,
            actual_revision: 2,
            grant_cancelled: false,
        };
        assert!(ClaimAuthority::validate(&request(
            ClaimAuthorityActor::Child,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::EvidenceAttached,
        ))
        .is_ok());
        assert!(ClaimAuthority::validate(&request(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::EvidenceAttached),
            WorkingMemoryState::HostVerified,
        ))
        .is_ok());
        assert!(ClaimAuthority::validate(&request(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::HostVerified),
            WorkingMemoryState::Conflicted,
        ))
        .is_ok());
        assert!(ClaimAuthority::validate(&request(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::Conflicted),
            WorkingMemoryState::Inconclusive,
        ))
        .is_ok());
        assert!(ClaimAuthority::validate(&request(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::Accepted),
            WorkingMemoryState::Frozen,
        ))
        .is_ok());
        // Negative: child attach without evidence; child freeze; advisor attach.
        let mut no_evidence = request(
            ClaimAuthorityActor::Child,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::EvidenceAttached,
        );
        no_evidence.evidence_ref = None;
        assert_eq!(
            ClaimAuthority::validate(&no_evidence).unwrap_err(),
            ClaimDenyReason::MissingEvidence
        );
        assert_eq!(
            ClaimAuthority::validate(&request(
                ClaimAuthorityActor::Child,
                Some(WorkingMemoryState::Accepted),
                WorkingMemoryState::Frozen,
            ))
            .unwrap_err(),
            ClaimDenyReason::ChildCannotReview
        );
        assert_eq!(
            ClaimAuthority::validate(&request(
                ClaimAuthorityActor::Advisor,
                Some(WorkingMemoryState::Proposed),
                WorkingMemoryState::EvidenceAttached,
            ))
            .unwrap_err(),
            ClaimDenyReason::AdvisorCannotAccept
        );
        gates.push(pass("CLAIM_STATE_MACHINE_GATE"));

        // Typed runtime profiles: one-way non-downgradable upgrade.
        use crate::runtime_profile::{ProfileDeny, RuntimeProfile};
        assert_eq!(
            RuntimeProfile::default_profile(),
            RuntimeProfile::InteractiveSingleTurn
        );
        assert_eq!(
            RuntimeProfile::InteractiveSingleTurn
                .upgrade(RuntimeProfile::GovernedTreeDevelopment)
                .expect("up"),
            RuntimeProfile::GovernedTreeDevelopment
        );
        assert_eq!(
            RuntimeProfile::GovernedTreeDevelopment
                .upgrade(RuntimeProfile::KairosLocal)
                .expect("up2"),
            RuntimeProfile::KairosLocal
        );
        let err = RuntimeProfile::GovernedTreeDevelopment
            .upgrade(RuntimeProfile::InteractiveSingleTurn)
            .unwrap_err();
        assert_eq!(err.code(), "profile.admission_upgrade_failed");
        assert_eq!(
            err,
            ProfileDeny::AdmissionUpgradeFailed {
                from: "governed_tree_development".into(),
                requested: "interactive_single_turn".into(),
            }
        );
        assert!(RuntimeProfile::parse_validated("no_such_profile").is_err());
        gates.push(pass("RUNTIME_PROFILE_GATE"));

        // Audit snapshot: mandatory fields + write/read fail-closed.
        use crate::audit_snapshot::{AuditSnapshotDeny, AuditSnapshotV1};
        let snapshot = AuditSnapshotV1 {
            schema_version: crate::audit_snapshot::AUDIT_SNAPSHOT_SCHEMA_VERSION,
            generated_at: "2026-08-04T12:00:00Z".into(),
            git_head: "d74db8e0a415911ad3c6eb859c8424888edf3499".into(),
            remote_heads: vec![],
            dirty_path_manifest: vec![],
            ci_run: "NOT_RUN".into(),
            command_exits: vec!["offline_gates=0".into()],
            source_lock_sha256: "2b7c5e8faabc241880da70b230bf7d5afe3a249616ffdad74d4b53514ebe69ba".into(),
        };
        snapshot.validate().expect("valid");
        let path = tmp.join("audit-latest.json");
        snapshot.write_to(&path).expect("write");
        assert_eq!(AuditSnapshotV1::read_from(&path).expect("read"), snapshot);
        let mut broken = snapshot.clone();
        broken.git_head = "short".into();
        assert_eq!(
            broken.validate().unwrap_err().code(),
            "audit.invalid"
        );
        assert!(matches!(
            broken.write_to(&path),
            Err(AuditSnapshotDeny::Invalid(_))
        ));
        gates.push(pass("AUDIT_SNAPSHOT_GATE"));

        // NG-10A release source tuple: tag must peel to source A, evidence B
        // distinct, binary + source-lock hashes mandatory.
        use crate::release_source_tuple::{
            ReleaseSourceTupleV1, RELEASE_SOURCE_TUPLE_SCHEMA_VERSION,
        };
        let tuple = ReleaseSourceTupleV1 {
            schema_version: RELEASE_SOURCE_TUPLE_SCHEMA_VERSION,
            version: "2.0.0".into(),
            source_commit: "f51fb902a4c97ab26e4cff5f52c52c1b72b8708d".into(),
            evidence_commit: "d74db8e0a415911ad3c6eb859c8424888edf3499".into(),
            tag_ref: "v2.0.0".into(),
            tag_commit: "f51fb902a4c97ab26e4cff5f52c52c1b72b8708d".into(),
            binary_sha256: "sha256:c929e50f8ef7ddacb552e2ea14261b80a4ae8b36485c4713ab55fd2b6dd62c4d"
                .into(),
            source_lock_sha256:
                "sha256:2b7c5e8faabc241880da70b230bf7d5afe3a249616ffdad74d4b53514ebe69ba".into(),
            generated_at: "2026-08-04T12:00:00Z".into(),
        };
        tuple.validate().expect("valid tuple");
        let tuple_path = tmp.join("release-source-tuple.json");
        tuple.write_to(&tuple_path).expect("write");
        assert_eq!(
            ReleaseSourceTupleV1::read_from(&tuple_path).expect("read"),
            tuple
        );
        let mut bad_tag = tuple.clone();
        bad_tag.tag_commit = bad_tag.evidence_commit.clone();
        assert_eq!(
            bad_tag.validate().unwrap_err(),
            crate::release_source_tuple::ReleaseTupleDeny::TagNotAtSource
        );
        gates.push(pass("RELEASE_TUPLE_GATE"));

        // Master plan §3.4.1 snapshot lease: safe-checkpoint advance only,
        // immediate invalidation under revocation classes.
        {
            use crate::snapshot_lease::{
                InvalidationClass, SnapshotLeaseDeny, SnapshotLeaseV1,
            };
            let mut lease = SnapshotLeaseV1::issue(
                "lease-1",
                "tree-1",
                "sha256:snap-a",
                10,
                20,
                InvalidationClass::NormalAdvance,
            )
            .expect("lease");
            lease.validate().expect("valid");
            assert_eq!(
                lease.advance(15, "sha256:snap-b", 30).unwrap_err(),
                SnapshotLeaseDeny::CheckpointNotReached,
                "advance before the safe checkpoint must deny"
            );
            lease
                .advance(20, "sha256:snap-b", 30)
                .expect("advance at the safe checkpoint");
            let mut revoked =
                SnapshotLeaseV1::issue("l2", "tree-1", "sha256:s", 1, 2, InvalidationClass::NormalAdvance)
                    .expect("lease2");
            revoked
                .invalidate(InvalidationClass::EvidenceInvalidated, "artifact revoked")
                .expect("invalidate");
            assert!(!revoked.is_usable());
            assert_eq!(
                revoked.advance(2, "sha256:s2", 3).unwrap_err(),
                SnapshotLeaseDeny::NotActive,
                "immediate invalidation must block every advance"
            );
            gates.push(pass("SNAPSHOT_LEASE_GATE"));
        }

        // Master plan §3.1.3 claim dependency index: indirect consumers are
        // never missed; unrelated siblings keep running.
        {
            use crate::claim_dependency_index::{
                BlockedState, ClaimDependencyIndex, ConsumerKind, ConsumerNode,
                RevocationDisposition,
            };
            let mut index = ClaimDependencyIndex::new();
            index.record_claim("c1", "snap-1", "manifest-1").expect("c1");
            index.record_claim("c2", "snap-1", "manifest-1").expect("c2");
            index.record_claim("c3", "snap-2", "manifest-2").expect("c3");
            index.record_derived_from("c2", "c1").expect("derived");
            index
                .register_consumer(
                    "c1",
                    ConsumerNode { node_id: "reader".into(), kind: ConsumerKind::ReadOnly, operation_id: None },
                )
                .expect("reader");
            index
                .register_consumer(
                    "c2",
                    ConsumerNode { node_id: "writer".into(), kind: ConsumerKind::Write, operation_id: None },
                )
                .expect("writer"); // indirect via derived_from
            index
                .register_consumer(
                    "c3",
                    ConsumerNode { node_id: "sibling".into(), kind: ConsumerKind::Write, operation_id: None },
                )
                .expect("sibling");
            let analysis = index.analyze_revocation("c1");
            assert_eq!(analysis.len(), 2, "indirect consumers must not be missed");
            let by_node = |node: &str| {
                analysis
                    .iter()
                    .find(|(c, _)| c.node_id == node)
                    .map(|(_, d)| d.clone())
                    .expect("consumer")
            };
            assert_eq!(by_node("reader"), RevocationDisposition::CancelAndRebase);
            assert_eq!(
                by_node("writer"),
                RevocationDisposition::BlockDispatch { state: BlockedState::Frozen }
            );
            assert_eq!(
                index.disposition_for(
                    "c1",
                    &ConsumerNode { node_id: "sibling".into(), kind: ConsumerKind::Write, operation_id: None },
                ),
                RevocationDisposition::Unaffected,
                "unrelated siblings must keep running"
            );
            gates.push(pass("CLAIM_DEPENDENCY_GATE"));
        }

        // Master plan §3.1.3 environment fingerprint + repro levels.
        {
            use crate::environment_fingerprint::{
                EnvironmentFingerprintV1, FingerprintDeny, ReproLevel,
            };
            let same_env = EnvironmentFingerprintV1::build(
                "rust-1.85",
                "sha256:lock",
                "x86_64-apple-darwin",
                "sha256:exe",
                "sha256:env",
                vec!["sha256:input".into()],
                ReproLevel::RecomputedSameEnv,
            )
            .expect("fingerprint");
            same_env.validate().expect("valid");
            assert_eq!(
                same_env.authorize_promotion().unwrap_err(),
                FingerprintDeny::InsufficientReproLevel,
                "same-env reproduction cannot cross task boundaries"
            );
            let third_party = EnvironmentFingerprintV1::build(
                "rust-1.85",
                "sha256:lock",
                "x86_64-apple-darwin",
                "sha256:exe",
                "sha256:env",
                vec!["sha256:input".into()],
                ReproLevel::ThirdPartyReproducible,
            )
            .expect("third party");
            third_party.authorize_long_term_memory().expect("memory ok");
            gates.push(pass("ENV_FINGERPRINT_GATE"));
        }

        // Master plan §3.4.2 checkpoint envelope + obligation.
        {
            use crate::checkpoint_envelope::{
                CheckpointEnvelopeV1, EnvelopeDeny, HostCheckablePredicate, LoopKind,
                ObligationState, ObligationV1,
            };
            let first =
                CheckpointEnvelopeV1::build(LoopKind::Node, "tree-1", "node-1", 1, None, 1)
                    .expect("first");
            first.validate().expect("valid");
            let second =
                CheckpointEnvelopeV1::build(LoopKind::Node, "tree-1", "node-1", 2, Some(1), 1)
                    .expect("second");
            second.validate_append(1, &[1]).expect("append ok");
            assert_eq!(
                CheckpointEnvelopeV1::build(LoopKind::Tree, "tree-1", "op-1", 3, Some(1), 1)
                    .expect("gap")
                    .validate_append(1, &[1])
                    .unwrap_err(),
                EnvelopeDeny::SequenceGap,
                "sequence gaps fail closed"
            );
            let mut obligation = ObligationV1::new(
                "obl-1",
                HostCheckablePredicate::parse("verify:go-test:./...").expect("predicate"),
                None,
                2,
            )
            .expect("obligation");
            obligation.authorize_refinement(1).expect("refine ok");
            obligation
                .transition(ObligationState::Discharged)
                .expect("discharge");
            assert_eq!(
                obligation.transition(ObligationState::Refuted).unwrap_err(),
                EnvelopeDeny::TerminalObligation
            );
            gates.push(pass("CHECKPOINT_ENVELOPE_GATE"));
        }
    }

    let offline_pass_count = gates.iter().filter(|g| g.status == "PASS").count();
    let offline_total = gates.len();

    NextGenContractGateReceipt {
        gates,
        offline_pass_count,
        offline_total,
        product_rc: "NOT_READY".into(),
        note: "offline pure gates only; S8 durable seal + P0-NR-A; FLOW_CONTROL + TOOL_CONTRACT_DISPATCH; A5-A12 exit gates; sandbox/advisor/kairos; exact-SHA CI + formal v2.0.0 tag NOT RUN"
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
        assert!(
            receipt.offline_total >= 30,
            "A3 + A5–A12 + identity/permit/bypass/red-team/release-tuple + snapshot-lease/claim-dep/env-fingerprint/checkpoint-envelope gates must be present"
        );
        assert_eq!(receipt.product_rc, "NOT_READY");
        let json = serde_json::to_string_pretty(&receipt).unwrap();
        assert!(json.contains("M1_GOVERNED_TREE_PREVIEW_GATE"));
        assert!(json.contains("A3_TOKEN_RESERVATION_GATE"));
        assert!(json.contains("A5_CONTEXT_REBUILD_GATE"));
        assert!(json.contains("A12_ROLLBACK_RECEIPT_GATE"));
        assert!(json.contains("IDENTITY_ENVELOPE_GATE"));
        assert!(json.contains("DISPATCH_PERMIT_GATE"));
        assert!(json.contains("ROOT_BYPASS_GATE"));
        assert!(json.contains("SECRET_REF_GATE"));
        assert!(json.contains("CLAIM_STATE_MACHINE_GATE"));
        assert!(json.contains("RUNTIME_PROFILE_GATE"));
        assert!(json.contains("AUDIT_SNAPSHOT_GATE"));
        assert!(json.contains("RELEASE_TUPLE_GATE"));
        assert!(json.contains("SNAPSHOT_LEASE_GATE"));
        assert!(json.contains("CLAIM_DEPENDENCY_GATE"));
        assert!(json.contains("ENV_FINGERPRINT_GATE"));
        assert!(json.contains("CHECKPOINT_ENVELOPE_GATE"));
        assert!(json.contains("NOT_READY"));
        // Durable evidence for implementer SCRATCH / offline suite.
        if let Ok(dir) = std::env::var("LUMEN_EVIDENCE_DIR") {
            let path = std::path::Path::new(&dir).join("offline-contract-gates-full.json");
            let _ = std::fs::create_dir_all(&dir);
            let _ = std::fs::write(&path, &json);
        }
    }
}
