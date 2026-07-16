//! Repair cycle state machine.

use super::config::Config;

/// Tracks the self-repair cycle for a session.
///
/// After each verify-after-edit failure the cycle increments.  When
/// `max_repair` is reached the state machine stops and returns the
/// diagnostic feedback to the user instead of the model.
#[derive(Debug, Clone)]
pub struct RepairState {
    /// Current repair cycle (0 = no repairs yet).
    pub cycle: u32,
    /// Maximum cycles before giving up.
    pub max_repair: u32,
}

impl RepairState {
    pub fn new(cfg: &Config) -> Self {
        Self {
            cycle: 0,
            max_repair: cfg.max_repair,
        }
    }

    /// Call after a failed verification.  Returns `true` if another repair
    /// cycle is allowed, `false` if the budget is exhausted.
    pub fn try_repair(&mut self) -> bool {
        if self.cycle >= self.max_repair {
            return false;
        }
        self.cycle += 1;
        true
    }

    /// Whether the repair budget is exhausted.
    pub fn exhausted(&self) -> bool {
        self.cycle >= self.max_repair
    }

    /// Format a feedback message for the model when a repair is attempted.
    pub fn repair_feedback(&self, diagnostics: &str) -> String {
        format!(
            "\n[verify-after-edit] Build/test failed (repair attempt {}/{}).\n\
             Fix the following issues and try again:\n\
             {}\n\
             If you cannot fix these, tell the user what is wrong.\n",
            self.cycle, self.max_repair, diagnostics
        )
    }

    /// Format a message when the repair budget is exhausted.
    pub fn exhausted_feedback(&self, diagnostics: &str) -> String {
        format!(
            "\n[verify-after-edit] Repair budget exhausted ({} cycles).\n\
             The following issues could not be automatically fixed:\n\
             {}\n\
             Please review and fix manually.\n",
            self.max_repair, diagnostics
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_repair_cycles() {
        let cfg = Config::default();
        let mut state = RepairState::new(&cfg);
        assert!(!state.exhausted());
        assert!(state.try_repair()); // cycle 1
        assert_eq!(state.cycle, 1);
        assert!(state.try_repair()); // cycle 2
        assert!(state.try_repair()); // cycle 3
        assert!(!state.try_repair()); // exhausted
        assert!(state.exhausted());
    }

    #[test]
    fn test_repair_feedback_contains_diagnostics() {
        let cfg = Config::default();
        let mut state = RepairState::new(&cfg);
        state.try_repair();
        let fb = state.repair_feedback("ERROR: undefined: foo");
        assert!(fb.contains("repair attempt 1/3"));
        assert!(fb.contains("undefined: foo"));
    }
}
