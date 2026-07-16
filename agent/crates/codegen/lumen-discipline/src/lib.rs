//! Lumen M2 loop discipline (pure logic, host-owned state).
//!
//! - [`StormBreaker`] — same tool + error signature fails ≥ N → nudge/stop
//! - [`RepeatSuccessGuard`] — same tool + args hash succeeds ≥ N → nudge
//! - [`DeliverySessionState`] + [`gate_goal_complete`] — anti fake-complete
//! - [`format_cache_line`] — status-bar friendly cache token summary
//!
//! **Never** put this state into the system-prompt *prefix* (breaks DeepSeek
//! cache). Inject only as turn-tail reminders or tool results.

mod cache;
mod delivery;
mod storm;

pub use cache::{CacheUsage, format_cache_line};
pub use delivery::{
    DELIVERY_REMINDER, DeliveryAction, DeliverySessionState, DeliveryStrictness, GoalGate,
    GoalIncompletePolicy, TodoSnapshot, gate_goal_complete, on_turn_end,
};
pub use storm::{
    RepeatSuccessAction, RepeatSuccessGuard, StormAction, StormBreaker, error_signature,
    hash_tool_args,
};
