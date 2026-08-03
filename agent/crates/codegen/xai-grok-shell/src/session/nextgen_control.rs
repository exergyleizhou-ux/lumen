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

/// Escalation guidance appended to terminal sampling failures.
///
/// The first failure keeps the transient-blip framing ("resubmit"). Repeated
/// failures must not read as "stuck": auth failures point at re-login +
/// `/model`, provider failures point at `/model` or retry later, and a
/// user-pinned model is called out explicitly because routing deliberately
/// never overrides a pin (P0-NR-A / S11).
pub fn failure_escalation_guidance(
    consecutive_failures: u32,
    is_auth_failure: bool,
    status_code: Option<u16>,
    model_pinned: bool,
    provider_degraded: bool,
) -> Option<String> {
    if consecutive_failures < 2 {
        return None;
    }
    let mut lines: Vec<String> = Vec::new();
    if is_auth_failure {
        let code = status_code
            .map(|c| c.to_string())
            .unwrap_or_else(|| "401".to_string());
        lines.push(format!(
            "Repeated auth failure ({} x{consecutive_failures}): this is no longer a transient blip; the credential may be invalid.",
            code
        ));
        lines.push(
            "Re-authenticate (grok login or /login) and retry, or switch the model with /model."
                .to_string(),
        );
    } else if provider_degraded {
        lines.push(format!(
            "Repeated failure (x{consecutive_failures}): the provider may be unavailable; health was recorded."
        ));
        lines.push("Switch the model with /model, or retry later.".to_string());
    } else {
        lines.push(format!("Repeated failure (x{consecutive_failures})."));
        lines.push("Switch the model with /model, or retry later.".to_string());
    }
    if model_pinned {
        lines.push(
            "You manually selected this model, so automatic rerouting is disabled to respect your choice; /model switches immediately."
                .to_string(),
        );
    }
    Some(lines.join("\n"))
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

    #[test]
    fn escalation_first_failure_stays_transient() {
        assert_eq!(
            failure_escalation_guidance(0, false, None, false, false),
            None
        );
        assert_eq!(
            failure_escalation_guidance(1, true, Some(401), false, false),
            None,
            "first failure keeps the transient-blip framing"
        );
    }

    #[test]
    fn escalation_repeated_auth_guides_relogin_and_model_switch() {
        let guidance = failure_escalation_guidance(2, true, Some(401), false, false)
            .expect("second auth failure must escalate");
        assert!(guidance.contains("401"), "guidance: {guidance}");
        assert!(guidance.contains("grok login"), "guidance: {guidance}");
        assert!(guidance.contains("/model"), "guidance: {guidance}");
        assert!(!guidance.contains("manually selected"));
    }

    #[test]
    fn escalation_pinned_model_is_called_out() {
        let guidance = failure_escalation_guidance(3, true, Some(401), true, false)
            .expect("must escalate");
        assert!(
            guidance.contains("manually selected"),
            "pin must be visible: {guidance}"
        );
        assert!(guidance.contains("/model switches immediately"));
    }

    #[test]
    fn escalation_provider_failure_mentions_health() {
        let guidance = failure_escalation_guidance(2, false, Some(503), false, true)
            .expect("must escalate");
        assert!(guidance.contains("provider"), "guidance: {guidance}");
        assert!(guidance.contains("health was recorded"), "guidance: {guidance}");
        assert!(!guidance.contains("credential"));
    }
}
