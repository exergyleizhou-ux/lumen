//! NG-02A — ToolContractV1 / ToolResultEnvelopeV1.
//!
//! The frozen contract for "every callable tool has a capability, scope,
//! result artifact and context budget". Pure DTO layer: no dispatch wiring
//! yet. Contract identity is a canonical hash via NG-00 (`CanonicalRecord`),
//! so the same tool with the same policy always hashes identically and any
//! policy drift changes the hash.
//!
//! Fail-closed admission semantics (mirrors `apply_child_tool_policy`):
//! a child/daemon surface may only run tools whose contract is *known*:
//! classified kind (not `Other`), pinned input schema hash, and an explicit
//! result policy. Missing any of these denies the tool for children; the
//! root interactive session may still approve explicitly.

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue, ENCODING_REVISION};
use sha2::{Digest, Sha256};
use xai_grok_tools::types::tool::ToolKind;

/// Schema revision of the contract itself (independent of the encoding
/// revision, which is part of the preimage).
pub const TOOL_CONTRACT_SCHEMA_VERSION: u16 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum OperationClass {
    ReadOnly,
    ReversibleWrite,
    ExternalEffect,
}

impl OperationClass {
    fn as_str(self) -> &'static str {
        match self {
            OperationClass::ReadOnly => "read-only",
            OperationClass::ReversibleWrite => "reversible-write",
            OperationClass::ExternalEffect => "external-effect",
        }
    }
}

/// Replay policy. `NeverReplay` tools must never be re-submitted after an
/// unknown outcome; `IdempotentWithReceipt` may resume only with a receipt;
/// `ReadOnlyRetryable` is safe to re-run (no side effects).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ToolIdempotencyClass {
    NeverReplay,
    IdempotentWithReceipt,
    ReadOnlyRetryable,
}

impl ToolIdempotencyClass {
    fn as_str(self) -> &'static str {
        match self {
            ToolIdempotencyClass::NeverReplay => "never-replay",
            ToolIdempotencyClass::IdempotentWithReceipt => "idempotent-with-receipt",
            ToolIdempotencyClass::ReadOnlyRetryable => "read-only-retryable",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ArtifactClass {
    Public,
    WorkspacePrivate,
    Credential,
    SensitiveArtifact,
}

impl ArtifactClass {
    fn as_str(self) -> &'static str {
        match self {
            ArtifactClass::Public => "public",
            ArtifactClass::WorkspacePrivate => "workspace-private",
            ArtifactClass::Credential => "credential",
            ArtifactClass::SensitiveArtifact => "sensitive-artifact",
        }
    }
}

/// Result handling contract: bounded redacted preview, full output goes to an
/// artifact reference, never into the model context un-bounded.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToolResultPolicyV1 {
    pub preview_byte_limit: u32,
    pub artifact_class: ArtifactClass,
}

/// A single tool's frozen contract. `encoding_revision` is part of the
/// preimage, so a re-encode is an explicit migration event.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToolContractV1 {
    pub schema_version: u16,
    pub namespace: String,
    pub tool_name: String,
    pub tool_kind: ToolKind,
    pub operation_class: OperationClass,
    pub input_schema_hash: Option<String>,
    pub result_policy: Option<ToolResultPolicyV1>,
    pub idempotency_class: ToolIdempotencyClass,
    pub provider_or_endpoint_ref: Option<String>,
    pub policy_revision: u64,
    pub encoding_revision: u32,
}

impl ToolContractV1 {
    pub fn canonical_bytes(&self) -> Result<Vec<u8>, CanonicalError> {
        let record = CanonicalRecord::new("tool-contract")
            .field("schema_version", CanonicalValue::U64(u64::from(self.schema_version)))
            .field("namespace", CanonicalValue::str(&self.namespace))
            .field("tool_name", CanonicalValue::str(&self.tool_name))
            .field(
                "tool_kind",
                CanonicalValue::str(self.tool_kind.as_key()),
            )
            .field("operation_class", CanonicalValue::str(self.operation_class.as_str()))
            .field(
                "input_schema_hash",
                self.input_schema_hash
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field(
                "result_policy",
                match &self.result_policy {
                    Some(policy) => CanonicalValue::Map(vec![
                        (
                            "artifact_class".to_owned(),
                            CanonicalValue::str(policy.artifact_class.as_str()),
                        ),
                        (
                            "preview_byte_limit".to_owned(),
                            CanonicalValue::U64(u64::from(policy.preview_byte_limit)),
                        ),
                    ]),
                    None => CanonicalValue::Null,
                },
            )
            .field(
                "idempotency_class",
                CanonicalValue::str(self.idempotency_class.as_str()),
            )
            .field(
                "provider_or_endpoint_ref",
                self.provider_or_endpoint_ref
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field("policy_revision", CanonicalValue::U64(self.policy_revision))
            .field("encoding_revision", CanonicalValue::U64(u64::from(self.encoding_revision)));
        record.canonical_bytes()
    }

    pub fn contract_hash(&self) -> Result<String, CanonicalError> {
        let digest = Sha256::digest(self.canonical_bytes()?);
        Ok(format!("sha256:{digest:x}"))
    }

    /// NG-02A child/daemon admission: fail closed unless the tool is known,
    /// schema-pinned and has an explicit result policy. External-effect tools
    /// additionally require an idempotency-with-receipt class (no receipt
    /// today means the tool stays root-interactive-only).
    pub fn child_admissible(&self) -> bool {
        if self.tool_kind == ToolKind::Other {
            return false;
        }
        if self.input_schema_hash.is_none() {
            return false;
        }
        if self.result_policy.is_none() {
            return false;
        }
        if self.operation_class == OperationClass::ExternalEffect
            && self.idempotency_class != ToolIdempotencyClass::IdempotentWithReceipt
        {
            return false;
        }
        true
    }
}

/// Surfaces that may invoke a tool. Child and daemon are fail-closed without a
/// full [`ToolContractV1`]; root interactive may still approve tools that lack
/// a sealed contract (legacy root UX).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ToolDispatchSurface {
    RootInteractive,
    Child,
    Daemon,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ToolDispatchDeny {
    MissingContract,
    NotChildAdmissible,
}

impl ToolDispatchDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::MissingContract => "tool_contract.missing",
            Self::NotChildAdmissible => "tool_contract.not_child_admissible",
        }
    }
}

/// Forced dispatch admission (NG-02A Exit Gate entry): every non-root surface
/// must present an admissible contract before the tool body runs.
pub fn authorize_tool_dispatch(
    surface: ToolDispatchSurface,
    contract: Option<&ToolContractV1>,
) -> Result<(), ToolDispatchDeny> {
    match surface {
        ToolDispatchSurface::RootInteractive => Ok(()),
        ToolDispatchSurface::Child | ToolDispatchSurface::Daemon => match contract {
            None => Err(ToolDispatchDeny::MissingContract),
            Some(c) if c.child_admissible() => Ok(()),
            Some(_) => Err(ToolDispatchDeny::NotChildAdmissible),
        },
    }
}

/// Force result projection: apply the contract's preview limit when present,
/// otherwise clamp to a hard fail-closed default so unbounded tool output
/// never reaches model context.
pub fn force_result_projection(
    envelope: ToolResultEnvelopeV1,
    contract: Option<&ToolContractV1>,
) -> ToolResultEnvelopeV1 {
    const HARD_DEFAULT_PREVIEW: u32 = 8192;
    let limit = contract
        .and_then(|c| c.result_policy.as_ref())
        .map(|p| p.preview_byte_limit)
        .unwrap_or(HARD_DEFAULT_PREVIEW);
    envelope.apply_preview_limit(limit)
}

/// Build a runtime contract from the registry tool kind for dispatch-time
/// admission. Classified tools get a pinned schema identity (name+kind hash)
/// and a default result policy; [`ToolKind::Other`] stays non-admissible for
/// child/daemon surfaces (callers still pass it so deny is explicit).
pub fn contract_from_runtime_kind(
    tool_name: &str,
    kind: ToolKind,
    is_read_only: bool,
    preview_byte_limit: u32,
) -> ToolContractV1 {
    let schema_seed = format!("{tool_name}\0{}", kind.as_key());
    let schema_hash = format!("sha256:{:x}", Sha256::digest(schema_seed.as_bytes()));
    let operation_class = if is_read_only {
        OperationClass::ReadOnly
    } else if matches!(
        kind,
        ToolKind::Execute
            | ToolKind::WebSearch
            | ToolKind::WebFetch
            | ToolKind::DeployApp
            | ToolKind::Task
            | ToolKind::Monitor
    ) {
        // Shell / network / spawn are external-effect for child admission.
        OperationClass::ExternalEffect
    } else {
        OperationClass::ReversibleWrite
    };
    // ExternalEffect requires IdempotentWithReceipt for child_admissible —
    // runtime classified tools that are effectful still need a receipt class
    // so children can run them under a sealed contract (NeverReplay would
    // force root-only).
    let idempotency_class = match operation_class {
        OperationClass::ReadOnly => ToolIdempotencyClass::ReadOnlyRetryable,
        OperationClass::ReversibleWrite | OperationClass::ExternalEffect => {
            ToolIdempotencyClass::IdempotentWithReceipt
        }
    };
    ToolContractV1 {
        schema_version: TOOL_CONTRACT_SCHEMA_VERSION,
        namespace: "runtime".into(),
        tool_name: tool_name.to_owned(),
        tool_kind: kind,
        operation_class,
        input_schema_hash: Some(schema_hash),
        result_policy: Some(ToolResultPolicyV1 {
            artifact_class: ArtifactClass::WorkspacePrivate,
            preview_byte_limit: preview_byte_limit.max(1),
        }),
        idempotency_class,
        provider_or_endpoint_ref: None,
        policy_revision: 1,
        encoding_revision: ENCODING_REVISION,
    }
}

/// Clamp raw tool result text the same way production dispatch does: wrap in
/// an envelope and apply [`force_result_projection`].
pub fn clamp_tool_result_text(
    call_id: &str,
    tool_name: &str,
    kind: ToolKind,
    is_read_only: bool,
    text: String,
    preview_byte_limit: u32,
) -> String {
    let contract = contract_from_runtime_kind(tool_name, kind, is_read_only, preview_byte_limit);
    let hash = contract.contract_hash().unwrap_or_else(|_| "sha256:unknown".into());
    let envelope = ToolResultEnvelopeV1 {
        call_id: call_id.to_owned(),
        tool_contract_hash: hash,
        operation_id: None,
        status: ToolResultStatus::Succeeded,
        preview: text,
        preview_truncated: false,
        full_artifact_ref: None,
        emitted_bytes: 0,
        context_bytes_admitted: 0,
        verification_ref: None,
    };
    force_result_projection(envelope, Some(&contract)).preview
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ToolResultStatus {
    Succeeded,
    Failed,
    Cancelled,
    Unknown,
}

impl ToolResultStatus {
    fn as_str(self) -> &'static str {
        match self {
            ToolResultStatus::Succeeded => "succeeded",
            ToolResultStatus::Failed => "failed",
            ToolResultStatus::Cancelled => "cancelled",
            ToolResultStatus::Unknown => "unknown",
        }
    }
}

/// Bounded, redacted projection of a tool result. The full output belongs in
/// an artifact store; the model context only ever sees `preview`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToolResultEnvelopeV1 {
    pub call_id: String,
    pub tool_contract_hash: String,
    pub operation_id: Option<String>,
    pub status: ToolResultStatus,
    pub preview: String,
    pub preview_truncated: bool,
    pub full_artifact_ref: Option<String>,
    pub emitted_bytes: u64,
    pub context_bytes_admitted: u32,
    pub verification_ref: Option<String>,
}

impl ToolResultEnvelopeV1 {
    pub fn canonical_bytes(&self) -> Result<Vec<u8>, CanonicalError> {
        let record = CanonicalRecord::new("tool-result")
            .field("call_id", CanonicalValue::str(&self.call_id))
            .field(
                "tool_contract_hash",
                CanonicalValue::str(&self.tool_contract_hash),
            )
            .field(
                "operation_id",
                self.operation_id
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field("status", CanonicalValue::str(self.status.as_str()))
            .field("preview", CanonicalValue::str(&self.preview))
            .field("preview_truncated", CanonicalValue::Bool(self.preview_truncated))
            .field(
                "full_artifact_ref",
                self.full_artifact_ref
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field("emitted_bytes", CanonicalValue::U64(self.emitted_bytes))
            .field("context_bytes_admitted", CanonicalValue::U64(u64::from(self.context_bytes_admitted)))
            .field(
                "verification_ref",
                self.verification_ref
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            );
        record.canonical_bytes()
    }

    pub fn result_hash(&self) -> Result<String, CanonicalError> {
        let digest = Sha256::digest(self.canonical_bytes()?);
        Ok(format!("sha256:{digest:x}"))
    }

    /// Apply the contract's preview byte limit. Truncation is always marked,
    /// never silent, and never splits a UTF-8 character.
    pub fn apply_preview_limit(mut self, limit: u32) -> Self {
        if self.preview.len() as u32 > limit {
            let mut end = limit as usize;
            while !self.preview.is_char_boundary(end) {
                end -= 1;
            }
            self.preview = self.preview[..end].to_owned();
            self.preview_truncated = true;
        }
        self
    }
}

/// S3 (NG-03D): children must never self-commit/push/merge (INV-13).
/// Scans command text for a `git` invocation whose subcommand is a mutation
/// verb. Tokens are split on non-alphanumeric characters (`-`, `_`, `.` are
/// kept so `merge-base`, `-C`, `--git-dir` stay intact — JSON punctuation
/// around the command field splits cleanly). Git option values are skipped
/// (`-C <path>`, `-c k=v`, `--git-dir <path>`), and known read-only git
/// verbs stop the scan. Fail-closed: any git mutation verb in command text
/// is reported; callers gate this on command-execution tools only, so
/// innocent prose that merely *mentions* `git commit` is never executed.
pub fn child_git_mutation_in(args: &str) -> Option<&'static str> {
    const SAFE_GIT_VERBS: &[&str] = &[
        "status",
        "log",
        "diff",
        "show",
        "rev-parse",
        "merge-base",
        "ls-files",
        "check-ignore",
        "remote",
        "config",
        "help",
        "version",
        "fetch",
        "pull",
    ];
    let words: Vec<&str> = args
        .split(|c: char| !c.is_ascii_alphanumeric() && c != '-' && c != '_' && c != '.')
        .filter(|word| !word.is_empty())
        .collect();
    let mut i = 0;
    while i < words.len() {
        if words[i] == "git" {
            let mut j = i + 1;
            let mut path_fragments = 0;
            while j < words.len() {
                let token = words[j];
                if matches!(token, "commit" | "push" | "merge") {
                    return Some("child_git_mutation");
                }
                if token.starts_with('-') {
                    // Option flags are skipped; -C/-c/--git-dir also consume
                    // their value word.
                    if matches!(token, "-C" | "-c" | "--git-dir") {
                        j += 1;
                    }
                    j += 1;
                    path_fragments = 0;
                    continue;
                }
                if SAFE_GIT_VERBS.contains(&token) {
                    break;
                }
                // Path fragments of an option value (e.g. `/tmp/repo`
                // splitting into `tmp`, `repo`): allow up to three before the
                // verb.
                path_fragments += 1;
                if path_fragments > 3 {
                    break;
                }
                j += 1;
            }
        }
        i += 1;
    }
    None
}

// ────────────────────────────────────────────────────────────────────────
// DEBT-028 W2b-2 — per-turn context ledger with an eviction rule.
//
// The model sees a *bounded projection* of tool results; the full raw output
// lives in artifacts. Eviction keeps the most recent `FULL_RESULT_KEEP_COUNT`
// results complete, downgrades older ones to preview + artifact ref, and
// defers new results when even the preview budget is exhausted. The tool
// CALL fact is never hidden — eviction affects bytes, never the call ledger.
// ────────────────────────────────────────────────────────────────────────

pub const TURN_CONTEXT_SCHEMA_VERSION: u16 = 1;
/// Default per-turn tool-result context budget.
pub const DEFAULT_TURN_CONTEXT_BYTES: u32 = 256 * 1024;
/// Preview size when a result is truncated.
pub const DEFAULT_TOOL_PREVIEW_BYTES: u32 = 4 * 1024;
/// How many most-recent results stay complete before downgrade.
pub const FULL_RESULT_KEEP_COUNT: usize = 8;

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct TurnContextBudgetV1 {
    pub schema_version: u16,
    pub total_bytes: u32,
    /// Assignment / manifest / system fragments — never evicted.
    pub fixed_reserved: u32,
    /// Remaining capacity for tool results after the fixed reservation.
    pub tool_result_capacity: u32,
    /// Bytes currently admitted.
    pub admitted: u32,
}

impl TurnContextBudgetV1 {
    pub fn new(total_bytes: u32, fixed_reserved: u32) -> Self {
        Self {
            schema_version: TURN_CONTEXT_SCHEMA_VERSION,
            total_bytes,
            fixed_reserved,
            tool_result_capacity: total_bytes.saturating_sub(fixed_reserved),
            admitted: 0,
        }
    }

    pub fn remaining(&self) -> u32 {
        self.tool_result_capacity.saturating_sub(self.admitted)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ContextAdmission {
    /// The full result bytes are admitted.
    Complete,
    /// Only the bounded preview is admitted; the full result is an artifact.
    TruncatedToPreview,
    /// Even the preview is deferred; the call fact remains visible.
    DeferredToArtifact,
}

/// One tool call fact. Recorded independently of the byte budget — the model
/// always knows WHICH calls happened, even when their bytes are evicted.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct ToolCallFact {
    pub call_id: String,
    pub tool_identity: String,
    pub admitted: ContextAdmission,
}

/// Pure admission into the per-turn context budget.
///
/// - `result_bytes` = raw result size;
/// - `release_oldest_full_bytes` = bytes freed by downgrading the oldest
///   complete result to preview + ref (0 when nothing can be released).
///
/// Invariants: `admitted ≤ tool_result_capacity` always holds; the call
/// fact is appended by the caller regardless of the admission outcome.
pub fn admit_tool_result(
    budget: &TurnContextBudgetV1,
    result_bytes: u32,
    release_oldest_full_bytes: u32,
) -> (ContextAdmission, TurnContextBudgetV1) {
    let mut next = budget.clone();
    if result_bytes <= next.remaining() {
        next.admitted += result_bytes;
        return (ContextAdmission::Complete, next);
    }
    // Try evicting the oldest complete result first.
    if release_oldest_full_bytes > 0 {
        let released = release_oldest_full_bytes.min(next.admitted);
        next.admitted -= released;
        if result_bytes <= next.remaining() {
            next.admitted += result_bytes;
            return (ContextAdmission::Complete, next);
        }
    }
    // Preview path: bounded preview only.
    let preview = DEFAULT_TOOL_PREVIEW_BYTES.min(result_bytes);
    if preview <= next.remaining() {
        next.admitted += preview;
        return (ContextAdmission::TruncatedToPreview, next);
    }
    // Even the preview does not fit — defer to artifact.
    (ContextAdmission::DeferredToArtifact, next)
}

#[cfg(test)]
mod tool_contract_tests {
    use super::*;

    fn read_contract() -> ToolContractV1 {
        ToolContractV1 {
            schema_version: TOOL_CONTRACT_SCHEMA_VERSION,
            namespace: "grok_build".to_owned(),
            tool_name: "read_file".to_owned(),
            tool_kind: ToolKind::Read,
            operation_class: OperationClass::ReadOnly,
            input_schema_hash: Some("sha256:schema".to_owned()),
            result_policy: Some(ToolResultPolicyV1 {
                preview_byte_limit: 4096,
                artifact_class: ArtifactClass::WorkspacePrivate,
            }),
            idempotency_class: ToolIdempotencyClass::ReadOnlyRetryable,
            provider_or_endpoint_ref: None,
            policy_revision: 1,
            encoding_revision: ENCODING_REVISION,
        }
    }

    #[test]
    fn contract_hash_is_stable_and_pinned() {
        assert_eq!(
            read_contract().contract_hash().unwrap(),
            "sha256:f2c99d3391ae6a1153a29906ac02f36ac4f0300ae3945541d15eaffd1bcee2d6"
        );
    }

    #[test]
    fn same_contract_same_hash_any_field_drift_changes_it() {
        let base = read_contract();
        assert_eq!(
            base.contract_hash().unwrap(),
            read_contract().contract_hash().unwrap()
        );
        let mut different = base.clone();
        different.policy_revision = 2;
        assert_ne!(base.contract_hash().unwrap(), different.contract_hash().unwrap());
        let mut different = base.clone();
        different.tool_kind = ToolKind::Execute;
        assert_ne!(base.contract_hash().unwrap(), different.contract_hash().unwrap());
        let mut different = base.clone();
        different.operation_class = OperationClass::ReversibleWrite;
        assert_ne!(base.contract_hash().unwrap(), different.contract_hash().unwrap());
    }

    #[test]
    fn child_admission_fails_closed_on_unknown_or_missing_parts() {
        let base = read_contract();
        assert!(base.child_admissible());

        let mut other = base.clone();
        other.tool_kind = ToolKind::Other;
        assert!(!other.child_admissible(), "Other kind must deny children");

        let mut no_schema = base.clone();
        no_schema.input_schema_hash = None;
        assert!(!no_schema.child_admissible(), "missing schema hash must deny");

        let mut no_policy = base.clone();
        no_policy.result_policy = None;
        assert!(!no_policy.child_admissible(), "missing result policy must deny");
    }

    #[test]
    fn external_effect_requires_idempotency_receipt_for_children() {
        let mut contract = read_contract();
        contract.operation_class = OperationClass::ExternalEffect;
        contract.idempotency_class = ToolIdempotencyClass::NeverReplay;
        assert!(
            !contract.child_admissible(),
            "external effect without receipt class must stay root-only"
        );
        contract.idempotency_class = ToolIdempotencyClass::IdempotentWithReceipt;
        assert!(
            contract.child_admissible(),
            "static admission only; runtime still requires an actual receipt"
        );
    }

    #[test]
    fn preview_limit_marks_truncation_and_never_splits_utf8() {
        let envelope = ToolResultEnvelopeV1 {
            call_id: "call-1".to_owned(),
            tool_contract_hash: "sha256:contract".to_owned(),
            operation_id: None,
            status: ToolResultStatus::Succeeded,
            preview: "héllo wörld".to_owned(),
            preview_truncated: false,
            full_artifact_ref: None,
            emitted_bytes: 100,
            context_bytes_admitted: 0,
            verification_ref: None,
        };
        let bounded = envelope.clone().apply_preview_limit(6);
        assert!(bounded.preview_truncated);
        assert!(bounded.preview.is_char_boundary(bounded.preview.len()));
        assert_eq!(bounded.preview, "héllo");

        let unbounded = envelope.apply_preview_limit(1000);
        assert!(!unbounded.preview_truncated);
        assert_eq!(unbounded.preview, "héllo wörld");
    }

    #[test]
    fn result_envelope_commits_to_preview_and_status() {
        let base = ToolResultEnvelopeV1 {
            call_id: "call-1".to_owned(),
            tool_contract_hash: "sha256:contract".to_owned(),
            operation_id: None,
            status: ToolResultStatus::Succeeded,
            preview: "ok".to_owned(),
            preview_truncated: false,
            full_artifact_ref: None,
            emitted_bytes: 10,
            context_bytes_admitted: 2,
            verification_ref: None,
        };
        let mut failed = base.clone();
        failed.status = ToolResultStatus::Failed;
        assert_ne!(base.result_hash().unwrap(), failed.result_hash().unwrap());
        let mut unknown_delivery = base.clone();
        unknown_delivery.status = ToolResultStatus::Unknown;
        assert_ne!(base.result_hash().unwrap(), unknown_delivery.result_hash().unwrap());
    }

    #[test]
    fn authorize_tool_dispatch_forces_contract_on_child_and_daemon() {
        let ok = read_contract();
        assert!(authorize_tool_dispatch(ToolDispatchSurface::RootInteractive, None).is_ok());
        assert!(authorize_tool_dispatch(ToolDispatchSurface::Child, Some(&ok)).is_ok());
        assert!(authorize_tool_dispatch(ToolDispatchSurface::Daemon, Some(&ok)).is_ok());
        assert_eq!(
            authorize_tool_dispatch(ToolDispatchSurface::Child, None).unwrap_err(),
            ToolDispatchDeny::MissingContract
        );
        let mut other = ok.clone();
        other.tool_kind = ToolKind::Other;
        assert_eq!(
            authorize_tool_dispatch(ToolDispatchSurface::Child, Some(&other)).unwrap_err(),
            ToolDispatchDeny::NotChildAdmissible
        );
    }

    #[test]
    fn context_budget_admits_complete_results_until_capacity() {
        let mut budget = TurnContextBudgetV1::new(10_000, 2_000);
        assert_eq!(budget.tool_result_capacity, 8_000);
        let (admission, next) = admit_tool_result(&budget, 3_000, 0);
        assert_eq!(admission, ContextAdmission::Complete);
        assert_eq!(next.admitted, 3_000);
        let (admission, next) = admit_tool_result(&next, 4_000, 0);
        assert_eq!(admission, ContextAdmission::Complete);
        assert_eq!(next.admitted, 7_000);
        assert!(next.admitted <= next.tool_result_capacity);
    }

    #[test]
    fn context_budget_evicts_oldest_before_truncating() {
        // Full budget: releasing the oldest complete result frees bytes and
        // lets the new result in complete.
        let mut budget = TurnContextBudgetV1::new(10_000, 2_000);
        budget.admitted = 8_000;
        let (admission, next) = admit_tool_result(&budget, 2_000, 3_000);
        assert_eq!(admission, ContextAdmission::Complete);
        assert_eq!(next.admitted, 8_000 - 3_000 + 2_000);
        assert!(next.admitted <= next.tool_result_capacity);
    }

    #[test]
    fn context_budget_truncates_or_defers_when_nothing_can_be_released() {
        let mut budget = TurnContextBudgetV1::new(10_000, 0);
        budget.admitted = 5_000; // 5_000 remaining
        // Result larger than remaining, nothing to release → preview only
        // (preview 4_096 fits in the 5_000 remaining).
        let (admission, next) = admit_tool_result(&budget, 6_000, 0);
        assert_eq!(admission, ContextAdmission::TruncatedToPreview);
        assert_eq!(next.admitted, 5_000 + DEFAULT_TOOL_PREVIEW_BYTES);
        // Preview no longer fits → deferred, bytes unchanged.
        let (admission, next2) = admit_tool_result(&next, 6_000, 0);
        assert_eq!(admission, ContextAdmission::DeferredToArtifact);
        assert_eq!(next2.admitted, next.admitted);
        assert!(next2.admitted <= next2.tool_result_capacity);
    }

    #[test]
    fn call_facts_are_never_hidden_by_eviction() {
        // The call ledger is independent of the byte budget: even a deferred
        // result leaves a visible call fact (the model always knows WHICH
        // tool was called, only its bytes are deferred).
        let mut budget = TurnContextBudgetV1::new(1_000, 500);
        budget.admitted = 500; // full
        let (admission, _) = admit_tool_result(&budget, 99_999, 0);
        let fact = ToolCallFact {
            call_id: "call-9".into(),
            tool_identity: "grep".into(),
            admitted: admission,
        };
        assert_eq!(fact.admitted, ContextAdmission::DeferredToArtifact);
        assert_eq!(fact.call_id, "call-9");
        assert_eq!(fact.tool_identity, "grep");
        // A deferred call fact is still recorded — eviction never erases
        // the fact that the call happened.
        let ledger = vec![fact];
        assert_eq!(ledger.len(), 1);
        assert_eq!(ledger[0].admitted, ContextAdmission::DeferredToArtifact);
    }

    #[test]
    fn force_result_projection_applies_contract_or_hard_default() {
        let contract = read_contract();
        let big = ToolResultEnvelopeV1 {
            call_id: "c".into(),
            tool_contract_hash: "sha256:x".into(),
            operation_id: None,
            status: ToolResultStatus::Succeeded,
            preview: "x".repeat(10_000),
            preview_truncated: false,
            full_artifact_ref: None,
            emitted_bytes: 10_000,
            context_bytes_admitted: 0,
            verification_ref: None,
        };
        let projected = force_result_projection(big.clone(), Some(&contract));
        assert!(projected.preview_truncated);
        assert!(projected.preview.len() as u32 <= contract.result_policy.as_ref().unwrap().preview_byte_limit);
        let no_contract = force_result_projection(big, None);
        assert!(no_contract.preview_truncated);
        assert!(no_contract.preview.len() <= 8192);
    }

    #[test]
    fn runtime_kind_contract_admits_classified_tools_and_denies_other() {
        let read = contract_from_runtime_kind("read_file", ToolKind::Read, true, 128);
        assert!(read.child_admissible());
        assert!(authorize_tool_dispatch(ToolDispatchSurface::Child, Some(&read)).is_ok());
        let exec = contract_from_runtime_kind("run_terminal_command", ToolKind::Execute, false, 128);
        assert!(
            exec.child_admissible(),
            "classified Execute must be child-admissible under sealed runtime contract"
        );
        let other = contract_from_runtime_kind("mcp_weird", ToolKind::Other, false, 128);
        assert!(!other.child_admissible());
        assert_eq!(
            authorize_tool_dispatch(ToolDispatchSurface::Child, Some(&other)).unwrap_err(),
            ToolDispatchDeny::NotChildAdmissible
        );
    }

    #[test]
    fn clamp_tool_result_text_enforces_preview_limit() {
        let big = "y".repeat(10_000);
        let out = clamp_tool_result_text(
            "call-1",
            "read_file",
            ToolKind::Read,
            true,
            big,
            64,
        );
        assert!(out.len() <= 64);
        assert_ne!(out.len(), 10_000);
    }

    #[test]
    fn child_git_mutation_detection_matrix() {
        // Positive: mutation verbs after git (with and without flags).
        assert_eq!(
            child_git_mutation_in(r#"{"command": "git commit -m x"}"#),
            Some("child_git_mutation")
        );
        assert_eq!(
            child_git_mutation_in("git push origin main"),
            Some("child_git_mutation")
        );
        assert_eq!(
            child_git_mutation_in("git -C /tmp/x merge feature"),
            Some("child_git_mutation")
        );
        assert_eq!(
            child_git_mutation_in("git -c user.name=a commit -m x"),
            Some("child_git_mutation")
        );
        assert_eq!(
            child_git_mutation_in("cd repo && git push"),
            Some("child_git_mutation")
        );
        // Negative: non-mutation git verbs and plain text are allowed.
        assert_eq!(child_git_mutation_in("git status"), None);
        assert_eq!(child_git_mutation_in("git log --oneline"), None);
        assert_eq!(child_git_mutation_in("git diff HEAD"), None);
        assert_eq!(child_git_mutation_in("git merge-base A B"), None);
        assert_eq!(child_git_mutation_in("grep -l commit file"), None);
        assert_eq!(child_git_mutation_in(""), None);
        assert_eq!(child_git_mutation_in("no git here"), None);
        // Fail-closed: command text that executes a git mutation verb is
        // denied even inside prose/quoting — children never self-mutate.
        assert_eq!(
            child_git_mutation_in("search for 'git commit' in docs"),
            Some("child_git_mutation")
        );
    }
}
