//! Verification repair-loop governor (DEBT-033 B2 core).
//!
//! Pure decision logic for the verification-obligation loop. The agent loop
//! (shell) drives the real pipeline; this module decides whether a repair may
//! proceed, when the loop must fail closed, and how budget participates.
//!
//! Contract (INV-VO-01..05):
//! - INV-VO-03: exceeding `max_repair_attempts` fails closed (terminal).
//! - INV-VO-05: verification itself consumes turn budget.
//! - A `Failed` attempt with attempts remaining must create a repair
//!   obligation (the shell wires that into JTMS).

use serde::{Deserialize, Serialize};

/// Default repair-attempt cap (contract §4 B2: `max_repair_attempts = 3`).
pub const DEFAULT_MAX_REPAIR_ATTEMPTS: u32 = 3;

/// Terminal failure reason of a repair loop.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RepairExhaustionReason {
    /// Attempts consumed without a passing verification.
    AttemptsExhausted,
    /// Turn budget ran out mid-loop.
    BudgetExhausted,
    /// A hard interrupt (operator / session end) stopped the loop.
    Interrupted,
}

/// One step of the repair loop.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RepairAttemptStatus {
    /// Verification passed; the effect is signed off.
    Succeeded,
    /// Verification failed; a repair may be created (if attempts remain).
    Failed,
    /// Verification could not run (tool missing / skipped) — treated as a
    /// failed attempt for the cap, never as success.
    Inconclusive,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RepairLoopState {
    /// Loop open; a repair may be created.
    Active,
    /// Loop closed after success; the effect is signed off.
    Succeeded,
    /// Loop failed closed; no further repair may be created.
    Exhausted { reason: RepairExhaustionReason },
}

/// Mutable loop state. The shell owns this (SessionActor); this module only
/// makes decisions over it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct RepairLoop {
    pub max_repair_attempts: u32,
    /// Attempts consumed so far (verifications actually run).
    pub attempts: u32,
    /// Remaining turn budget at the last decision (tokens).
    pub remaining_budget_tokens: u32,
    pub state: RepairLoopState,
}

impl RepairLoop {
    pub fn new(max_repair_attempts: u32, remaining_budget_tokens: u32) -> Self {
        Self {
            max_repair_attempts: max_repair_attempts.max(1),
            attempts: 0,
            remaining_budget_tokens,
            state: RepairLoopState::Active,
        }
    }

    /// Minimum budget a verification run may consume before the loop fails
    /// closed (INV-VO-05: verification consumes budget; starving budgets
    /// cannot verify).
    pub const MIN_VERIFY_BUDGET_TOKENS: u32 = 512;

    /// Record one verification outcome. Returns the resulting state.
    ///
    /// * `Succeeded` — loop closes as signed off.
    /// * `Failed` / `Inconclusive` — consumes an attempt; the loop stays
    ///   Active while attempts remain and budget allows, otherwise it fails
    ///   closed (INV-VO-03).
    pub fn record(
        &mut self,
        status: RepairAttemptStatus,
        budget_consumed: u32,
    ) -> RepairLoopState {
        if self.state != RepairLoopState::Active {
            return self.state;
        }
        self.attempts = self.attempts.saturating_add(1);
        self.remaining_budget_tokens = self.remaining_budget_tokens.saturating_sub(budget_consumed);
        match status {
            RepairAttemptStatus::Succeeded => {
                self.state = RepairLoopState::Succeeded;
            }
            RepairAttemptStatus::Failed | RepairAttemptStatus::Inconclusive => {
                if self.attempts >= self.max_repair_attempts {
                    self.state = RepairLoopState::Exhausted {
                        reason: RepairExhaustionReason::AttemptsExhausted,
                    };
                } else if self.remaining_budget_tokens < Self::MIN_VERIFY_BUDGET_TOKENS {
                    self.state = RepairLoopState::Exhausted {
                        reason: RepairExhaustionReason::BudgetExhausted,
                    };
                }
            }
        }
        self.state
    }

    /// May a repair obligation be created right now?
    pub fn may_repair(&self) -> bool {
        self.state == RepairLoopState::Active
    }

    /// Interrupt the loop (operator/session end). Terminal; cannot be revived.
    pub fn interrupt(&mut self) {
        if self.state == RepairLoopState::Active {
            self.state = RepairLoopState::Exhausted {
                reason: RepairExhaustionReason::Interrupted,
            };
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn success_closes_loop_and_signs_off() {
        let mut loop_state = RepairLoop::new(3, 10_000);
        assert!(loop_state.may_repair());
        assert_eq!(
            loop_state.record(RepairAttemptStatus::Succeeded, 500),
            RepairLoopState::Succeeded
        );
        assert!(!loop_state.may_repair());
        // Recording after closure is a no-op.
        assert_eq!(
            loop_state.record(RepairAttemptStatus::Failed, 500),
            RepairLoopState::Succeeded
        );
        assert_eq!(loop_state.attempts, 1);
    }

    #[test]
    fn failures_exhaust_after_cap_inv_vo_03() {
        let mut loop_state = RepairLoop::new(3, 10_000);
        assert_eq!(
            loop_state.record(RepairAttemptStatus::Failed, 500),
            RepairLoopState::Active
        );
        assert_eq!(
            loop_state.record(RepairAttemptStatus::Failed, 500),
            RepairLoopState::Active
        );
        assert_eq!(
            loop_state.record(RepairAttemptStatus::Failed, 500),
            RepairLoopState::Exhausted {
                reason: RepairExhaustionReason::AttemptsExhausted
            }
        );
        assert!(!loop_state.may_repair());
        assert_eq!(loop_state.attempts, 3);
    }

    #[test]
    fn inconclusive_never_counts_as_success() {
        let mut loop_state = RepairLoop::new(1, 10_000);
        assert_eq!(
            loop_state.record(RepairAttemptStatus::Inconclusive, 0),
            RepairLoopState::Exhausted {
                reason: RepairExhaustionReason::AttemptsExhausted
            }
        );
    }

    #[test]
    fn budget_exhaustion_fails_closed_inv_vo_05() {
        let mut loop_state = RepairLoop::new(5, 600);
        assert_eq!(
            loop_state.record(RepairAttemptStatus::Failed, 500),
            RepairLoopState::Exhausted {
                reason: RepairExhaustionReason::BudgetExhausted
            }
        );
        assert!(!loop_state.may_repair());
    }

    #[test]
    fn interrupt_is_terminal_and_cannot_revive() {
        let mut loop_state = RepairLoop::new(3, 10_000);
        loop_state.interrupt();
        assert_eq!(
            loop_state.state,
            RepairLoopState::Exhausted {
                reason: RepairExhaustionReason::Interrupted
            }
        );
        // Even a success after interrupt is refused.
        assert_eq!(
            loop_state.record(RepairAttemptStatus::Succeeded, 0),
            RepairLoopState::Exhausted {
                reason: RepairExhaustionReason::Interrupted
            }
        );
    }

    #[test]
    fn max_attempts_floor_at_one() {
        let loop_state = RepairLoop::new(0, 10_000);
        assert_eq!(loop_state.max_repair_attempts, 1);
    }

    #[test]
    fn loop_state_serializes() {
        let loop_state = RepairLoop::new(3, 1_000);
        let bytes = serde_json::to_vec(&loop_state).unwrap();
        let back: RepairLoop = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(loop_state, back);
    }
}
