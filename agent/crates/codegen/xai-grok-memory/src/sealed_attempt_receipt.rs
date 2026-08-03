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

/// Policy for ordinary sampler turns (INV-11 / P0-NR-A).
///
/// Until a **durable** per-attempt receipt is the sole authority across every
/// transport path, the in-process budget stays at zero even when a clean
/// preflight seal is presented. Callers must still consult
/// [`may_in_process_retry`] before any future non-zero budget path.
pub fn ordinary_turn_max_retries(receipt: Option<&SealedAttemptReceiptV1>) -> u32 {
    match receipt {
        Some(r) => {
            // Keep the gate exercised so a partial seal never silently becomes
            // a retry budget; clean seals still map to 0 until durable store.
            let _ = may_in_process_retry(r);
            0
        }
        None => 0,
    }
}

/// In-memory seal builder for a single attempt (shell/sampler wiring).
#[derive(Debug, Clone)]
pub struct AttemptSealTracker {
    receipt: SealedAttemptReceiptV1,
}

impl AttemptSealTracker {
    pub fn new(attempt_id: impl Into<String>) -> Self {
        Self {
            receipt: clean_preflight_receipt(attempt_id),
        }
    }

    pub fn receipt(&self) -> &SealedAttemptReceiptV1 {
        &self.receipt
    }

    pub fn mark_output(&mut self) {
        self.receipt = mark_output_emitted(self.receipt.clone());
    }

    pub fn mark_tool(&mut self) {
        self.receipt = mark_tool_call(self.receipt.clone());
    }

    pub fn mark_started(&mut self) {
        self.receipt = mark_attempt_started(self.receipt.clone());
    }

    pub fn mark_effect_unknown(&mut self) {
        self.receipt = mark_external_effect_unknown(self.receipt.clone());
    }

    pub fn may_retry(&self) -> Result<(), RetryDenyReason> {
        may_in_process_retry(&self.receipt)
    }

    pub fn max_retries(&self) -> u32 {
        ordinary_turn_max_retries(Some(&self.receipt))
    }
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

    #[test]
    fn ordinary_turn_budget_stays_zero_with_or_without_clean_seal() {
        assert_eq!(ordinary_turn_max_retries(None), 0);
        assert_eq!(
            ordinary_turn_max_retries(Some(&clean_preflight_receipt("c"))),
            0
        );
        let mut t = AttemptSealTracker::new("t1");
        assert!(t.may_retry().is_ok());
        assert_eq!(t.max_retries(), 0);
        t.mark_output();
        assert!(t.may_retry().is_err());
        assert_eq!(t.max_retries(), 0);
    }
}
