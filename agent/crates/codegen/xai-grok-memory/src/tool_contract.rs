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

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
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
}
