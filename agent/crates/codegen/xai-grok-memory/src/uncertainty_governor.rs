//! Uncertainty-to-governance mapping (DEBT-033 C2, adopted from Grok 4.5
//! review as a lightweight decision table — NOT a new subsystem).
//!
//! All inputs are existing signals already produced by the runtime:
//! - goal no-progress count (goal backoff)
//! - priority aging state (class_fairness)
//! - cache hit ratio (A2 cache_health)
//! - effort demotion (A3 EffortController)
//! - repair loop depth (B2 RepairLoop)
//!
//! The table maps signal combinations to EXISTING governance actions, and
//! every decision is journaled (INV-UG-01). `FailClosed` is terminal and
//! cannot be auto-revived (INV-UG-02). `PauseAndRequestHuman` aligns with the
//! M5/M6 human gates (INV-UG-03) — this module only *proposes*; the actor
//! enforces.

use serde::{Deserialize, Serialize};

/// Collapse threshold for cache health (matches the A2 alert line).
pub const CACHE_COLLAPSE_HIT_RATIO: f64 = 0.85;

/// No-progress turns before the governor proposes a pause (goal backoff
/// already pauses at 4; the governor flags earlier for effort escalation).
pub const NO_PROGRESS_ESCALATE_TURNS: u32 = 2;

/// Repair-loop depth at which the governor escalates effort (B2 loop active).
pub const REPAIR_LOOP_ESCALATE_DEPTH: u32 = 1;

/// Signals observed at the decision point.
#[derive(Debug, Clone, Copy, PartialEq, Default, Serialize, Deserialize)]
pub struct UncertaintySignals {
    /// Consecutive turns without host-verifiable progress.
    pub no_progress_turns: u32,
    /// Priority aging has demoted this goal.
    pub priority_demoted: bool,
    /// Recent cache hit ratio; `None` when unknown.
    pub recent_cache_hit_ratio: Option<f64>,
    /// Effort was demoted by the controller this turn.
    pub effort_demoted: bool,
    /// Current repair-loop depth (0 = no repair loop active).
    pub repair_loop_depth: u32,
    /// A human gate (M5/M6) is pending for this flow.
    pub human_gate_pending: bool,
}

/// Governance actions the actor may take. All map to existing machinery.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum GovernanceAction {
    /// Proceed unchanged.
    Continue,
    /// Escalate reasoning effort (A3 EffortController path).
    EscalateEffort,
    /// Pause and request human input (aligned with M5/M6 gates, INV-UG-03).
    PauseAndRequestHuman,
    /// Demote the goal's priority (class_fairness aging path).
    DemoteGoalPriority,
    /// Force a compaction / cache-reset point (A2 policy path).
    ForceCompaction,
    /// Terminal: stop, do not auto-recover (INV-UG-02).
    FailClosed,
}

/// Decision table (pure; the actor applies it and journals the outcome).
pub fn decide(signals: &UncertaintySignals) -> GovernanceAction {
    // 1. Human gate pending + stalled → ask the human (never auto-resolve).
    if signals.human_gate_pending
        && (signals.no_progress_turns >= NO_PROGRESS_ESCALATE_TURNS || signals.priority_demoted)
    {
        return GovernanceAction::PauseAndRequestHuman;
    }
    // 2. Persistent stall + prior demotion → the goal is not recoverable
    //    autonomously; fail closed rather than burn budget.
    if signals.priority_demoted && signals.no_progress_turns >= 4 {
        return GovernanceAction::FailClosed;
    }
    // 3. Cache collapse → the prefix is contested; force a reset point.
    if signals
        .recent_cache_hit_ratio
        .is_some_and(|ratio| ratio < CACHE_COLLAPSE_HIT_RATIO)
    {
        return GovernanceAction::ForceCompaction;
    }
    // 4. Early stall or active repair loop → escalate effort.
    if signals.no_progress_turns >= NO_PROGRESS_ESCALATE_TURNS
        || signals.repair_loop_depth >= REPAIR_LOOP_ESCALATE_DEPTH
    {
        return GovernanceAction::EscalateEffort;
    }
    // 5. Priority demotion alone → keep priority demoted (no action needed
    //    here; aging already applied). Effort demotion alone → keep.
    GovernanceAction::Continue
}

#[cfg(test)]
mod tests {
    use super::*;

    fn s(
        no_progress: u32,
        demoted: bool,
        hit: Option<f64>,
        effort_demoted: bool,
        repair_depth: u32,
        gate: bool,
    ) -> UncertaintySignals {
        UncertaintySignals {
            no_progress_turns: no_progress,
            priority_demoted: demoted,
            recent_cache_hit_ratio: hit,
            effort_demoted,
            repair_loop_depth: repair_depth,
            human_gate_pending: gate,
        }
    }

    #[test]
    fn calm_signals_continue() {
        assert_eq!(decide(&s(0, false, Some(0.98), false, 0, false)), GovernanceAction::Continue);
        assert_eq!(decide(&s(1, false, Some(0.90), false, 0, false)), GovernanceAction::Continue);
    }

    #[test]
    fn early_stall_or_repair_escalates_effort() {
        assert_eq!(decide(&s(2, false, Some(0.98), false, 0, false)), GovernanceAction::EscalateEffort);
        assert_eq!(decide(&s(0, false, Some(0.98), false, 1, false)), GovernanceAction::EscalateEffort);
        assert_eq!(decide(&s(2, false, Some(0.98), false, 2, false)), GovernanceAction::EscalateEffort);
    }

    #[test]
    fn cache_collapse_forces_compaction() {
        assert_eq!(decide(&s(0, false, Some(0.80), false, 0, false)), GovernanceAction::ForceCompaction);
        assert_eq!(decide(&s(2, false, Some(0.84), false, 1, false)), GovernanceAction::ForceCompaction);
        // Unknown ratio is not a collapse signal.
        assert_eq!(decide(&s(0, false, None, false, 0, false)), GovernanceAction::Continue);
    }

    #[test]
    fn demoted_and_deep_stall_fails_closed_inv_ug_02() {
        assert_eq!(decide(&s(4, true, Some(0.98), false, 0, false)), GovernanceAction::FailClosed);
        assert_eq!(decide(&s(5, true, Some(0.90), true, 3, false)), GovernanceAction::FailClosed);
        // Demoted but not stalled: no auto-fail.
        assert_eq!(decide(&s(1, true, Some(0.98), false, 0, false)), GovernanceAction::Continue);
    }

    #[test]
    fn human_gate_pending_pauses_inv_ug_03() {
        assert_eq!(decide(&s(2, false, Some(0.98), false, 0, true)), GovernanceAction::PauseAndRequestHuman);
        assert_eq!(decide(&s(1, true, Some(0.98), false, 0, true)), GovernanceAction::PauseAndRequestHuman);
        // Gate pending but healthy → continue working.
        assert_eq!(decide(&s(0, false, Some(0.98), false, 0, true)), GovernanceAction::Continue);
    }

    #[test]
    fn signals_serialize_round_trip() {
        let signals = s(2, true, Some(0.80), true, 1, false);
        let bytes = serde_json::to_vec(&signals).unwrap();
        let back: UncertaintySignals = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(signals, back);
    }
}
