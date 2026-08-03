//! Host-side call surfaces for NextGen pure control planes (offline-safe).
//!
//! These wrappers let shell / operator paths exercise Advisor shadow and
//! Kairos supervisor contracts without a second runtime and without claiming
//! durable daemon or exact-binary product readiness.

use xai_grok_memory::{
    AdviceReportV1, AdvisorDeny, AdvisorMode, AttemptSealTracker, KairosCommand, KairosDeny,
    KairosSupervisorState, LoopPhase, SealedAttemptReceiptV1, advice_may_mutate_authority,
    apply_kairos_command, issue_shadow_advice, ordinary_turn_max_retries,
};

/// Session-local shadow advisor host: records advice, never mutates authority.
#[derive(Debug, Clone)]
pub struct ShadowAdvisorHost {
    pub mode: AdvisorMode,
    pub reports: Vec<AdviceReportV1>,
}

impl Default for ShadowAdvisorHost {
    fn default() -> Self {
        Self::new(AdvisorMode::Shadow)
    }
}

impl ShadowAdvisorHost {
    pub fn new(mode: AdvisorMode) -> Self {
        Self {
            mode,
            reports: Vec::new(),
        }
    }

    /// Issue and retain shadow advice. Always `applies_authority == false`.
    pub fn issue(
        &mut self,
        advice_id: impl Into<String>,
        summary: impl Into<String>,
        recommended_next_step: Option<String>,
        usage_receipt_ref: Option<String>,
    ) -> Result<&AdviceReportV1, AdvisorDeny> {
        let report = issue_shadow_advice(
            self.mode,
            advice_id,
            summary,
            recommended_next_step,
            usage_receipt_ref,
        )?;
        debug_assert!(!advice_may_mutate_authority(&report));
        self.reports.push(report);
        Ok(self.reports.last().expect("just pushed"))
    }

    pub fn last(&self) -> Option<&AdviceReportV1> {
        self.reports.last()
    }
}

/// Session-local Kairos operator surface (pure state machine only).
#[derive(Debug, Clone)]
pub struct KairosControlHost {
    pub state: KairosSupervisorState,
}

impl KairosControlHost {
    pub fn new(id: impl Into<String>) -> Self {
        Self {
            state: KairosSupervisorState::new(id),
        }
    }

    pub fn phase(&self) -> LoopPhase {
        self.state.phase()
    }

    pub fn apply(
        &mut self,
        cmd: KairosCommand,
        tree_id: Option<&str>,
        lease_epoch: Option<u64>,
    ) -> Result<LoopPhase, KairosDeny> {
        let (next, _effect) = apply_kairos_command(self.state.clone(), cmd, tree_id, lease_epoch)?;
        self.state = next;
        Ok(self.state.phase())
    }
}

/// Resolve ordinary-turn sampler max retries from an optional seal (always 0
/// until durable multi-transport receipts exist).
pub fn ordinary_sampler_max_retries(receipt: Option<&SealedAttemptReceiptV1>) -> u32 {
    ordinary_turn_max_retries(receipt)
}

/// Build a fresh attempt seal for a turn id (preflight clean).
pub fn begin_attempt_seal(attempt_id: impl Into<String>) -> AttemptSealTracker {
    AttemptSealTracker::new(attempt_id)
}

#[cfg(test)]
mod tests {
    use super::*;
    use xai_grok_memory::{mark_output_emitted, may_in_process_retry};

    #[test]
    fn shadow_host_never_grants_authority() {
        let mut host = ShadowAdvisorHost::new(AdvisorMode::Shadow);
        let report = host
            .issue("a1", "run the unit tests before claiming done", None, None)
            .unwrap();
        assert!(!report.applies_authority);
        assert!(!advice_may_mutate_authority(report));
        assert_eq!(host.reports.len(), 1);
    }

    #[test]
    fn kairos_host_claim_freeze_takeover() {
        let mut host = KairosControlHost::new("k1");
        assert_eq!(
            host.apply(KairosCommand::Claim, Some("tree"), Some(1))
                .unwrap(),
            LoopPhase::Running
        );
        assert_eq!(
            host.apply(KairosCommand::Freeze, None, None).unwrap(),
            LoopPhase::Frozen
        );
        assert_eq!(
            host.apply(KairosCommand::TakeOver, Some("tree"), Some(2))
                .unwrap(),
            LoopPhase::Running
        );
    }

    #[test]
    fn ordinary_sampler_budget_is_zero_without_durable_store() {
        assert_eq!(ordinary_sampler_max_retries(None), 0);
        let mut seal = begin_attempt_seal("turn-1");
        assert!(seal.may_retry().is_ok());
        assert_eq!(ordinary_sampler_max_retries(Some(seal.receipt())), 0);
        seal.mark_output();
        assert!(may_in_process_retry(seal.receipt()).is_err());
        assert_eq!(
            ordinary_sampler_max_retries(Some(&mark_output_emitted(
                xai_grok_memory::clean_preflight_receipt("x")
            ))),
            0
        );
    }
}
