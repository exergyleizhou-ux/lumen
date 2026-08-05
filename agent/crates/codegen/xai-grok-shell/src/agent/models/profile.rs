//! Model profiles + adaptive effort controller (DEBT-033 A3).
//!
//! Pure decision logic, host-owned. The sampler request site applies the
//! resolved effort; this module never touches the wire.
//!
//! Official DeepSeek agentic profile (api-docs.deepseek.com 2026-07-31,
//! change log Note 1 — DeepSeek Harness minimal mode):
//! `temperature = 1.0, top_p = 0.95, max effort`. Default effort for agent
//! work is `high` (community-verified: max burns output budget in long
//! reasoning loops); escalation is signal-driven and budget-guarded.

use xai_grok_sampling_types::ReasoningEffort;

/// DeepSeek-V4-Flash official context window (config.json max_position_embeddings).
pub const DEEPSEEK_V4_FLASH_CONTEXT_WINDOW: u32 = 1_048_576;

/// Agentic sampling params (official, 2026-07-31).
pub const DEEPSEEK_V4_FLASH_TEMPERATURE: f64 = 1.0;
pub const DEEPSEEK_V4_FLASH_TOP_P: f64 = 0.95;

/// Cache-hit ratio below which the controller demotes effort
/// (cache collapse signals degraded/contested context).
pub const CACHE_COLLAPSE_HIT_RATIO: f64 = 0.85;

/// Remaining output budget below which effort must not stay/enter Max
/// (INV-EP-03): max reasoning would exhaust the quota mid-task.
pub const OUTPUT_BUDGET_DEMOTE_THRESHOLD: u32 = 8_192;

/// Goal complexity (0..=10) at which the controller escalates effort.
pub const COMPLEXITY_ESCALATE_AT: u8 = 8;

/// Consecutive verification failures at which the controller escalates effort.
pub const VERIFY_FAILURES_ESCALATE_AT: u32 = 2;

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ThinkingMode {
    Enabled,
    Disabled,
}

#[derive(Debug, Clone, Copy, PartialEq, serde::Serialize, serde::Deserialize)]
pub struct SamplingParams {
    pub temperature: f64,
    pub top_p: f64,
}

/// One model's runtime contract (DEBT-033 A3). Not user-facing config: it is
/// the resolved, provider-verified contract the harness operates under.
#[derive(Debug, Clone, PartialEq, serde::Serialize, serde::Deserialize)]
pub struct ModelProfile {
    pub id: String,
    pub context_window: u32,
    /// `None` = defer to provider default; the TurnContextBudget guard still
    /// caps the working budget (do not invent unverified numbers).
    pub max_output_tokens: Option<u32>,
    pub sampling: SamplingParams,
    pub thinking: ThinkingMode,
    pub default_effort: ReasoningEffort,
    /// INV-EP-02: Max effort forces VerifyFirst.
    pub verify_first_at_max: bool,
}

/// Official DeepSeek-V4-Flash-0731 profile (`deepseek-v4-flash` API id).
pub fn deepseek_v4_flash_0731() -> ModelProfile {
    ModelProfile {
        id: "deepseek-v4-flash-0731".into(),
        context_window: DEEPSEEK_V4_FLASH_CONTEXT_WINDOW,
        max_output_tokens: None,
        sampling: SamplingParams {
            temperature: DEEPSEEK_V4_FLASH_TEMPERATURE,
            top_p: DEEPSEEK_V4_FLASH_TOP_P,
        },
        thinking: ThinkingMode::Enabled,
        default_effort: ReasoningEffort::High,
        verify_first_at_max: true,
    }
}

/// Live signals the effort controller may observe.
#[derive(Debug, Clone, Copy, PartialEq, Default)]
pub struct EffortSignals {
    /// Goal complexity 0..=10 (JTMS goal state).
    pub goal_complexity: u8,
    /// Consecutive verification failures.
    pub consecutive_verify_failures: u32,
    /// A verification repair loop is active.
    pub repair_loop_active: bool,
    /// Remaining output budget (tokens).
    pub remaining_output_budget: u32,
    /// Recent cache hit ratio; `None` when unknown.
    pub recent_cache_hit_ratio: Option<f64>,
    /// Turn budget is tight (TurnContextBudget near exhaustion).
    pub turn_budget_tight: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EffortDecision {
    Keep,
    Escalate,
    Demote,
}

/// Decision table (DEBT-033 A3, INV-EP-01/03).
///
/// Order matters and is intentional:
/// 1. **Demote first** (safety): output budget exhausted, turn budget tight,
///    or cache collapse — never stay/enter Max on a starving budget.
/// 2. **Escalate** on complexity / failure signals (unless already Max).
/// 3. Otherwise keep.
pub fn decide_effort(current: ReasoningEffort, signals: &EffortSignals) -> EffortDecision {
    let starving = signals.remaining_output_budget < OUTPUT_BUDGET_DEMOTE_THRESHOLD
        || signals.turn_budget_tight
        || signals
            .recent_cache_hit_ratio
            .is_some_and(|ratio| ratio < CACHE_COLLAPSE_HIT_RATIO);
    if starving {
        return EffortDecision::Demote;
    }
    let wants_more = signals.goal_complexity >= COMPLEXITY_ESCALATE_AT
        || signals.consecutive_verify_failures >= VERIFY_FAILURES_ESCALATE_AT
        || signals.repair_loop_active;
    if wants_more && current != ReasoningEffort::Max {
        return EffortDecision::Escalate;
    }
    EffortDecision::Keep
}

/// One-step effort escalation (DEBT-033 C2 application). `None` (no explicit
/// effort) escalates to High; Max stays Max.
pub fn escalate_effort(current: Option<ReasoningEffort>) -> ReasoningEffort {
    match current {
        None | Some(ReasoningEffort::None | ReasoningEffort::Minimal | ReasoningEffort::Low) => {
            ReasoningEffort::High
        }
        Some(ReasoningEffort::Medium | ReasoningEffort::High) => ReasoningEffort::Max,
        Some(ReasoningEffort::Max) => ReasoningEffort::Max,
        Some(ReasoningEffort::Xhigh) => ReasoningEffort::Max,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn official_flash_profile_values() {
        let p = deepseek_v4_flash_0731();
        assert_eq!(p.id, "deepseek-v4-flash-0731");
        assert_eq!(p.context_window, 1_048_576);
        assert_eq!(p.sampling.temperature, 1.0);
        assert_eq!(p.sampling.top_p, 0.95);
        assert_eq!(p.thinking, ThinkingMode::Enabled);
        assert_eq!(p.default_effort, ReasoningEffort::High);
        assert!(p.verify_first_at_max, "INV-EP-02: max forces verify-first");
    }

    #[test]
    fn profile_serializes_and_round_trips() {
        let p = deepseek_v4_flash_0731();
        let bytes = serde_json::to_vec(&p).unwrap();
        let back: ModelProfile = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(p, back);
    }

    fn sig(
        complexity: u8,
        failures: u32,
        repair: bool,
        budget: u32,
        hit: Option<f64>,
        tight: bool,
    ) -> EffortSignals {
        EffortSignals {
            goal_complexity: complexity,
            consecutive_verify_failures: failures,
            repair_loop_active: repair,
            remaining_output_budget: budget,
            recent_cache_hit_ratio: hit,
            turn_budget_tight: tight,
        }
    }

    #[test]
    fn keep_on_calm_signals() {
        let s = sig(3, 0, false, 100_000, Some(0.95), false);
        assert_eq!(decide_effort(ReasoningEffort::High, &s), EffortDecision::Keep);
        assert_eq!(decide_effort(ReasoningEffort::Max, &s), EffortDecision::Keep);
    }

    #[test]
    fn escalate_on_complexity_failures_and_repair() {
        assert_eq!(
            decide_effort(ReasoningEffort::High, &sig(9, 0, false, 100_000, Some(0.95), false)),
            EffortDecision::Escalate
        );
        assert_eq!(
            decide_effort(ReasoningEffort::High, &sig(3, 2, false, 100_000, Some(0.95), false)),
            EffortDecision::Escalate
        );
        assert_eq!(
            decide_effort(ReasoningEffort::High, &sig(3, 0, true, 100_000, Some(0.95), false)),
            EffortDecision::Escalate
        );
    }

    #[test]
    fn no_escalation_beyond_max() {
        assert_eq!(
            decide_effort(ReasoningEffort::Max, &sig(9, 2, true, 100_000, Some(0.95), false)),
            EffortDecision::Keep
        );
    }

    #[test]
    fn demote_on_starving_budget_even_from_max() {
        // INV-EP-03: budget below threshold forbids staying Max.
        assert_eq!(
            decide_effort(ReasoningEffort::Max, &sig(9, 2, true, 8_000, Some(0.95), false)),
            EffortDecision::Demote
        );
        assert_eq!(
            decide_effort(ReasoningEffort::High, &sig(3, 0, false, 8_000, Some(0.95), false)),
            EffortDecision::Demote
        );
    }

    #[test]
    fn demote_on_turn_budget_tight_or_cache_collapse() {
        assert_eq!(
            decide_effort(ReasoningEffort::High, &sig(9, 0, false, 100_000, Some(0.95), true)),
            EffortDecision::Demote
        );
        assert_eq!(
            decide_effort(ReasoningEffort::High, &sig(9, 0, false, 100_000, Some(0.80), false)),
            EffortDecision::Demote
        );
        // Unknown hit ratio is not a collapse signal.
        assert_eq!(
            decide_effort(ReasoningEffort::High, &sig(9, 0, false, 100_000, None, false)),
            EffortDecision::Escalate
        );
    }

    #[test]
    fn effort_escalation_steps() {
        assert_eq!(escalate_effort(None), ReasoningEffort::High);
        assert_eq!(escalate_effort(Some(ReasoningEffort::Low)), ReasoningEffort::High);
        assert_eq!(escalate_effort(Some(ReasoningEffort::High)), ReasoningEffort::Max);
        assert_eq!(escalate_effort(Some(ReasoningEffort::Max)), ReasoningEffort::Max);
        assert_eq!(escalate_effort(Some(ReasoningEffort::Xhigh)), ReasoningEffort::Max);
    }
}
