//! Class-fair dispatch with aging — DEBT-028 W2b-1.
//!
//! The authority mailbox processes classes in a fixed priority order
//! (`control → revoke/cancel → delivery → operation → budget →
//! claim/snapshot`). Strict priority is deterministic, but it starves the
//! lowest class: under sustained control traffic, claims are enqueued yet
//! never reduced — "the tree responds but no fact is ever accepted". That is
//! a silent liveness failure, not a fairness nicety.
//!
//! This module provides the pure decision: with aging enabled, the lowest
//! non-empty class whose oldest message has waited past the threshold is
//! served next (it "ages up" one level). With aging disabled the starvation
//! is reproducible — which is exactly what the negative test asserts, so the
//! mechanism is proven necessary, not decorative.

pub const CLASS_PRIORITY_ORDER: [&str; 6] = [
    "control",
    "revoke_cancel",
    "delivery",
    "operation",
    "budget",
    "claim_snapshot",
];

/// One priority class in the dispatch queue.
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct ClassQueueState {
    /// Index into [`CLASS_PRIORITY_ORDER`] (0 = highest priority).
    pub class_index: usize,
    pub count: u32,
    /// Oldest enqueued message wait (monotonic clock, ms).
    pub oldest_wait_ms: u64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FairnessConfig {
    /// None = strict priority (deterministic, starvation reproducible).
    pub aging_threshold_ms: Option<u64>,
}

impl FairnessConfig {
    pub fn strict() -> Self {
        Self {
            aging_threshold_ms: None,
        }
    }

    pub fn with_aging(threshold_ms: u64) -> Self {
        Self {
            aging_threshold_ms: Some(threshold_ms),
        }
    }
}

/// Next class to dispatch, or `None` when the queue is empty.
///
/// Rule (aging on): let `L` be the lowest-priority non-empty class. If
/// `L.oldest_wait_ms >= threshold`, serve `L` (it has aged up past every
/// higher class). Otherwise serve the highest-priority non-empty class.
pub fn next_class(state: &[ClassQueueState], config: &FairnessConfig) -> Option<usize> {
    let non_empty: Vec<&ClassQueueState> = state.iter().filter(|c| c.count > 0).collect();
    let lowest = non_empty.iter().max_by_key(|c| c.class_index)?;
    if let Some(threshold) = config.aging_threshold_ms
        && lowest.oldest_wait_ms >= threshold
    {
        return Some(lowest.class_index);
    }
    non_empty.iter().min_by_key(|c| c.class_index).map(|c| c.class_index)
}

/// Class starvation metric (ms): the oldest wait across the lowest-priority
/// non-empty class — the value aging removes.
pub fn class_starvation_ms(state: &[ClassQueueState]) -> u64 {
    state
        .iter()
        .filter(|c| c.count > 0)
        .max_by_key(|c| c.class_index)
        .map_or(0, |c| c.oldest_wait_ms)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn state(classes: &[(usize, u32, u64)]) -> Vec<ClassQueueState> {
        classes
            .iter()
            .map(|(idx, count, wait)| ClassQueueState {
                class_index: *idx,
                count: *count,
                oldest_wait_ms: *wait,
            })
            .collect()
    }

    #[test]
    fn strict_priority_starves_lowest_class_reproducibly() {
        // Control (class 0) has a continuous message stream; claim/snapshot
        // (class 5) waits forever. With aging disabled the lowest class is
        // NEVER served — reproducible starvation (the negative case that
        // proves aging is required, not decorative).
        let queue = state(&[(0, 1, 0), (5, 1, 99_999)]);
        let config = FairnessConfig::strict();
        for _ in 0..100 {
            assert_eq!(next_class(&queue, &config), Some(0));
        }
        assert_eq!(class_starvation_ms(&queue), 99_999);
    }

    #[test]
    fn aging_serves_the_starved_class_after_threshold() {
        let queue = state(&[(0, 1, 0), (5, 1, 5_000)]);
        // Below threshold: strict priority still applies.
        let config = FairnessConfig::with_aging(10_000);
        assert_eq!(next_class(&queue, &config), Some(0));
        // Above threshold: the starved class ages up and is served.
        let aged = state(&[(0, 1, 0), (5, 1, 10_000)]);
        assert_eq!(next_class(&aged, &config), Some(5));
        // Empty lower classes leave the highest priority in charge.
        let only_control = state(&[(0, 1, 0)]);
        assert_eq!(next_class(&only_control, &config), Some(0));
    }

    #[test]
    fn aging_boundary_is_inclusive() {
        let queue = state(&[(3, 1, 1_000), (5, 1, 500)]);
        let config = FairnessConfig::with_aging(1_000);
        // Highest non-empty class 3 serves; class 5 is below threshold.
        assert_eq!(next_class(&queue, &config), Some(3));
        let at_threshold = state(&[(3, 1, 1_000), (5, 1, 1_000)]);
        assert_eq!(next_class(&at_threshold, &config), Some(5));
    }

    #[test]
    fn empty_queue_returns_none() {
        assert_eq!(next_class(&[], &FairnessConfig::strict()), None);
        assert_eq!(class_starvation_ms(&[]), 0);
    }
}
