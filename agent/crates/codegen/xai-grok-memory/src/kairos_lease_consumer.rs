//! S12 / NG-08 — Kairos lease-consumer loop (pure, offline-safe).
//!
//! The daemon/lease-consumer is proven here as a *pure step function* over
//! operation views: claim → heartbeat → reconcile (crash recovery) →
//! complete/fail, with fail-closed edges for duplicate consumers, stale
//! leases, unknown external effects and outbox replays. The shell/product
//! path drives this loop with a fake clock in fixtures; no real daemon, no
//! 24h soak, no side effects (plan §NG-08).

use serde::{Deserialize, Serialize};

use crate::evidence_loop::LoopPhase;
use crate::operator_control::OperationView;

/// One decision of the consumer loop for a single operation.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ConsumerStep {
    /// Take the lease (or renew) with a new epoch.
    Claim { op_id: String, new_lease_epoch: u64 },
    /// Renew the lease without changing the holder.
    Heartbeat { op_id: String, new_lease_epoch: u64 },
    /// Lease expired → reconcile: former holder + result + new epoch.
    Reconcile {
        op_id: String,
        former_holder: String,
        new_holder: String,
        new_lease_epoch: u64,
    },
    /// Operation completed; release the lease.
    Complete { op_id: String },
    /// Operation failed terminally; release the lease.
    Fail { op_id: String },
    /// Unknown external effect: freeze and wait for a human — never replay.
    Freeze { op_id: String },
    /// Nothing to do this tick.
    Idle,
}

/// Pure policy inputs for the consumer step decision.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ConsumerPolicy {
    /// Lease duration in ms; a lease older than this is expired.
    pub lease_ttl_epoch_ms: u64,
    /// Heartbeat interval in ms.
    pub heartbeat_interval_epoch_ms: u64,
    /// Max heartbeats without completion before the consumer re-checks.
    pub max_heartbeats: u64,
}

impl Default for ConsumerPolicy {
    fn default() -> Self {
        Self {
            lease_ttl_epoch_ms: 30_000,
            heartbeat_interval_epoch_ms: 5_000,
            max_heartbeats: 6,
        }
    }
}

/// Who owns this consumer's view of an operation.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ConsumerOperation {
    pub view: OperationView,
    pub last_touched_epoch_ms: u64,
    pub heartbeats: u64,
    /// The consumer id (holder) — must match `view.owner` for continue.
    pub consumer_id: String,
    /// Outbox replay guard: set of already-delivered event ids (idempotency).
    pub delivered_events: Vec<String>,
}

impl ConsumerOperation {
    /// Decide the next step for this operation at `now`.
    pub fn next_step(&self, policy: &ConsumerPolicy, now_epoch_ms: u64) -> ConsumerStep {
        // Terminal states: release and stop supervising.
        if matches!(
            self.view.phase,
            LoopPhase::TerminalSucceeded | LoopPhase::TerminalFailed
        ) {
            return match self.view.phase {
                LoopPhase::TerminalSucceeded => ConsumerStep::Complete {
                    op_id: self.view.op_id.clone(),
                },
                _ => ConsumerStep::Fail {
                    op_id: self.view.op_id.clone(),
                },
            };
        }
        if matches!(self.view.phase, LoopPhase::Cancelled) {
            return ConsumerStep::Complete {
                op_id: self.view.op_id.clone(),
            };
        }
        // Frozen: only a human (operator plane) may resume — the consumer
        // never auto-resumes.
        if matches!(
            self.view.phase,
            LoopPhase::Frozen | LoopPhase::NeedsParentDecision
        ) {
            return ConsumerStep::Freeze {
                op_id: self.view.op_id.clone(),
            };
        }
        // Unknown external effect: never replay, never resume.
        if self.view.external_effect_unknown {
            return ConsumerStep::Freeze {
                op_id: self.view.op_id.clone(),
            };
        }
        let Some(owner) = &self.view.owner else {
            // No lease yet → claim.
            return ConsumerStep::Claim {
                op_id: self.view.op_id.clone(),
                new_lease_epoch: self.view.lease_epoch.saturating_add(1),
            };
        };
        if owner != &self.consumer_id {
            // Foreign lease: if expired, reconcile (take over); otherwise idle
            // (never dual-own).
            let lease_age = now_epoch_ms.saturating_sub(self.last_touched_epoch_ms);
            if lease_age >= policy.lease_ttl_epoch_ms {
                return ConsumerStep::Reconcile {
                    op_id: self.view.op_id.clone(),
                    former_holder: owner.clone(),
                    new_holder: self.consumer_id.clone(),
                    new_lease_epoch: self.view.lease_epoch.saturating_add(1),
                };
            }
            return ConsumerStep::Idle;
        }
        // Our own lease.
        if self.heartbeats >= policy.max_heartbeats {
            // Heartbeat budget exhausted → re-check convergence (claim again).
            return ConsumerStep::Claim {
                op_id: self.view.op_id.clone(),
                new_lease_epoch: self.view.lease_epoch.saturating_add(1),
            };
        }
        let age = now_epoch_ms.saturating_sub(self.last_touched_epoch_ms);
        if age >= policy.heartbeat_interval_epoch_ms {
            return ConsumerStep::Heartbeat {
                op_id: self.view.op_id.clone(),
                new_lease_epoch: self.view.lease_epoch,
            };
        }
        ConsumerStep::Idle
    }
}

/// Outbox idempotency: a delivered event id is dropped on replay (never
/// double-applied).
pub fn outbox_should_deliver(delivered_events: &[String], event_id: &str) -> bool {
    !delivered_events.iter().any(|d| d == event_id)
}

/// Crash recovery: an operation whose lease is expired and whose owner is
/// gone (or unreachable) can only be reconciled by a consumer that proves
/// the lease age — this pure check gates the reconcile path.
pub fn lease_is_expired(
    last_touched_epoch_ms: u64,
    now_epoch_ms: u64,
    lease_ttl_epoch_ms: u64,
) -> bool {
    now_epoch_ms.saturating_sub(last_touched_epoch_ms) >= lease_ttl_epoch_ms
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::operator_control::OperationView;

    fn view(op_id: &str, phase: LoopPhase, owner: Option<&str>) -> OperationView {
        OperationView {
            op_id: op_id.to_string(),
            owner: owner.map(str::to_string),
            lease_epoch: 1,
            phase,
            attempt_observed: false,
            external_effect_unknown: false,
            manifest_hash: "m".into(),
            evidence_hash: "e".into(),
            budget_hash: "b".into(),
        }
    }

    fn consumer(op_id: &str, view: OperationView) -> ConsumerOperation {
        ConsumerOperation {
            view,
            last_touched_epoch_ms: 0,
            heartbeats: 0,
            consumer_id: "consumer-1".to_string(),
            delivered_events: Vec::new(),
        }
    }

    #[test]
    fn claims_when_unowned_and_heartbeats_when_owned() {
        let policy = ConsumerPolicy::default();
        let c = consumer("op1", view("op1", LoopPhase::Running, None));
        assert_eq!(
            c.next_step(&policy, 1000),
            ConsumerStep::Claim {
                op_id: "op1".into(),
                new_lease_epoch: 2
            }
        );

        let owned = consumer("op1", view("op1", LoopPhase::Running, Some("consumer-1")));
        let c2 = ConsumerOperation {
            last_touched_epoch_ms: 0,
            heartbeats: 1,
            ..owned
        };
        assert_eq!(
            c2.next_step(&policy, 5_001),
            ConsumerStep::Heartbeat {
                op_id: "op1".into(),
                new_lease_epoch: 1
            }
        );
    }

    #[test]
    fn foreign_lease_idles_until_expired_then_reconciles() {
        let policy = ConsumerPolicy::default();
        let foreign = consumer("op2", view("op2", LoopPhase::Running, Some("other-consumer")));
        // Not expired yet → idle, never dual-own.
        let c = ConsumerOperation {
            last_touched_epoch_ms: 1_000,
            ..foreign.clone()
        };
        assert_eq!(c.next_step(&policy, 5_000), ConsumerStep::Idle);
        // Expired → reconcile with former holder recorded.
        let c2 = ConsumerOperation {
            last_touched_epoch_ms: 1_000,
            ..foreign
        };
        assert_eq!(
            c2.next_step(&policy, 100_000),
            ConsumerStep::Reconcile {
                op_id: "op2".into(),
                former_holder: "other-consumer".into(),
                new_holder: "consumer-1".into(),
                new_lease_epoch: 2
            }
        );
    }

    #[test]
    fn terminal_and_cancelled_release_cleanly() {
        let policy = ConsumerPolicy::default();
        let done = consumer("op3", view("op3", LoopPhase::TerminalSucceeded, Some("consumer-1")));
        assert_eq!(
            done.next_step(&policy, 1000),
            ConsumerStep::Complete {
                op_id: "op3".into()
            }
        );
        let failed = consumer("op4", view("op4", LoopPhase::TerminalFailed, Some("consumer-1")));
        assert_eq!(
            failed.next_step(&policy, 1000),
            ConsumerStep::Fail {
                op_id: "op4".into()
            }
        );
        let cancelled = consumer("op5", view("op5", LoopPhase::Cancelled, Some("consumer-1")));
        assert_eq!(
            cancelled.next_step(&policy, 1000),
            ConsumerStep::Complete {
                op_id: "op5".into()
            }
        );
    }

    #[test]
    fn frozen_and_unknown_effect_never_auto_resume() {
        let policy = ConsumerPolicy::default();
        let frozen = consumer("op6", view("op6", LoopPhase::Frozen, Some("consumer-1")));
        assert_eq!(
            frozen.next_step(&policy, 1000),
            ConsumerStep::Freeze {
                op_id: "op6".into()
            }
        );
        let mut u = view("op7", LoopPhase::Running, Some("consumer-1"));
        u.external_effect_unknown = true;
        let uncertain = consumer("op7", u);
        assert_eq!(
            uncertain.next_step(&policy, 1000),
            ConsumerStep::Freeze {
                op_id: "op7".into()
            }
        );
    }

    #[test]
    fn outbox_replay_is_idempotent() {
        assert!(outbox_should_deliver(&[], "evt-1"));
        assert!(outbox_should_deliver(&["evt-1".to_string()], "evt-2"));
        assert!(!outbox_should_deliver(&["evt-1".to_string()], "evt-1"));
    }

    #[test]
    fn lease_expiry_is_pure_and_boundary_inclusive() {
        assert!(lease_is_expired(0, 30_000, 30_000));
        assert!(lease_is_expired(0, 30_001, 30_000));
        assert!(!lease_is_expired(0, 29_999, 30_000));
    }

    #[test]
    fn heartbeat_budget_exhaustion_reclaims() {
        let policy = ConsumerPolicy::default();
        let owned = consumer("op8", view("op8", LoopPhase::Running, Some("consumer-1")));
        let c = ConsumerOperation {
            heartbeats: policy.max_heartbeats,
            last_touched_epoch_ms: 0,
            ..owned
        };
        assert_eq!(
            c.next_step(&policy, 100_000),
            ConsumerStep::Claim {
                op_id: "op8".into(),
                new_lease_epoch: 2
            }
        );
    }

    #[test]
    fn crash_recovery_never_replays_unknown_effect() {
        // A crashed consumer's op with unknown external effect: reconcile is
        // forbidden (fail-closed) even though the lease is expired.
        let policy = ConsumerPolicy::default();
        let mut u = view("op9", LoopPhase::Running, Some("crashed-consumer"));
        u.external_effect_unknown = true;
        let crashed = consumer("op9", u);
        let c = ConsumerOperation {
            last_touched_epoch_ms: 0,
            ..crashed
        };
        assert_eq!(
            c.next_step(&policy, 100_000),
            ConsumerStep::Freeze {
                op_id: "op9".into()
            },
            "unknown external effect must freeze, never reconcile"
        );
    }
}
