//! Staged compaction policy (DEBT-033 A2-b).
//!
//! Long-context providers (DeepSeek V4-Flash: 1M window) make proportional
//! thresholds (e.g. 0.6/0.8/0.9 of window) fire far too late or too early
//! depending on the absolute scale. This module drives compaction stages by
//! **absolute stale-token thresholds** plus a **remaining-budget trigger**,
//! per the adopted review contract:
//!
//! - Level 1 (`Level1Snip`): stale tool output is archived and shortened with
//!   deterministic head/tail markers (full content stays recoverable).
//! - Level 2 (`Level2Placeholder`): stale tool output is replaced with a
//!   short placeholder.
//! - Level 3 (`Level3Summary`): summary folding — only when remaining budget
//!   is tight; user turns and existing digests are never folded
//!   (`never_fold_user` is a hard invariant, not a knob).
//!
//! Pure logic only: this module never touches history. The pruning site
//! (`xai-chat-state/src/actor/mutations.rs` retained-tool prune) is the
//! execution hook, wired in a follow-up cycle.

use serde::{Deserialize, Serialize};

/// Which compaction stage applies to the current conversation state.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CompactionStage {
    /// Below every threshold: leave the session untouched.
    None,
    /// Archive + deterministic head/tail markers on stale tool output.
    Level1Snip,
    /// Replace stale tool output with a short placeholder.
    Level2Placeholder,
    /// Summary folding (budget-tight only; user turns/digests never folded).
    Level3Summary,
}

/// Absolute-token thresholds + remaining-budget trigger for staged compaction.
#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
pub struct CompactionPolicy {
    /// Stale tool-output tokens at which Level-1 snip starts.
    pub snip_threshold_tokens: u64,
    /// Stale tool-output tokens at which Level-2 placeholder replaces snip.
    pub placeholder_threshold_tokens: u64,
    /// Stale tool-output tokens at which summary folding may run.
    pub fold_threshold_tokens: u64,
    /// When remaining budget ≤ this ratio of the context window, folding is
    /// allowed (dual drive with `fold_threshold_tokens`).
    pub remaining_budget_trigger_ratio: f64,
    /// Hard invariant (INV-CS-04): user turns and existing digests are never
    /// folded. Not configurable by design.
    pub never_fold_user: bool,
}

impl Default for CompactionPolicy {
    fn default() -> Self {
        // Initial values per contract §3 A2-3; calibrated by the eval baseline
        // (DEBT-033 ⑥) before any production tuning.
        Self {
            snip_threshold_tokens: 50_000,
            placeholder_threshold_tokens: 80_000,
            fold_threshold_tokens: 120_000,
            remaining_budget_trigger_ratio: 0.4,
            never_fold_user: true,
        }
    }
}

impl CompactionPolicy {
    /// Decide the compaction stage for the current conversation.
    ///
    /// * `stale_tool_tokens` — accumulated tokens of stale (already-consumed)
    ///   tool output.
    /// * `total_tokens` — current prompt size.
    /// * `context_window` — provider window (0 = unknown; budget trigger
    ///   disabled).
    pub fn stage_for(
        &self,
        stale_tool_tokens: u64,
        total_tokens: u64,
        context_window: u64,
    ) -> CompactionStage {
        debug_assert!(self.never_fold_user, "never_fold_user is a hard invariant");
        let remaining = context_window.saturating_sub(total_tokens);
        let budget_tight = context_window > 0
            && (remaining as f64) <= (context_window as f64) * self.remaining_budget_trigger_ratio;
        if stale_tool_tokens >= self.fold_threshold_tokens && budget_tight {
            CompactionStage::Level3Summary
        } else if stale_tool_tokens >= self.placeholder_threshold_tokens {
            CompactionStage::Level2Placeholder
        } else if stale_tool_tokens >= self.snip_threshold_tokens {
            CompactionStage::Level1Snip
        } else {
            CompactionStage::None
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn below_snip_threshold_is_none() {
        let policy = CompactionPolicy::default();
        assert_eq!(policy.stage_for(49_999, 10_000, 1_048_576), CompactionStage::None);
        assert_eq!(policy.stage_for(0, 10_000, 1_048_576), CompactionStage::None);
    }

    #[test]
    fn snip_then_placeholder_by_absolute_tokens() {
        let policy = CompactionPolicy::default();
        assert_eq!(policy.stage_for(50_000, 10_000, 1_048_576), CompactionStage::Level1Snip);
        assert_eq!(policy.stage_for(79_999, 10_000, 1_048_576), CompactionStage::Level1Snip);
        assert_eq!(policy.stage_for(80_000, 10_000, 1_048_576), CompactionStage::Level2Placeholder);
        assert_eq!(policy.stage_for(119_999, 10_000, 1_048_576), CompactionStage::Level2Placeholder);
    }

    #[test]
    fn summary_requires_fold_threshold_and_tight_budget() {
        let policy = CompactionPolicy::default();
        // Above fold threshold but budget NOT tight → placeholder, not summary.
        assert_eq!(
            policy.stage_for(200_000, 10_000, 1_048_576),
            CompactionStage::Level2Placeholder
        );
        // Budget tight (remaining ≤ 40% of window) AND above fold threshold → summary.
        assert_eq!(
            policy.stage_for(120_000, 700_000, 1_048_576),
            CompactionStage::Level3Summary
        );
        // Budget tight alone is not enough.
        assert_eq!(
            policy.stage_for(50_000, 700_000, 1_048_576),
            CompactionStage::Level1Snip
        );
    }

    #[test]
    fn unknown_context_window_disables_budget_trigger() {
        let policy = CompactionPolicy::default();
        // context_window = 0: budget trigger disabled → no summary even when
        // tokens look "tight" against a zero window.
        assert_eq!(
            policy.stage_for(200_000, 0, 0),
            CompactionStage::Level2Placeholder
        );
    }

    #[test]
    fn one_million_window_does_not_prematurely_summarize() {
        let policy = CompactionPolicy::default();
        // 1M window: 400K total tokens leaves 60% budget → placeholder, not summary.
        assert_eq!(
            policy.stage_for(200_000, 400_000, 1_048_576),
            CompactionStage::Level2Placeholder
        );
        // 900K total tokens leaves ~14% budget → summary allowed.
        assert_eq!(
            policy.stage_for(200_000, 900_000, 1_048_576),
            CompactionStage::Level3Summary
        );
    }

    #[test]
    fn never_fold_user_is_hard_default() {
        assert!(CompactionPolicy::default().never_fold_user);
    }

    #[test]
    fn policy_serializes_and_round_trips() {
        let policy = CompactionPolicy::default();
        let bytes = serde_json::to_vec(&policy).unwrap();
        let back: CompactionPolicy = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(policy, back);
        assert!(bytes.len() > 10);
    }
}
