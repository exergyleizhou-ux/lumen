//! Delivery / Goal anti-fake-complete gates.

/// Fixed-byte reminder (stable text helps prompt cache when used as tail).
pub const DELIVERY_REMINDER: &str = "\
<delivery-reminder>
This turn modified files but no successful verification evidence was recorded
(build/test/verify). Run the project checks or cite a successful command before
claiming completion.
</delivery-reminder>";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum DeliveryStrictness {
    Off,
    #[default]
    Soft,
    Strict,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum GoalIncompletePolicy {
    /// First `completed:true` with open todos → reject; second may pass.
    #[default]
    SoftOnce,
    /// Never allow complete while incomplete todos remain.
    Strict,
}

/// Host-owned per-session delivery evidence (not in provider prefix).
#[derive(Debug, Clone, Default)]
pub struct DeliverySessionState {
    pub turn_id: u64,
    pub writer_tools_this_turn: u32,
    pub verify_ok_this_turn: bool,
    pub bash_success_with_test_hint: bool,
    pub soft_nudge_count: u32,
    /// SoftOnce: true after we already rejected one complete with open todos.
    pub goal_incomplete_override_armed: bool,
}

impl DeliverySessionState {
    pub fn on_writer_tool(&mut self) {
        self.writer_tools_this_turn = self.writer_tools_this_turn.saturating_add(1);
    }

    pub fn on_verify_ok(&mut self) {
        self.verify_ok_this_turn = true;
    }

    pub fn on_bash_test_success(&mut self, output: &str) {
        let lower = output.to_ascii_lowercase();
        if lower.contains("pass")
            || lower.contains("ok")
            || lower.contains("0 failed")
            || lower.contains("test result: ok")
        {
            self.bash_success_with_test_hint = true;
        }
    }

    pub fn begin_turn(&mut self) {
        self.turn_id = self.turn_id.saturating_add(1);
        self.writer_tools_this_turn = 0;
        self.verify_ok_this_turn = false;
        self.bash_success_with_test_hint = false;
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DeliveryAction {
    None,
    InjectSystemReminder(String),
}

/// Turn-end delivery check.
pub fn on_turn_end(state: &mut DeliverySessionState, strictness: DeliveryStrictness) -> DeliveryAction {
    if matches!(strictness, DeliveryStrictness::Off) {
        return DeliveryAction::None;
    }
    let needs = state.writer_tools_this_turn > 0
        && !state.verify_ok_this_turn
        && !state.bash_success_with_test_hint;
    if !needs {
        return DeliveryAction::None;
    }
    // Soft: at most one nudge per turn (tracked via soft_nudge_count on turn).
    if state.soft_nudge_count > 0 && matches!(strictness, DeliveryStrictness::Soft) {
        // already nudged this "episode" — still allow one per turn after begin_turn resets writers
    }
    state.soft_nudge_count = state.soft_nudge_count.saturating_add(1);
    DeliveryAction::InjectSystemReminder(DELIVERY_REMINDER.to_owned())
}

/// Minimal todo snapshot for goal gate (ids + open?).
#[derive(Debug, Clone)]
pub struct TodoSnapshot {
    pub id: String,
    pub open: bool, // pending or in_progress
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GoalGate {
    Allow,
    Reject {
        reason: &'static str,
        detail: String,
    },
}

/// Gate `update_goal(completed: true)` against incomplete todos.
pub fn gate_goal_complete(
    todos: &[TodoSnapshot],
    delivery: &mut DeliverySessionState,
    policy: GoalIncompletePolicy,
    enabled: bool,
) -> GoalGate {
    if !enabled {
        return GoalGate::Allow;
    }
    let incomplete: Vec<&str> = todos
        .iter()
        .filter(|t| t.open)
        .map(|t| t.id.as_str())
        .collect();
    if incomplete.is_empty() {
        return GoalGate::Allow;
    }
    let detail = format!(
        "Incomplete todos remain: {}. Finish or cancel them before update_goal(completed: true).",
        incomplete.join(", ")
    );
    match policy {
        GoalIncompletePolicy::Strict => GoalGate::Reject {
            reason: "incomplete_todos",
            detail,
        },
        GoalIncompletePolicy::SoftOnce => {
            if !delivery.goal_incomplete_override_armed {
                delivery.goal_incomplete_override_armed = true;
                GoalGate::Reject {
                    reason: "incomplete_todos",
                    detail: format!(
                        "{detail} (soft gate: one more completed:true may override)"
                    ),
                }
            } else {
                GoalGate::Allow
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn turn_end_nudge_when_writers_without_verify() {
        let mut s = DeliverySessionState::default();
        s.on_writer_tool();
        match on_turn_end(&mut s, DeliveryStrictness::Soft) {
            DeliveryAction::InjectSystemReminder(m) => {
                assert!(m.contains("delivery-reminder"));
            }
            _ => panic!("expected reminder"),
        }
    }

    #[test]
    fn turn_end_silent_when_verify_ok() {
        let mut s = DeliverySessionState::default();
        s.on_writer_tool();
        s.on_verify_ok();
        assert_eq!(
            on_turn_end(&mut s, DeliveryStrictness::Soft),
            DeliveryAction::None
        );
    }

    #[test]
    fn goal_soft_once() {
        let todos = vec![TodoSnapshot {
            id: "a".into(),
            open: true,
        }];
        let mut d = DeliverySessionState::default();
        match gate_goal_complete(&todos, &mut d, GoalIncompletePolicy::SoftOnce, true) {
            GoalGate::Reject { reason, .. } => assert_eq!(reason, "incomplete_todos"),
            _ => panic!("first should reject"),
        }
        assert!(d.goal_incomplete_override_armed);
        assert!(matches!(
            gate_goal_complete(&todos, &mut d, GoalIncompletePolicy::SoftOnce, true),
            GoalGate::Allow
        ));
    }

    #[test]
    fn goal_strict_never_overrides() {
        let todos = vec![TodoSnapshot {
            id: "x".into(),
            open: true,
        }];
        let mut d = DeliverySessionState::default();
        d.goal_incomplete_override_armed = true;
        assert!(matches!(
            gate_goal_complete(&todos, &mut d, GoalIncompletePolicy::Strict, true),
            GoalGate::Reject { .. }
        ));
    }

    #[test]
    fn goal_allow_when_all_done() {
        let todos = vec![TodoSnapshot {
            id: "a".into(),
            open: false,
        }];
        let mut d = DeliverySessionState::default();
        assert!(matches!(
            gate_goal_complete(&todos, &mut d, GoalIncompletePolicy::SoftOnce, true),
            GoalGate::Allow
        ));
    }
}
