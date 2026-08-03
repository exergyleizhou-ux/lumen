//! S8 / P0-NR-A — sealed attempt receipt for in-process retry permission.
//!
//! Provider may be retried only when the attempt is sealed as:
//! `NoOutput + NoToolCall + NotAttempted + NoExternalEffect` (INV-11).
//! Unknown observations fail closed (no retry). This module does not call
//! providers; it is the pure gate the sampler/shell must consult.

use serde::{Deserialize, Serialize};

/// Ternary observation: Unknown never permits retry.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Obs {
    True,
    False,
    Unknown,
}

impl Obs {
    pub fn is_true(self) -> bool {
        matches!(self, Obs::True)
    }
}

/// Sealed attempt surface. All four must be True for in-process retry.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SealedAttemptReceiptV1 {
    pub attempt_id: String,
    pub no_output: Obs,
    pub no_tool_call: Obs,
    pub not_attempted: Obs,
    pub no_external_effect: Obs,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RetryDenyReason {
    OutputEmitted,
    ToolCallEmitted,
    AttemptStarted,
    ExternalEffect,
    ObservationUnknown { field: &'static str },
}

impl RetryDenyReason {
    pub fn code(&self) -> &'static str {
        match self {
            Self::OutputEmitted => "retry.output_emitted",
            Self::ToolCallEmitted => "retry.tool_call_emitted",
            Self::AttemptStarted => "retry.attempt_started",
            Self::ExternalEffect => "retry.external_effect",
            Self::ObservationUnknown { .. } => "retry.observation_unknown",
        }
    }
}

/// Fail-closed: only when every observation is explicitly True.
pub fn may_in_process_retry(receipt: &SealedAttemptReceiptV1) -> Result<(), RetryDenyReason> {
    check_true(receipt.no_output, "no_output", RetryDenyReason::OutputEmitted)?;
    check_true(
        receipt.no_tool_call,
        "no_tool_call",
        RetryDenyReason::ToolCallEmitted,
    )?;
    check_true(
        receipt.not_attempted,
        "not_attempted",
        RetryDenyReason::AttemptStarted,
    )?;
    check_true(
        receipt.no_external_effect,
        "no_external_effect",
        RetryDenyReason::ExternalEffect,
    )?;
    Ok(())
}

fn check_true(
    obs: Obs,
    field: &'static str,
    false_reason: RetryDenyReason,
) -> Result<(), RetryDenyReason> {
    match obs {
        Obs::True => Ok(()),
        Obs::False => Err(false_reason),
        Obs::Unknown => Err(RetryDenyReason::ObservationUnknown { field }),
    }
}

/// Clean pre-attempt seal: nothing ran, safe to start (not a "retry" of a
/// partial attempt — used for first submission and for true no-start failures).
pub fn clean_preflight_receipt(attempt_id: impl Into<String>) -> SealedAttemptReceiptV1 {
    SealedAttemptReceiptV1 {
        attempt_id: attempt_id.into(),
        no_output: Obs::True,
        no_tool_call: Obs::True,
        not_attempted: Obs::True,
        no_external_effect: Obs::True,
    }
}

/// After any model output is observed, retry is forbidden.
pub fn mark_output_emitted(mut r: SealedAttemptReceiptV1) -> SealedAttemptReceiptV1 {
    r.no_output = Obs::False;
    r.not_attempted = Obs::False;
    r
}

pub fn mark_tool_call(mut r: SealedAttemptReceiptV1) -> SealedAttemptReceiptV1 {
    r.no_tool_call = Obs::False;
    r.not_attempted = Obs::False;
    r
}

pub fn mark_attempt_started(mut r: SealedAttemptReceiptV1) -> SealedAttemptReceiptV1 {
    r.not_attempted = Obs::False;
    r
}

pub fn mark_external_effect_unknown(mut r: SealedAttemptReceiptV1) -> SealedAttemptReceiptV1 {
    // Unknown effect alone is enough to fail closed; do not invent other fields.
    r.no_external_effect = Obs::Unknown;
    r
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_fully_clean_seal_permits_retry() {
        let clean = clean_preflight_receipt("a1");
        assert!(may_in_process_retry(&clean).is_ok());

        assert_eq!(
            may_in_process_retry(&mark_output_emitted(clean_preflight_receipt("a2")))
                .unwrap_err(),
            RetryDenyReason::OutputEmitted
        );
        assert_eq!(
            may_in_process_retry(&mark_tool_call(clean_preflight_receipt("a3"))).unwrap_err(),
            RetryDenyReason::ToolCallEmitted
        );
        assert_eq!(
            may_in_process_retry(&mark_attempt_started(clean_preflight_receipt("a4")))
                .unwrap_err(),
            RetryDenyReason::AttemptStarted
        );
        assert_eq!(
            may_in_process_retry(&mark_external_effect_unknown(clean_preflight_receipt(
                "a5"
            )))
            .unwrap_err(),
            RetryDenyReason::ObservationUnknown {
                field: "no_external_effect"
            }
        );
    }

    #[test]
    fn unknown_any_field_fails_closed() {
        let mut r = clean_preflight_receipt("u1");
        r.no_output = Obs::Unknown;
        assert!(matches!(
            may_in_process_retry(&r).unwrap_err(),
            RetryDenyReason::ObservationUnknown { field: "no_output" }
        ));
    }
}
