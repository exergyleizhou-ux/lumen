//! NG-03C-4 — compose integration of the durable pieces that must agree
//! before coordinator wiring:
//!
//! 1. [`BudgetLedger`](xai_grok_tools::implementations::grok_build::task::budget::BudgetLedger)
//!    — atomic check-and-reserve structural + token ceilings
//! 2. [`GovernedOperationStore`] — durable operation identity, lease, cancel,
//!    and outbox records in the same atomic snapshot
//! 3. [`LifecycleJournal`] — append-only authority events (K2 read model)
//! 4. [`project_authority_trail`] — coordinator `TreeAuthorityLog` → journal
//!    schema bridge (no tools→memory crate cycle)
//!
//! Plus [`crash_action_for`] for the recovery decision that must never invent
//! a replay after output/effect uncertainty (P0-NR-A / K4).
//!
//! This is intentionally *not* a coordinator rewrite: the live coordinator
//! already enforces structural ceilings. The contract proven here is the
//! composition order and fail-closed edges those stores must share when
//! stitched onto spawn/complete paths.

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use crate::authority_projection::{
        project_authority_trail, AuthorityProjectionContext, AuthorityProjectionError,
    };
    use crate::effect_recovery::{
        crash_action_for, CrashSafeAction, EffectRecoveryClass, ExternalEffectObservation,
        OutputObservation,
    };
    use crate::governed_operation::{GovernedOperationState, GovernedOperationStore};
    use crate::lifecycle_journal::{
        DerivedOperationState, GovernedLifecycleEventKind, GovernedLifecycleEventSource,
        GovernedLifecycleEventV1, JournalError, LifecycleJournal,
    };
    use xai_grok_tools::implementations::grok_build::task::authority_log::{
        AuthorityEventKind, TreeAuthorityLog,
    };
    use xai_grok_tools::implementations::grok_build::task::budget::{
        BudgetDenial, BudgetLedger, ReleaseOutcome, TreeBudgetV1, UsageSettlement,
    };

    const TREE: &str = "compose-tree";
    const ROOT: &str = "root";
    const CHILD: &str = "child-a";

    fn tight_ledger() -> BudgetLedger {
        BudgetLedger::new(TreeBudgetV1 {
            max_depth: 2,
            max_children_per_node: 1,
            max_live_nodes: 2,
            max_background_nodes: 1,
            token_reservation_limit: Some(500),
            tool_call_limit: Some(4),
            wall_time_limit: Duration::from_secs(3600),
            daily_cost_limit: None,
            artifact_byte_limit: None,
        })
    }

    fn lifecycle_event(
        journal: &LifecycleJournal,
        sequence: u64,
        kind: GovernedLifecycleEventKind,
        parent: Option<u64>,
        node_id: &str,
        lease_id: Option<&str>,
        evidence: &[&str],
    ) -> GovernedLifecycleEventV1 {
        let mut event = GovernedLifecycleEventV1 {
            event_id: format!("evt-{sequence}"),
            task_tree_id: TREE.to_owned(),
            node_id: node_id.to_owned(),
            owner_session_id: ROOT.to_owned(),
            sequence,
            causal_parent: parent,
            kind,
            source: GovernedLifecycleEventSource::Actor,
            lease_id: lease_id.map(str::to_owned),
            contract_hash: Some("sha256:contract".to_owned()),
            policy_revision: 1,
            evidence_refs: evidence.iter().map(|s| (*s).to_owned()).collect(),
            occurred_at: 1_700_000_000 + sequence,
            payload_hash: String::new(),
        };
        event.payload_hash = event.compute_payload_hash().unwrap();
        let _ = journal;
        event
    }

    /// Happy path: reserve → create → journal progress → claim → complete →
    /// settle → release → terminal journal. Then prove ceilings and
    /// no-revival edges still fail closed.
    #[test]
    fn budget_store_journal_compose_happy_path_and_fail_closed() {
        let temp = tempfile::tempdir().unwrap();
        let mut budget = tight_ledger();
        let ops = GovernedOperationStore::with_path(TREE, temp.path().join("ops.json"));
        let mut journal = LifecycleJournal::at_path(TREE, temp.path().join("events.jsonl"));

        // --- 1. atomic reserve (structural + token) ---
        let res = budget
            .reserve_spawn(CHILD, Some(ROOT), 1, false, 100, 1)
            .expect("first child reserve");
        let res_key = format!("res-{}", res.0);
        assert_eq!(budget.live_node_count(), 1);

        // Structural: children_per_node=1 → second sibling denied before create.
        assert_eq!(
            budget.reserve_spawn("child-b", Some(ROOT), 1, false, 0, 0),
            Err(BudgetDenial::ChildrenPerNodeExceeded {
                node: ROOT.into(),
                max: 1
            })
        );

        // --- 2. durable operation identity bound to the reservation ---
        let created = ops
            .create(
                "op-child",
                CHILD,
                "spawn_child",
                "idem-child",
                Some(res_key.clone()),
                None,
                60,
            )
            .unwrap();
        assert_eq!(created.state, GovernedOperationState::Created);
        assert_eq!(created.reservation_id.as_deref(), Some(res_key.as_str()));

        // Idempotency: same key returns the same op, no double-create.
        let again = ops
            .create(
                "op-child-dup",
                CHILD,
                "spawn_child",
                "idem-child",
                Some(res_key.clone()),
                None,
                60,
            )
            .unwrap();
        assert_eq!(again.operation_id, created.operation_id);

        // --- 3. authority journal (K2): booting → ready → running ---
        journal
            .append(lifecycle_event(
                &journal,
                0,
                GovernedLifecycleEventKind::Booting,
                None,
                CHILD,
                None,
                &[],
            ))
            .unwrap();
        journal
            .append(lifecycle_event(
                &journal,
                1,
                GovernedLifecycleEventKind::Ready,
                Some(0),
                CHILD,
                None,
                &[&format!("op:{}", created.operation_id)],
            ))
            .unwrap();
        journal
            .append(lifecycle_event(
                &journal,
                2,
                GovernedLifecycleEventKind::PromptAccepted,
                Some(1),
                CHILD,
                None,
                &[&format!("op:{}", created.operation_id)],
            ))
            .unwrap();

        let claimed = ops.claim("op-child", CHILD, "lease-1", 60).unwrap();
        assert_eq!(claimed.state, GovernedOperationState::Claimed);
        assert_eq!(claimed.lease_id.as_deref(), Some("lease-1"));

        journal
            .append(lifecycle_event(
                &journal,
                3,
                GovernedLifecycleEventKind::Running,
                Some(2),
                CHILD,
                Some("lease-1"),
                &[&format!("op:{}", created.operation_id)],
            ))
            .unwrap();
        assert_eq!(
            journal.derived_state(),
            DerivedOperationState {
                current: Some(GovernedLifecycleEventKind::Running),
                last_sequence: 3,
                terminal: false,
            }
        );

        // --- 4. complete + settle + release (exactly-once settle, idempotent release) ---
        let done = ops
            .complete("op-child", CHILD, "lease-1", "receipt://ok")
            .unwrap();
        assert_eq!(done.state, GovernedOperationState::Completed);
        assert!(done.budget_released, "store releases reservation flag once");

        assert_eq!(
            budget.settle_usage(res, Some(80), Some(1)),
            UsageSettlement::Applied
        );
        assert_eq!(budget.settled_tokens(), 80);
        assert_eq!(
            budget.settle_usage(res, Some(80), Some(1)),
            UsageSettlement::AlreadySettled,
            "double settle must not double-count"
        );
        assert_eq!(budget.settled_tokens(), 80);

        // Release after settle is idempotent accounting (AlreadyReleased).
        assert_eq!(budget.release(res), ReleaseOutcome::AlreadyReleased);
        assert_eq!(budget.live_node_count(), 0);

        journal
            .append(lifecycle_event(
                &journal,
                4,
                GovernedLifecycleEventKind::TerminalSucceeded,
                Some(3),
                CHILD,
                Some("lease-1"),
                &["receipt://ok"],
            ))
            .unwrap();
        let terminal = journal.derived_state();
        assert!(terminal.terminal);
        assert_eq!(
            terminal.current,
            Some(GovernedLifecycleEventKind::TerminalSucceeded)
        );

        // --- 5. fail-closed edges (compose-level, not unit-level alone) ---
        // Late complete after terminal store state.
        assert!(
            ops.complete("op-child", CHILD, "lease-1", "receipt://late")
                .is_err()
        );
        // Journal no-revival after terminal.
        assert_eq!(
            journal.append(lifecycle_event(
                &journal,
                5,
                GovernedLifecycleEventKind::Running,
                Some(4),
                CHILD,
                None,
                &[],
            )),
            Err(JournalError::LateEventAfterTerminal {
                kind: GovernedLifecycleEventKind::Running
            })
        );
        // Late settle after full release path already settled → AlreadySettled /
        // not a debit; a never-reserved id is NotFound.
        assert_eq!(
            budget.settle_usage(
                xai_grok_tools::implementations::grok_build::task::budget::ReservationId(999),
                Some(1),
                Some(1)
            ),
            UsageSettlement::NotFound
        );

        // Disk reload of journal + store must agree with in-memory truth.
        drop(journal);
        let reloaded = LifecycleJournal::at_path(TREE, temp.path().join("events.jsonl"));
        assert_eq!(reloaded.derived_state(), terminal);
        let reopened = GovernedOperationStore::with_path(TREE, temp.path().join("ops.json"));
        let op = reopened.get("op-child").expect("op reloads");
        assert_eq!(op.state, GovernedOperationState::Completed);
        assert!(op.budget_released);
    }

    /// Cancel cascade: store marks cancelled + budget_released; BudgetLedger
    /// release is exactly-once; journal records Cancelled and refuses revival.
    #[test]
    fn cancel_cascade_composes_with_budget_and_journal() {
        let temp = tempfile::tempdir().unwrap();
        let mut budget = tight_ledger();
        let ops = GovernedOperationStore::with_path(TREE, temp.path().join("ops-cancel.json"));
        let mut journal = LifecycleJournal::in_memory(TREE);

        let res_parent = budget
            .reserve_spawn("parent", Some(ROOT), 1, false, 50, 1)
            .unwrap();
        // Parent occupies the only children_per_node slot under root; child
        // uses a different parent edge so the structural limit does not block.
        let res_child = budget
            .reserve_spawn("leaf", Some("parent"), 2, false, 50, 1)
            .unwrap();

        ops.create(
            "op-parent",
            "parent",
            "spawn_parent",
            "idem-parent",
            Some(format!("res-{}", res_parent.0)),
            None,
            60,
        )
        .unwrap();
        ops.create(
            "op-leaf",
            "leaf",
            "spawn_leaf",
            "idem-leaf",
            Some(format!("res-{}", res_child.0)),
            Some("op-parent".into()),
            60,
        )
        .unwrap();
        ops.claim("op-parent", "parent", "lease-p", 60).unwrap();
        ops.claim("op-leaf", "leaf", "lease-l", 60).unwrap();

        journal
            .append(lifecycle_event(
                &journal,
                0,
                GovernedLifecycleEventKind::Running,
                None,
                "parent",
                Some("lease-p"),
                &["op:op-parent"],
            ))
            .unwrap();

        let cancelled = ops.cancel_cascade_from_root(TREE, "op-parent").unwrap();
        assert_eq!(cancelled.len(), 2);
        assert!(cancelled.iter().all(|op| {
            op.state == GovernedOperationState::Cancelled && op.budget_released
        }));

        // Budget: release each reservation once; second call is AlreadyReleased.
        assert_eq!(budget.release(res_parent), ReleaseOutcome::Released);
        assert_eq!(budget.release(res_parent), ReleaseOutcome::AlreadyReleased);
        assert_eq!(budget.release(res_child), ReleaseOutcome::Released);
        assert_eq!(budget.live_node_count(), 0);

        // Late complete after cancel is rejected by the store.
        assert!(
            ops.complete("op-leaf", "leaf", "lease-l", "receipt://zombie")
                .is_err()
        );

        journal
            .append(lifecycle_event(
                &journal,
                1,
                GovernedLifecycleEventKind::Cancelled,
                Some(0),
                "parent",
                None,
                &["op:op-parent", "op:op-leaf"],
            ))
            .unwrap();
        assert!(journal.derived_state().terminal);
        assert_eq!(
            journal.append(lifecycle_event(
                &journal,
                2,
                GovernedLifecycleEventKind::Running,
                Some(1),
                "parent",
                None,
                &[],
            )),
            Err(JournalError::LateEventAfterTerminal {
                kind: GovernedLifecycleEventKind::Running
            })
        );

        // Live-node ceiling: after release, new work may reserve again.
        let again = budget
            .reserve_spawn("parent-2", Some(ROOT), 1, false, 10, 0)
            .unwrap();
        assert_eq!(budget.release(again), ReleaseOutcome::Released);
    }

    /// Coordinator authority log + op store outbox + lifecycle projection
    /// share one compose path: reserve/claim/complete enqueue outbox, and
    /// the slim authority trail projects into NG-00 journal kinds.
    #[test]
    fn authority_log_outbox_and_lifecycle_projection_compose() {
        let temp = tempfile::tempdir().unwrap();
        let mut budget = tight_ledger();
        let ops = GovernedOperationStore::with_path(TREE, temp.path().join("ops-auth.json"));
        let mut authority = TreeAuthorityLog::at_path(temp.path().join("authority.jsonl"));
        let mut journal = LifecycleJournal::at_path(TREE, temp.path().join("lifecycle.jsonl"));

        let res = budget
            .reserve_spawn(CHILD, Some(ROOT), 1, false, 40, 1)
            .unwrap();
        let res_key = format!("ledger:{}", res.0);

        authority
            .append(CHILD, "op-auth", AuthorityEventKind::SpawnReserved, Some(res_key.clone()))
            .unwrap();
        let created = ops
            .create(
                "op-auth",
                CHILD,
                "spawn_child",
                "idem-auth",
                Some(res_key.clone()),
                None,
                60,
            )
            .unwrap();
        assert!(
            ops.list_outbox()
                .iter()
                .any(|r| r.transition == "created" && r.operation_id == "op-auth"),
            "create must enqueue outbox atomically"
        );

        authority
            .append(CHILD, "op-auth", AuthorityEventKind::SpawnClaimed, Some(res_key.clone()))
            .unwrap();
        let claimed = ops.claim("op-auth", CHILD, "lease-auth", 60).unwrap();
        assert_eq!(claimed.state, GovernedOperationState::Claimed);

        let done = ops
            .complete("op-auth", CHILD, "lease-auth", "receipt://auth-ok")
            .unwrap();
        authority
            .append(CHILD, "op-auth", AuthorityEventKind::TerminalSucceeded, None)
            .unwrap();
        assert!(done.budget_released);
        let completed_outbox = ops
            .list_outbox()
            .into_iter()
            .find(|r| r.transition == "completed")
            .expect("complete must enqueue outbox");
        assert_eq!(
            completed_outbox.payload_ref.as_deref(),
            Some("receipt://auth-ok")
        );
        assert_eq!(
            completed_outbox.delivery,
            xai_grok_tools::implementations::grok_build::task::OutboxDeliveryState::Undelivered
        );

        let ctx = AuthorityProjectionContext {
            task_tree_id: TREE.into(),
            owner_session_id: ROOT.into(),
            policy_revision: 1,
        };
        let written =
            project_authority_trail(&mut journal, &ctx, authority.events(), 1_700_000_000)
                .unwrap();
        assert_eq!(written, 3);
        assert_eq!(
            journal.events()[0].kind,
            GovernedLifecycleEventKind::Ready
        );
        assert_eq!(
            journal.events()[1].kind,
            GovernedLifecycleEventKind::Running
        );
        assert_eq!(
            journal.events()[2].kind,
            GovernedLifecycleEventKind::TerminalSucceeded
        );
        assert!(journal.derived_state().terminal);

        // Disk: ops snapshot carries outbox; authority + lifecycle reload.
        drop(journal);
        drop(authority);
        let reopened = GovernedOperationStore::with_path(TREE, temp.path().join("ops-auth.json"));
        assert_eq!(
            reopened.get("op-auth").unwrap().state,
            GovernedOperationState::Completed
        );
        assert!(
            reopened
                .list_outbox()
                .iter()
                .any(|r| r.transition == "completed")
        );
        let reloaded_auth = TreeAuthorityLog::at_path(temp.path().join("authority.jsonl"));
        assert!(reloaded_auth.is_operation_terminal("op-auth"));
        let reloaded_life = LifecycleJournal::at_path(TREE, temp.path().join("lifecycle.jsonl"));
        assert!(reloaded_life.derived_state().terminal);

        // Budget settle after projection still exactly-once.
        assert_eq!(
            budget.settle_usage(res, Some(40), Some(1)),
            UsageSettlement::Applied
        );
        assert_eq!(budget.release(res), ReleaseOutcome::AlreadyReleased);

        // Projecting again after terminal fails closed.
        let mut journal2 = LifecycleJournal::at_path(TREE, temp.path().join("lifecycle.jsonl"));
        assert!(matches!(
            project_authority_trail(
                &mut journal2,
                &ctx,
                reloaded_auth.events(),
                1_700_000_500
            ),
            Err(AuthorityProjectionError::Journal(_))
        ));
        let _ = created;
    }

    /// P0-NR-A / K4: crash recovery is derived, never a free replay.
    #[test]
    fn crash_recovery_composes_with_opaque_and_output_rules() {
        // Opaque + no observations → Frozen (never unattended replay).
        assert_eq!(
            crash_action_for(
                &EffectRecoveryClass::Opaque,
                OutputObservation::None,
                ExternalEffectObservation::None,
            ),
            CrashSafeAction::Frozen
        );
        // Pure + clean → Rerun; any output observation freezes.
        assert_eq!(
            crash_action_for(
                &EffectRecoveryClass::Pure,
                OutputObservation::None,
                ExternalEffectObservation::None,
            ),
            CrashSafeAction::Rerun
        );
        assert_eq!(
            crash_action_for(
                &EffectRecoveryClass::Pure,
                OutputObservation::Emitted,
                ExternalEffectObservation::None,
            ),
            CrashSafeAction::Frozen
        );
        // Idempotent may rerun only with the same key when effect is unknown.
        assert_eq!(
            crash_action_for(
                &EffectRecoveryClass::Idempotent {
                    key: "idem-1".into()
                },
                OutputObservation::None,
                ExternalEffectObservation::Unknown,
            ),
            CrashSafeAction::RerunWithKey {
                key: "idem-1".into()
            }
        );
    }
}
