//! S9 / NG-06A — ClientAdvisor shadow-only pure contract.
//!
//! Advisor may produce structured advice; it cannot accept claims, change
//! assignment, or declare completion (INV-1/2).

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AdvisorMode {
    Off,
    Shadow,
    // Active consult is a later slice; issue path stays Shadow until wired.
    Consult,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AdviceReportV1 {
    pub advice_id: String,
    pub mode: AdvisorMode,
    pub summary: String,
    pub recommended_next_step: Option<String>,
    pub usage_receipt_ref: Option<String>,
    /// Always false for shadow: advice is never an authority transition.
    pub applies_authority: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AdvisorDeny {
    ModeOff,
    EmptySummary,
    AuthorityClaim,
    SecretLike,
    Oversize,
}

impl AdvisorDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::ModeOff => "advisor.mode_off",
            Self::EmptySummary => "advisor.empty_summary",
            Self::AuthorityClaim => "advisor.authority_claim",
            Self::SecretLike => "advisor.secret_like",
            Self::Oversize => "advisor.oversize",
        }
    }
}

const MAX_SUMMARY: usize = 2_000;

/// Build a shadow advice report. `applies_authority` is forced false.
pub fn issue_shadow_advice(
    mode: AdvisorMode,
    advice_id: impl Into<String>,
    summary: impl Into<String>,
    recommended_next_step: Option<String>,
    usage_receipt_ref: Option<String>,
) -> Result<AdviceReportV1, AdvisorDeny> {
    if matches!(mode, AdvisorMode::Off) {
        return Err(AdvisorDeny::ModeOff);
    }
    let summary = summary.into();
    if summary.trim().is_empty() {
        return Err(AdvisorDeny::EmptySummary);
    }
    if summary.len() > MAX_SUMMARY {
        return Err(AdvisorDeny::Oversize);
    }
    let lower = summary.to_ascii_lowercase();
    if lower.contains("api_key=") || lower.contains("-----begin ") {
        return Err(AdvisorDeny::SecretLike);
    }
    if lower.contains("i accept this claim")
        || lower.contains("mark as completed")
        || lower.contains("assignment applied")
    {
        return Err(AdvisorDeny::AuthorityClaim);
    }
    Ok(AdviceReportV1 {
        advice_id: advice_id.into(),
        mode: AdvisorMode::Shadow,
        summary,
        recommended_next_step,
        usage_receipt_ref,
        applies_authority: false,
    })
}

/// Actor gate: advice never becomes Accepted or Applied.
pub fn advice_may_mutate_authority(report: &AdviceReportV1) -> bool {
    report.applies_authority
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shadow_advice_never_has_authority() {
        let r = issue_shadow_advice(
            AdvisorMode::Shadow,
            "a1",
            "consider running tests",
            Some("run tests".into()),
            Some("usage://1".into()),
        )
        .unwrap();
        assert!(!r.applies_authority);
        assert!(!advice_may_mutate_authority(&r));
        assert_eq!(r.mode, AdvisorMode::Shadow);
    }

    #[test]
    fn rejects_off_empty_secret_and_authority_prose() {
        assert_eq!(
            issue_shadow_advice(AdvisorMode::Off, "a", "x", None, None).unwrap_err(),
            AdvisorDeny::ModeOff
        );
        assert_eq!(
            issue_shadow_advice(AdvisorMode::Shadow, "a", "  ", None, None).unwrap_err(),
            AdvisorDeny::EmptySummary
        );
        assert_eq!(
            issue_shadow_advice(AdvisorMode::Shadow, "a", "api_key=sk-xxx", None, None)
                .unwrap_err(),
            AdvisorDeny::SecretLike
        );
        assert_eq!(
            issue_shadow_advice(
                AdvisorMode::Shadow,
                "a",
                "I accept this claim as truth",
                None,
                None
            )
            .unwrap_err(),
            AdvisorDeny::AuthorityClaim
        );
    }
}
