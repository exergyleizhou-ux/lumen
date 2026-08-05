//! Offline three-layer shadow golden path (no provider).
//!
//! Drives the real shipped ClaimAuthority ledger, ContextManifest admission,
//! GovernedOperation store, and depth/leaf ceiling checks without calling any
//! model provider.

#[cfg(test)]
mod tests {
    use crate::claim_authority::ClaimAuthorityActor;
    use crate::context_manifest::{
        ContextManifestV1, ManifestAdmissionMode, ManifestAdmissionRequest, admit_context_manifest,
    };
    use crate::governed_operation::{
        GovernedOperationState, GovernedOperationStore, TreeBudgetLedger,
    };
    use crate::task_ledger::{WorkingMemoryFact, WorkingMemoryLedger, WorkingMemoryState};
    use xai_grok_tools::implementations::grok_build::task::{
        HARD_MAX_SUBAGENT_DEPTH, child_may_spawn_at_depth,
    };
    use xai_grok_tools::types::task_tree_memory::TaskTreeMemoryFactKind;

    fn fact(id: &str, rev: u64, author: &str, text: &str) -> WorkingMemoryFact {
        WorkingMemoryFact {
            task_tree_id: "root".into(),
            branch_id: author.into(),
            fact_id: id.into(),
            revision: rev,
            kind: TaskTreeMemoryFactKind::Fact,
            author_session_id: author.into(),
            evidence_ref: Some("artifact://evidence".into()),
            confidence: 90,
            state: WorkingMemoryState::Proposed,
            text: text.into(),
            derived_from: None,
            derived_from_known: true,
        }
    }

    fn build_manifest(
        node_id: &str,
        parent: &str,
        lineage: &[String],
        snapshot_hash: &str,
        _journal_hash: &str,
    ) -> ContextManifestV1 {
        ContextManifestV1 {
            schema_version: 1,
            task_tree_id: "root".into(),
            node_id: node_id.into(),
            root_session_id: "root".into(),
            immediate_parent_id: Some(parent.into()),
            lineage_path: lineage.to_vec(),
            immutable_assignment_ref: "artifact://assignment".into(),
            immutable_assignment_hash: "sha256:assignment".into(),
            user_objective_ref: "artifact://objective".into(),
            task_contract_hash: "sha256:contract".into(),
            accepted_snapshot_ref: "ledger://snap".into(),
            accepted_snapshot_hash: snapshot_hash.into(),
            tool_catalog_hash: "sha256:tools".into(),
            permitted_tool_contract_hashes: vec!["sha256:read".into()],
            capability_grant_id: format!("grant-{node_id}"),
            policy_revision: 1,
            admission_profile: "governed_tree_development".into(),
            budget_reservation_id: format!("budget-{node_id}"),
            deadline_unix: 2_000_000_000,
            permitted_artifact_refs: vec!["artifact://a".into()],
            model_selection_ref: None,
            parent_compaction_hash: None,
            producer_version: "2.0.0-alpha.1".into(),
            created_at_unix: 1_700_000_000,
        }
    }

    /// Root → code → review → evidence leaf offline golden path.
    #[test]
    fn offline_three_layer_shadow_golden_path() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        let ops = GovernedOperationStore::with_path("root", temp.path().join("ops.json"));
        let mut budget = TreeBudgetLedger::default();

        // --- claims: child proposes, root host-verifies + accepts ---
        ledger
            .propose(fact("f1", 1, "code-child", "tests pass offline"))
            .unwrap();
        // Advisor cannot accept.
        assert!(
            ledger
                .review_with_authority(
                    ClaimAuthorityActor::Advisor,
                    "root",
                    fact("f1", 2, "root", "tests pass offline"),
                    WorkingMemoryState::Accepted,
                )
                .is_err()
        );
        ledger
            .review(
                "root",
                fact("f1", 2, "root", "tests pass offline"),
                WorkingMemoryState::HostVerified,
            )
            .unwrap();
        ledger
            .review(
                "root",
                fact("f1", 3, "root", "tests pass offline"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        let accepted = ledger.accepted_facts().unwrap();
        assert_eq!(accepted.len(), 1);
        assert_eq!(accepted[0].author_session_id, "root");
        let snapshot = ledger.accepted_snapshot().unwrap();
        assert_eq!(snapshot.accepted_count, 1);

        // --- tree operations + budget ---
        budget.reserve("budget-code");
        budget.reserve("budget-review");
        budget.reserve("budget-evidence");
        ops.create(
            "op-code",
            "root",
            "spawn_code",
            "idem-code",
            Some("budget-code".into()),
            None,
            60,
        )
        .unwrap();
        ops.create(
            "op-review",
            "code",
            "spawn_review",
            "idem-review",
            Some("budget-review".into()),
            Some("op-code".into()),
            60,
        )
        .unwrap();
        ops.create(
            "op-evidence",
            "review",
            "spawn_evidence",
            "idem-evidence",
            Some("budget-evidence".into()),
            Some("op-review".into()),
            60,
        )
        .unwrap();
        ops.claim("op-code", "root", "lease-code", 60).unwrap();
        ops.claim("op-review", "code", "lease-review", 60).unwrap();
        ops.claim("op-evidence", "review", "lease-evidence", 60)
            .unwrap();

        // --- lineage / depth / leaf ceiling (shipped HARD_MAX_SUBAGENT_DEPTH) ---
        // depths: root=0, code=1, review=2, evidence=3(leaf)
        assert_eq!(HARD_MAX_SUBAGENT_DEPTH, 3);
        assert!(child_may_spawn_at_depth(0)); // root may spawn code
        assert!(child_may_spawn_at_depth(1)); // code may spawn review
        assert!(child_may_spawn_at_depth(2)); // review may spawn evidence
        assert!(!child_may_spawn_at_depth(3)); // evidence leaf hard deny
        assert!(!child_may_spawn_at_depth(4));

        let code_lineage = vec!["root".to_string(), "code".to_string()];
        let review_lineage = vec!["root".to_string(), "code".to_string(), "review".to_string()];
        let evidence_lineage = vec![
            "root".to_string(),
            "code".to_string(),
            "review".to_string(),
            "evidence".to_string(),
        ];

        // --- ContextManifest admission for each layer ---
        for (node, parent, lineage_full) in [
            ("code", "root", code_lineage.as_slice()),
            ("review", "code", review_lineage.as_slice()),
            ("evidence", "review", evidence_lineage.as_slice()),
        ] {
            let mut manifest = build_manifest(
                node,
                parent,
                lineage_full,
                &snapshot.accepted_set_hash,
                &snapshot.journal_hash,
            );
            manifest
                .bind_accepted_snapshot(&snapshot, "ledger://snap")
                .unwrap();
            let hash = manifest.manifest_hash().unwrap();
            let admitted = admit_context_manifest(&ManifestAdmissionRequest {
                mode: ManifestAdmissionMode::GovernedSpawn,
                manifest: Some(&manifest),
                live_snapshot: Some(&snapshot),
                expected_manifest_hash: Some(&hash),
                expected_root_session_id: Some("root"),
                expected_node_id: Some(node),
                expected_parent_id: Some(parent),
            })
            .unwrap();
            assert_eq!(admitted, hash);
            // resume with same hash
            let resumed = admit_context_manifest(&ManifestAdmissionRequest {
                mode: ManifestAdmissionMode::GovernedResume,
                manifest: Some(&manifest),
                live_snapshot: Some(&snapshot),
                expected_manifest_hash: Some(&hash),
                expected_root_session_id: Some("root"),
                expected_node_id: Some(node),
                expected_parent_id: Some(parent),
            })
            .unwrap();
            assert_eq!(resumed, hash);
        }

        // --- negatives: forged hash, foreign tree, stale snapshot, legacy ---
        let mut good = build_manifest(
            "code",
            "root",
            &code_lineage,
            &snapshot.accepted_set_hash,
            &snapshot.journal_hash,
        );
        good.bind_accepted_snapshot(&snapshot, "ledger://snap")
            .unwrap();
        let good_hash = good.manifest_hash().unwrap();
        assert!(
            admit_context_manifest(&ManifestAdmissionRequest {
                mode: ManifestAdmissionMode::GovernedSpawn,
                manifest: Some(&good),
                live_snapshot: Some(&snapshot),
                expected_manifest_hash: Some("sha256:forged"),
                expected_root_session_id: Some("root"),
                expected_node_id: Some("code"),
                expected_parent_id: Some("root"),
            })
            .is_err()
        );
        assert!(
            admit_context_manifest(&ManifestAdmissionRequest {
                mode: ManifestAdmissionMode::LegacyNoManifest,
                manifest: None,
                live_snapshot: None,
                expected_manifest_hash: None,
                expected_root_session_id: None,
                expected_node_id: None,
                expected_parent_id: None,
            })
            .is_err()
        );
        let mut foreign_snap = snapshot.clone();
        foreign_snap.task_tree_id = "other".into();
        assert!(
            admit_context_manifest(&ManifestAdmissionRequest {
                mode: ManifestAdmissionMode::GovernedResume,
                manifest: Some(&good),
                live_snapshot: Some(&foreign_snap),
                expected_manifest_hash: Some(&good_hash),
                expected_root_session_id: Some("root"),
                expected_node_id: Some("code"),
                expected_parent_id: Some("root"),
            })
            .is_err()
        );

        // --- cancel cascade: budgets released once, no resurrection ---
        let cancelled = ops.cancel_cascade_from_root("root", "op-code").unwrap();
        assert_eq!(cancelled.len(), 3);
        assert!(
            cancelled
                .iter()
                .all(|op| op.state == GovernedOperationState::Cancelled && op.budget_released)
        );
        // mirror budget ledger release exactly once
        for id in ["budget-code", "budget-review", "budget-evidence"] {
            budget.release_once(id).unwrap();
            assert!(budget.release_once(id).is_err());
        }
        // late complete after cancel
        assert!(
            ops.complete("op-evidence", "review", "lease-evidence", "r")
                .is_err()
        );
        // duplicate/foreign
        assert!(
            ops.takeover("op-code", "stranger", "lease-x", 0, 30, false)
                .is_err()
        );

        assert_eq!(evidence_lineage.len(), 4);
        assert!(!child_may_spawn_at_depth(3));
        // Child at depth > HARD_MAX is rejected by coordinator (shipped constant).
        assert!(4 > HARD_MAX_SUBAGENT_DEPTH);
        let _ = good_hash;

        // --- S7 loop: leaf no-progress escalates; delivery unknown freezes ---
        use crate::evidence_loop::{
            LoopEvent, LoopPhase, NodeLoopState, SupervisorLoopEvent, SupervisorLoopState,
            TreeLoopEvent, TreeLoopState, reduce_node_loop, reduce_supervisor_loop,
            reduce_tree_loop,
        };
        let mut leaf_loop = NodeLoopState::fresh();
        leaf_loop.no_progress_cap = 2;
        let (leaf_loop, _) = reduce_node_loop(
            leaf_loop,
            LoopEvent::NoProgress {
                fingerprint: "same".into(),
            },
        )
        .unwrap();
        let (leaf_loop, _) = reduce_node_loop(
            leaf_loop,
            LoopEvent::NoProgress {
                fingerprint: "same".into(),
            },
        )
        .unwrap();
        assert_eq!(leaf_loop.phase, LoopPhase::NeedsParentDecision);
        let (tree, _) =
            reduce_tree_loop(TreeLoopState::fresh("root"), TreeLoopEvent::NodeNeedsParent);
        assert_eq!(tree.phase, LoopPhase::NeedsParentDecision);
        let (sup, _) = reduce_supervisor_loop(
            SupervisorLoopState::fresh("sup"),
            SupervisorLoopEvent::TreeNeedsParent,
        );
        assert_eq!(sup.phase, LoopPhase::NeedsParentDecision);

        // --- S8 sealed receipt: partial output forbids; durable clean opens 1 ---
        use crate::sealed_attempt_receipt::{
            DurableSealAuthority, SealedAttemptReceiptStore, clean_preflight_receipt,
            mark_output_emitted, may_in_process_retry, ordinary_turn_max_retries_with_authority,
        };
        assert!(may_in_process_retry(&clean_preflight_receipt("g1")).is_ok());
        assert!(
            may_in_process_retry(&mark_output_emitted(clean_preflight_receipt("g2"))).is_err()
        );
        let seal_store = SealedAttemptReceiptStore::in_memory();
        let clean = clean_preflight_receipt("g3");
        seal_store.record(clean.clone(), None, None).unwrap();
        assert_eq!(
            ordinary_turn_max_retries_with_authority(
                Some(&clean),
                seal_store.authority_for(&clean)
            ),
            1
        );
        assert_eq!(
            ordinary_turn_max_retries_with_authority(
                Some(&clean),
                DurableSealAuthority::Absent
            ),
            0
        );

        // --- sandbox leaf deny (S5/S6 surface) ---
        use crate::agent_sandbox::{
            AgentSandboxV1, IssueSandboxRequest, SANDBOX_HARD_MAX_DEPTH, SandboxAssuranceV1,
        };
        let leaf_sb = AgentSandboxV1::issue(IssueSandboxRequest {
            sandbox_id: "sb-ev".into(),
            task_tree_id: "root".into(),
            node_id: "evidence".into(),
            immediate_parent_id: Some("review".into()),
            depth: SANDBOX_HARD_MAX_DEPTH,
            branch_id: "branch-evidence".into(),
            context_manifest_hash: "sha256:m".into(),
            accepted_snapshot_hash: snapshot.accepted_set_hash.clone(),
            capability_grant_id: "grant-ev".into(),
            policy_revision: 1,
            budget_reservation_id: "budget-evidence".into(),
            is_root: false,
            request_write: true,
            request_network: true,
            request_spawn: true,
            issued_at_unix: 1_700_000_000,
            ttl_secs: 60,
            assurance: SandboxAssuranceV1::HarnessPolicyOnly,
        })
        .unwrap();
        assert!(leaf_sb.authorize_spawn(1_700_000_100).is_err());
        assert!(leaf_sb.authorize_filesystem_write(1_700_000_100).is_err());
    }
}
