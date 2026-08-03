//! S8 / P0-NR-A — sealed attempt receipt for in-process retry permission.
//!
//! Provider may be retried only when the attempt is sealed as:
//! `NoOutput + NoToolCall + NotAttempted + NoExternalEffect` (INV-11).
//! Unknown observations fail closed (no retry). This module does not call
//! providers; it is the pure gate the sampler/shell must consult.
//!
//! S8 closure adds a **durable** per-attempt receipt store and a P4b-style
//! admission gate. A clean in-memory seal alone does not grant budget: the
//! store must hold a matching clean receipt and policy side-conditions
//! (pin / pool / breaker / schema / advice) must all pass. `GROK_MAX_RETRIES`
//! and other actor ceilings may only *lower* the seal budget (INV-11).

use std::collections::BTreeMap;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};

/// Wire schema for durable sealed-attempt snapshots. Mismatch fails closed.
pub const SEALED_RECEIPT_SCHEMA_VERSION: u16 = 1;

/// Hard safety ceiling for in-process retries after a durable clean seal.
/// Auth-refresh is the only consumer reopened by S8; transport-level sampler
/// policy remains `NO_RECEIPT_MAX_RETRIES=0` unless admission explicitly
/// grants a non-zero budget for a new attempt id.
pub const DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES: u32 = 1;

/// Bounded store cap (DEBT-005): refuse new records past this instead of
/// growing the JSON snapshot without limit.
pub const SEAL_RECORDS_MAX: usize = 4096;

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

/// Why in-process retry / budget was denied (INV-11 + P4b side conditions).
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RetryDenyReason {
    OutputEmitted,
    ToolCallEmitted,
    AttemptStarted,
    ExternalEffect,
    ObservationUnknown { field: &'static str },
    /// `/model` pin disables ordinary-turn auto budget (design).
    ModelPinned,
    /// User pool has no healthy candidate left.
    PoolExhausted,
    /// Provider circuit breaker is open for the endpoint.
    BreakerOpen,
    /// Durable snapshot schema does not match this binary.
    SchemaMismatch,
    /// Advisor advice is stale relative to the current admission context.
    StaleAdvice,
    /// No durable receipt store, unhealthy journal, or missing record.
    DurableStoreUntrusted,
    /// Already consumed the bounded retry budget for this attempt lineage.
    BudgetExhausted,
    /// Caller presented no receipt at all.
    NoReceipt,
}

impl RetryDenyReason {
    pub fn code(&self) -> &'static str {
        match self {
            Self::OutputEmitted => "retry.output_emitted",
            Self::ToolCallEmitted => "retry.tool_call_emitted",
            Self::AttemptStarted => "retry.attempt_started",
            Self::ExternalEffect => "retry.external_effect",
            Self::ObservationUnknown { .. } => "retry.observation_unknown",
            Self::ModelPinned => "retry.model_pinned",
            Self::PoolExhausted => "retry.pool_exhausted",
            Self::BreakerOpen => "retry.breaker_open",
            Self::SchemaMismatch => "retry.schema_mismatch",
            Self::StaleAdvice => "retry.stale_advice",
            Self::DurableStoreUntrusted => "retry.durable_store_untrusted",
            Self::BudgetExhausted => "retry.budget_exhausted",
            Self::NoReceipt => "retry.no_receipt",
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
///
/// DEBT-004 semantics: a *clean failure* after the request was dispatched to
/// the provider still carries `not_attempted: True` in this schema — the
/// field means "no attempt state was *observed* (no output/tool/effect
/// evidence)", not "no request was sent". Retry budget is bounded (max 1) so
/// a clean-sealed resubmit can at most double a request that may have been
/// billed once; the durable store records the attempt_id so replay is
/// trackable. Do not reinterpret `not_attempted` as a billing guarantee.
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

/// Whether a durable store confirms the seal as the sole retry authority.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DurableSealAuthority {
    /// No store, no record, or store not consulted.
    Absent,
    /// Store is healthy and holds a byte-matching clean seal for this attempt.
    ConfirmedClean,
    /// Store unhealthy, schema mismatch, or receipt diverges — fail closed.
    Untrusted,
}

/// Policy for ordinary sampler turns (INV-11 / P0-NR-A / S8).
///
/// Without durable confirmation a clean in-memory seal still maps to **0**.
/// Only [`DurableSealAuthority::ConfirmedClean`] may raise the budget, and
/// only up to [`DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES`].
pub fn ordinary_turn_max_retries(receipt: Option<&SealedAttemptReceiptV1>) -> u32 {
    ordinary_turn_max_retries_with_authority(receipt, DurableSealAuthority::Absent)
}

/// Resolve ordinary-turn retry budget from seal + durable authority.
pub fn ordinary_turn_max_retries_with_authority(
    receipt: Option<&SealedAttemptReceiptV1>,
    authority: DurableSealAuthority,
) -> u32 {
    match (receipt, authority) {
        (Some(r), DurableSealAuthority::ConfirmedClean) if may_in_process_retry(r).is_ok() => {
            DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES
        }
        _ => 0,
    }
}

/// P4b admission inputs for granting a non-zero in-process retry budget.
#[derive(Debug, Clone)]
pub struct RetryAdmissionRequest<'a> {
    pub receipt: Option<&'a SealedAttemptReceiptV1>,
    pub durable_authority: DurableSealAuthority,
    /// Schema version on the durable record (or caller's claim).
    pub schema_version: u16,
    pub expected_schema_version: u16,
    /// User `/model` pin — ordinary-turn auto budget stays closed.
    pub model_pinned: bool,
    /// No healthy model left in the user pool.
    pub pool_exhausted: bool,
    /// Circuit breaker open for the active provider endpoint.
    pub breaker_open: bool,
    /// Advisor advice no longer matches live catalog/health/policy.
    pub stale_advice: bool,
    /// Actor / env ceiling (e.g. `GROK_MAX_RETRIES`). May only **lower**.
    pub actor_policy_max_retries: u32,
    /// Retries already consumed for this turn lineage.
    pub already_used_retries: u32,
}

/// Authorize a bounded in-process retry budget. Fail-closed on any deny path.
///
/// Returns the effective `max_retries` (remaining) for the caller. Never
/// exceeds [`DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES`], and never exceeds
/// `actor_policy_max_retries` (so env/config cannot reopen safety closure).
pub fn authorize_in_process_retry_budget(
    req: &RetryAdmissionRequest<'_>,
) -> Result<u32, RetryDenyReason> {
    if req.schema_version != req.expected_schema_version {
        return Err(RetryDenyReason::SchemaMismatch);
    }
    if req.model_pinned {
        return Err(RetryDenyReason::ModelPinned);
    }
    if req.pool_exhausted {
        return Err(RetryDenyReason::PoolExhausted);
    }
    if req.breaker_open {
        return Err(RetryDenyReason::BreakerOpen);
    }
    if req.stale_advice {
        return Err(RetryDenyReason::StaleAdvice);
    }
    let Some(receipt) = req.receipt else {
        return Err(RetryDenyReason::NoReceipt);
    };
    may_in_process_retry(receipt)?;
    match req.durable_authority {
        DurableSealAuthority::ConfirmedClean => {}
        DurableSealAuthority::Absent | DurableSealAuthority::Untrusted => {
            return Err(RetryDenyReason::DurableStoreUntrusted);
        }
    }
    let seal_budget = ordinary_turn_max_retries_with_authority(
        Some(receipt),
        DurableSealAuthority::ConfirmedClean,
    );
    // Actor policy is a ceiling, never a raise: min(seal, actor).
    let capped = seal_budget.min(req.actor_policy_max_retries);
    if capped == 0 || req.already_used_retries >= capped {
        return Err(RetryDenyReason::BudgetExhausted);
    }
    Ok(capped.saturating_sub(req.already_used_retries))
}

/// Effective max retries when combining seal budget with an actor/env policy.
///
/// Unlike [`authorize_in_process_retry_budget`], this pure helper does not
/// require full P4b side-conditions; it only enforces INV-11's rule that
/// env/config cannot raise the seal budget.
pub fn effective_retry_budget(seal_budget: u32, actor_policy_max_retries: u32) -> u32 {
    seal_budget.min(actor_policy_max_retries)
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

    /// Consume the tracker, returning the final sealed receipt. Wired call
    /// sites use `receipt()`; this exists for durable-store writes that need
    /// ownership (DEBT-006: previously dead, now used by store writers).
    pub fn into_receipt(self) -> SealedAttemptReceiptV1 {
        self.receipt
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

    /// Apply fail-closed observations from a completed attempt.
    ///
    /// `had_output` / `had_tool_call` are positive proof of partial progress.
    /// When `observation_complete` is false (e.g. stream drain race), external
    /// effect is marked Unknown so retry fails closed.
    pub fn apply_failure_observations(
        &mut self,
        had_output: bool,
        had_tool_call: bool,
        observation_complete: bool,
    ) {
        if had_output {
            self.mark_output();
        }
        if had_tool_call {
            self.mark_tool();
        }
        if !observation_complete {
            self.mark_effect_unknown();
        }
    }

    pub fn may_retry(&self) -> Result<(), RetryDenyReason> {
        may_in_process_retry(&self.receipt)
    }

    pub fn max_retries(&self) -> u32 {
        ordinary_turn_max_retries(Some(&self.receipt))
    }

    pub fn max_retries_with_authority(&self, authority: DurableSealAuthority) -> u32 {
        ordinary_turn_max_retries_with_authority(Some(&self.receipt), authority)
    }
}

// ---------------------------------------------------------------------------
// Durable store (JSON snapshot, GovernedOperationStore pattern)
// ---------------------------------------------------------------------------

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

/// One durable seal record (per attempt id).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SealedAttemptReceiptRecord {
    pub schema_version: u16,
    pub attempt_id: String,
    pub receipt: SealedAttemptReceiptV1,
    pub sealed_at_unix: u64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub turn_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub session_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct SealedReceiptStoreSnapshot {
    schema_version: u16,
    records: Vec<SealedAttemptReceiptRecord>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SealedReceiptStoreError {
    Persistence(String),
    SchemaMismatch { found: u16, expected: u16 },
    Unhealthy,
    NotFound { attempt_id: String },
}

impl std::fmt::Display for SealedReceiptStoreError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Persistence(m) => write!(f, "sealed receipt persistence: {m}"),
            Self::SchemaMismatch { found, expected } => {
                write!(
                    f,
                    "sealed receipt schema mismatch: found {found}, expected {expected}"
                )
            }
            Self::Unhealthy => write!(f, "sealed receipt store is unhealthy"),
            Self::NotFound { attempt_id } => {
                write!(f, "sealed receipt not found for attempt {attempt_id:?}")
            }
        }
    }
}

impl std::error::Error for SealedReceiptStoreError {}

/// Root-owned durable registry of sealed attempt receipts.
///
/// Persistence path is optional for pure unit tests; when set, a JSON snapshot
/// is rewritten after each mutation (atomic rename). A poisoned store never
/// silently falls back to memory for authorization decisions.
#[derive(Debug, Clone)]
pub struct SealedAttemptReceiptStore {
    path: Option<PathBuf>,
    healthy: Arc<Mutex<bool>>,
    records: Arc<Mutex<BTreeMap<String, SealedAttemptReceiptRecord>>>,
}

impl SealedAttemptReceiptStore {
    /// Ephemeral store (tests / offline gates). Healthy until a write fails.
    pub fn in_memory() -> Self {
        Self {
            path: None,
            healthy: Arc::new(Mutex::new(true)),
            records: Arc::new(Mutex::new(BTreeMap::new())),
        }
    }

    /// Load-or-create a durable store at `path`. Corrupt/unsupported schema → unhealthy.
    pub fn with_path(path: impl Into<PathBuf>) -> Self {
        let path = path.into();
        let store = Self {
            path: Some(path.clone()),
            healthy: Arc::new(Mutex::new(true)),
            records: Arc::new(Mutex::new(BTreeMap::new())),
        };
        match std::fs::read(&path) {
            Ok(bytes) => match decode_snapshot(&bytes) {
                Ok(records) => {
                    let mut map = store.records.lock().expect("seal store lock");
                    for rec in records {
                        if rec.schema_version != SEALED_RECEIPT_SCHEMA_VERSION
                            || rec.attempt_id != rec.receipt.attempt_id
                            || map.insert(rec.attempt_id.clone(), rec).is_some()
                        {
                            store.mark_unhealthy();
                            break;
                        }
                    }
                }
                Err(_) => store.mark_unhealthy(),
            },
            Err(error) if error.kind() == ErrorKind::NotFound => {}
            Err(_) => store.mark_unhealthy(),
        }
        store
    }

    pub fn is_healthy(&self) -> bool {
        *self.healthy.lock().expect("seal health lock")
    }

    pub fn path(&self) -> Option<&Path> {
        self.path.as_deref()
    }

    /// Persist a sealed receipt. Idempotent when the stored receipt matches.
    pub fn record(
        &self,
        receipt: SealedAttemptReceiptV1,
        turn_id: Option<String>,
        session_id: Option<String>,
    ) -> Result<SealedAttemptReceiptRecord, SealedReceiptStoreError> {
        self.ensure_healthy()?;
        if receipt.attempt_id.trim().is_empty() {
            return Err(SealedReceiptStoreError::Persistence(
                "attempt_id must not be empty".into(),
            ));
        }
        let rec = SealedAttemptReceiptRecord {
            schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            attempt_id: receipt.attempt_id.clone(),
            receipt,
            sealed_at_unix: now_unix(),
            turn_id,
            session_id,
        };
        let mut map = self.records.lock().expect("seal store lock");
        if let Some(existing) = map.get(&rec.attempt_id) {
            if existing.receipt != rec.receipt {
                return Err(SealedReceiptStoreError::Persistence(format!(
                    "attempt {} already sealed with a different receipt",
                    rec.attempt_id
                )));
            }
            return Ok(existing.clone());
        }
        // DEBT-005: bounded store — refuse new records past the cap instead
        // of growing the snapshot without limit (fail-closed; the caller sees
        // a Persistence error and treats the store as unhealthy).
        if map.len() >= SEAL_RECORDS_MAX {
            return Err(SealedReceiptStoreError::Persistence(format!(
                "sealed receipt store at capacity ({SEAL_RECORDS_MAX} records)"
            )));
        }
        map.insert(rec.attempt_id.clone(), rec.clone());
        if let Err(error) = self.persist(&map) {
            map.remove(&rec.attempt_id);
            self.mark_unhealthy();
            return Err(error);
        }
        Ok(rec)
    }

    pub fn get(
        &self,
        attempt_id: &str,
    ) -> Result<SealedAttemptReceiptRecord, SealedReceiptStoreError> {
        self.ensure_healthy()?;
        self.records
            .lock()
            .expect("seal store lock")
            .get(attempt_id)
            .cloned()
            .ok_or_else(|| SealedReceiptStoreError::NotFound {
                attempt_id: attempt_id.to_owned(),
            })
    }

    /// Resolve durable authority for a live seal (must match stored record).
    pub fn authority_for(&self, receipt: &SealedAttemptReceiptV1) -> DurableSealAuthority {
        if !self.is_healthy() {
            return DurableSealAuthority::Untrusted;
        }
        let Ok(stored) = self.get(&receipt.attempt_id) else {
            return DurableSealAuthority::Absent;
        };
        if stored.schema_version != SEALED_RECEIPT_SCHEMA_VERSION {
            return DurableSealAuthority::Untrusted;
        }
        if stored.receipt != *receipt {
            return DurableSealAuthority::Untrusted;
        }
        if may_in_process_retry(&stored.receipt).is_ok() {
            DurableSealAuthority::ConfirmedClean
        } else {
            // Durable dirty seal is authoritative: still "confirmed" as dirty,
            // but ordinary_turn_max_retries_with_authority only raises on clean.
            // Surface Untrusted is wrong; Absent would let callers invent a
            // clean seal. Use Untrusted only for corruption; dirty match is
            // simply not ConfirmedClean → budget 0 via Absent-like path.
            DurableSealAuthority::Absent
        }
    }

    fn ensure_healthy(&self) -> Result<(), SealedReceiptStoreError> {
        if self.is_healthy() {
            Ok(())
        } else {
            Err(SealedReceiptStoreError::Unhealthy)
        }
    }

    fn mark_unhealthy(&self) {
        *self.healthy.lock().expect("seal health lock") = false;
    }

    fn persist(
        &self,
        records: &BTreeMap<String, SealedAttemptReceiptRecord>,
    ) -> Result<(), SealedReceiptStoreError> {
        let Some(path) = &self.path else {
            return Ok(());
        };
        let parent = path.parent().ok_or_else(|| {
            SealedReceiptStoreError::Persistence("seal path has no parent".into())
        })?;
        std::fs::create_dir_all(parent)
            .map_err(|e| SealedReceiptStoreError::Persistence(e.to_string()))?;
        let snapshot = SealedReceiptStoreSnapshot {
            schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            records: records.values().cloned().collect(),
        };
        let bytes = serde_json::to_vec_pretty(&snapshot)
            .map_err(|e| SealedReceiptStoreError::Persistence(e.to_string()))?;
        let mut temp = tempfile::NamedTempFile::new_in(parent)
            .map_err(|e| SealedReceiptStoreError::Persistence(e.to_string()))?;
        use std::io::Write;
        temp.write_all(&bytes)
            .map_err(|e| SealedReceiptStoreError::Persistence(e.to_string()))?;
        temp.as_file()
            .sync_all()
            .map_err(|e| SealedReceiptStoreError::Persistence(e.to_string()))?;
        temp.persist(path)
            .map_err(|e| SealedReceiptStoreError::Persistence(e.error.to_string()))?;
        Ok(())
    }
}

fn decode_snapshot(bytes: &[u8]) -> Result<Vec<SealedAttemptReceiptRecord>, String> {
    let snapshot: SealedReceiptStoreSnapshot =
        serde_json::from_slice(bytes).map_err(|e| e.to_string())?;
    if snapshot.schema_version != SEALED_RECEIPT_SCHEMA_VERSION {
        return Err(format!(
            "unsupported sealed receipt schema_version {}",
            snapshot.schema_version
        ));
    }
    Ok(snapshot.records)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn admission_ok(receipt: &SealedAttemptReceiptV1) -> RetryAdmissionRequest<'_> {
        RetryAdmissionRequest {
            receipt: Some(receipt),
            durable_authority: DurableSealAuthority::ConfirmedClean,
            schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            expected_schema_version: SEALED_RECEIPT_SCHEMA_VERSION,
            model_pinned: false,
            pool_exhausted: false,
            breaker_open: false,
            stale_advice: false,
            actor_policy_max_retries: 15,
            already_used_retries: 0,
        }
    }

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
    fn ordinary_turn_budget_stays_zero_without_durable_authority() {
        assert_eq!(ordinary_turn_max_retries(None), 0);
        assert_eq!(
            ordinary_turn_max_retries(Some(&clean_preflight_receipt("c"))),
            0,
            "clean in-memory seal alone must not raise budget"
        );
        let mut t = AttemptSealTracker::new("t1");
        assert!(t.may_retry().is_ok());
        assert_eq!(t.max_retries(), 0);
        t.mark_output();
        assert!(t.may_retry().is_err());
        assert_eq!(t.max_retries(), 0);
    }

    #[test]
    fn ordinary_turn_budget_opens_only_for_durable_clean_seal() {
        let clean = clean_preflight_receipt("d1");
        assert_eq!(
            ordinary_turn_max_retries_with_authority(
                Some(&clean),
                DurableSealAuthority::ConfirmedClean
            ),
            DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES
        );
        assert_eq!(
            ordinary_turn_max_retries_with_authority(
                Some(&clean),
                DurableSealAuthority::Untrusted
            ),
            0
        );
        let dirty = mark_output_emitted(clean_preflight_receipt("d2"));
        assert_eq!(
            ordinary_turn_max_retries_with_authority(
                Some(&dirty),
                DurableSealAuthority::ConfirmedClean
            ),
            0,
            "existing output never gets budget even with durable store"
        );
    }

    #[test]
    fn admission_denies_pin_pool_breaker_schema_stale_advice_and_existing_output() {
        let clean = clean_preflight_receipt("pol");

        let mut req = admission_ok(&clean);
        req.model_pinned = true;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::ModelPinned
        );

        req = admission_ok(&clean);
        req.pool_exhausted = true;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::PoolExhausted
        );

        req = admission_ok(&clean);
        req.breaker_open = true;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::BreakerOpen
        );

        req = admission_ok(&clean);
        req.schema_version = 99;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::SchemaMismatch
        );

        req = admission_ok(&clean);
        req.stale_advice = true;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::StaleAdvice
        );

        let dirty = mark_output_emitted(clean_preflight_receipt("out"));
        req = admission_ok(&dirty);
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::OutputEmitted
        );
    }

    #[test]
    fn admission_denies_absent_store_and_no_receipt() {
        let clean = clean_preflight_receipt("abs");
        let mut req = admission_ok(&clean);
        req.durable_authority = DurableSealAuthority::Absent;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::DurableStoreUntrusted
        );
        req.durable_authority = DurableSealAuthority::Untrusted;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::DurableStoreUntrusted
        );
        req = admission_ok(&clean);
        req.receipt = None;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::NoReceipt
        );
    }

    #[test]
    fn clean_durable_seal_grants_bounded_budget_and_cap_applies() {
        let clean = clean_preflight_receipt("ok");
        let remaining = authorize_in_process_retry_budget(&admission_ok(&clean)).unwrap();
        assert_eq!(remaining, DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES);

        let mut req = admission_ok(&clean);
        req.already_used_retries = DURABLE_CLEAN_MAX_IN_PROCESS_RETRIES;
        assert_eq!(
            authorize_in_process_retry_budget(&req).unwrap_err(),
            RetryDenyReason::BudgetExhausted
        );
    }

    #[test]
    fn grok_max_retries_cannot_reopen_safety_closure() {
        let clean = clean_preflight_receipt("env");
        // Actor claims 15 (GROK_MAX_RETRIES default territory) but seal budget is 1.
        let remaining = authorize_in_process_retry_budget(&admission_ok(&clean)).unwrap();
        assert_eq!(remaining, 1);
        assert_eq!(effective_retry_budget(0, 15), 0);
        assert_eq!(effective_retry_budget(1, 15), 1);
        assert_eq!(effective_retry_budget(1, 0), 0, "actor may only lower");

        // Dirty seal: even actor_policy=15 yields deny / zero.
        let dirty = mark_output_emitted(clean_preflight_receipt("env2"));
        let mut req = admission_ok(&dirty);
        req.actor_policy_max_retries = 15;
        assert!(authorize_in_process_retry_budget(&req).is_err());
        assert_eq!(
            ordinary_turn_max_retries_with_authority(
                Some(&dirty),
                DurableSealAuthority::ConfirmedClean
            ),
            0
        );
        assert_eq!(effective_retry_budget(0, 15), 0);
    }

    #[test]
    fn durable_store_persists_and_confirms_clean_seal() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("sealed-attempts.json");
        let store = SealedAttemptReceiptStore::with_path(&path);
        assert!(store.is_healthy());

        let clean = clean_preflight_receipt("att-1");
        store
            .record(clean.clone(), Some("turn-1".into()), Some("sess".into()))
            .unwrap();
        assert_eq!(
            store.authority_for(&clean),
            DurableSealAuthority::ConfirmedClean
        );
        assert_eq!(
            ordinary_turn_max_retries_with_authority(
                Some(&clean),
                store.authority_for(&clean)
            ),
            1
        );

        // Recover from disk.
        let reloaded = SealedAttemptReceiptStore::with_path(&path);
        assert!(reloaded.is_healthy());
        let got = reloaded.get("att-1").unwrap();
        assert_eq!(got.receipt, clean);
        assert_eq!(
            reloaded.authority_for(&clean),
            DurableSealAuthority::ConfirmedClean
        );

        // Dirty seal stores but does not open budget.
        let dirty = mark_output_emitted(clean_preflight_receipt("att-2"));
        store.record(dirty.clone(), None, None).unwrap();
        assert_eq!(store.authority_for(&dirty), DurableSealAuthority::Absent);
        assert_eq!(
            ordinary_turn_max_retries_with_authority(
                Some(&dirty),
                store.authority_for(&dirty)
            ),
            0
        );
    }

    #[test]
    fn durable_store_schema_mismatch_marks_untrusted() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("bad-schema.json");
        let bad = serde_json::json!({
            "schema_version": 99,
            "records": []
        });
        std::fs::write(&path, serde_json::to_vec(&bad).unwrap()).unwrap();
        let store = SealedAttemptReceiptStore::with_path(&path);
        assert!(!store.is_healthy());
        let clean = clean_preflight_receipt("x");
        assert_eq!(
            store.authority_for(&clean),
            DurableSealAuthority::Untrusted
        );
    }

    #[test]
    fn tracker_failure_observations_mark_output_and_unknown_effect() {
        let mut t = AttemptSealTracker::new("obs");
        t.apply_failure_observations(true, false, true);
        assert_eq!(t.may_retry().unwrap_err(), RetryDenyReason::OutputEmitted);

        let mut t2 = AttemptSealTracker::new("obs2");
        t2.apply_failure_observations(false, false, false);
        assert!(matches!(
            t2.may_retry().unwrap_err(),
            RetryDenyReason::ObservationUnknown {
                field: "no_external_effect"
            }
        ));

        let mut t3 = AttemptSealTracker::new("obs3");
        t3.apply_failure_observations(false, false, true);
        assert!(t3.may_retry().is_ok());
    }

    #[test]
    fn deny_reason_codes_are_stable() {
        assert_eq!(RetryDenyReason::ModelPinned.code(), "retry.model_pinned");
        assert_eq!(
            RetryDenyReason::PoolExhausted.code(),
            "retry.pool_exhausted"
        );
        assert_eq!(RetryDenyReason::BreakerOpen.code(), "retry.breaker_open");
        assert_eq!(
            RetryDenyReason::SchemaMismatch.code(),
            "retry.schema_mismatch"
        );
        assert_eq!(RetryDenyReason::StaleAdvice.code(), "retry.stale_advice");
        assert_eq!(
            RetryDenyReason::OutputEmitted.code(),
            "retry.output_emitted"
        );
    }
}
