//! NG-01 identity layer — the four run-identity DTOs from the master plan §0.1.4.
//!
//! [`NodeIdentityV1`] is the immutable identity of a task-tree node;
//! [`GrantRevisionV1`] is the revocable grant revision bound to an accepted
//! snapshot and context manifest; [`AttemptContextV1`] is the single-attempt
//! context (budget reservation, model receipt, deadline); and
//! [`GovernedRunEnvelopeV1`] is the immutable receipt/projection that
//! references the other three by hash/revision without mixing eternal
//! identity, revocable grant and per-attempt state.
//!
//! Fail-closed rules (INV-12/INV-20):
//! - Every identity-bearing record uses the canonical encoding; the stored
//!   hash must recompute or the record is invalid (tamper = deny).
//! - `expires_at_unix`/`deadline_unix` are mandatory on grant/attempt; a zero
//!   or missing expiry is an invalid record, never "forever".
//! - An envelope must bind exactly one identity hash, one grant revision and
//!   one attempt; any mismatch (foreign identity, stale revision, foreign
//!   attempt) is a hard deny.

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue};
use crate::tool_contract::OperationClass;

pub const IDENTITY_ENVELOPE_SCHEMA_VERSION: u16 = 1;

/// Immutable identity of one task-tree node (master plan §0.1.4).
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct NodeIdentityV1 {
    pub schema_version: u16,
    pub task_tree_id: String,
    pub node_id: String,
    pub root_session_id: String,
    pub immediate_parent_id: Option<String>,
    /// Full lineage path root..=node. Last element must equal `node_id`.
    pub lineage_path: Vec<String>,
    /// Immutable objective/assignment content hash (`sha256:...`).
    pub immutable_assignment_hash: String,
    /// Canonical hash over all fields above (identity is public, never secret).
    pub identity_hash: String,
}

/// Revocable grant revision: what a node may do right now (master plan §0.1.4).
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct GrantRevisionV1 {
    pub schema_version: u16,
    /// Monotonic grant revision — a rebase/revoke creates a new revision.
    pub revision: u64,
    pub accepted_snapshot_hash: String,
    pub context_manifest_hash: String,
    pub capability_grant_id: String,
    pub policy_revision: u64,
    pub sandbox_id: String,
    pub write_scope_lease_id: Option<String>,
    /// Mandatory expiry; a zero/omitted expiry is an invalid record.
    pub valid_until_unix: u64,
    /// Canonical hash over all fields above.
    pub grant_hash: String,
}

/// Single-attempt context (master plan §0.1.4).
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct AttemptContextV1 {
    pub schema_version: u16,
    pub attempt_id: String,
    pub budget_reservation_id: String,
    pub model_selection_receipt: String,
    pub idempotency_key: Option<String>,
    /// Mandatory deadline; a zero/omitted deadline is an invalid record.
    pub deadline_unix: u64,
    /// Grant revision this attempt observed at admission time.
    pub observed_grant_revision: u64,
    /// Canonical hash over all fields above.
    pub attempt_hash: String,
}

/// Immutable receipt/projection of one governed run (master plan §0.1.4).
/// Only references identity/grant/attempt by hash/revision — never embeds
/// revocable state.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct GovernedRunEnvelopeV1 {
    pub schema_version: u16,
    pub run_id: String,
    pub identity_hash: String,
    pub grant_revision: u64,
    pub attempt_id: String,
    pub lease_id: Option<String>,
    pub operation_class: OperationClass,
    pub evidence_sink: String,
    pub created_at_unix: u64,
    /// Canonical hash over all fields above.
    pub envelope_hash: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum IdentityDeny {
    Invalid(String),
    EmptyField(&'static str),
    BadLineage(String),
    HashMismatch(&'static str),
    ForeignIdentity,
    ForeignAttempt,
    StaleGrantRevision,
    ExpiredGrant,
    ExpiredDeadline,
    EnvelopeCreatedInFuture,
}

impl IdentityDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "identity.invalid",
            Self::EmptyField(_) => "identity.empty_field",
            Self::BadLineage(_) => "identity.bad_lineage",
            Self::HashMismatch(_) => "identity.hash_mismatch",
            Self::ForeignIdentity => "envelope.foreign_identity",
            Self::ForeignAttempt => "envelope.foreign_attempt",
            Self::StaleGrantRevision => "envelope.stale_grant_revision",
            Self::ExpiredGrant => "envelope.expired_grant",
            Self::ExpiredDeadline => "envelope.expired_deadline",
            Self::EnvelopeCreatedInFuture => "envelope.created_in_future",
        }
    }
}

impl std::fmt::Display for IdentityDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::BadLineage(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            Self::HashMismatch(what) => write!(f, "{}: {what}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

fn require_non_empty(field: &'static str, value: &str) -> Result<(), IdentityDeny> {
    if value.trim().is_empty() {
        return Err(IdentityDeny::EmptyField(field));
    }
    Ok(())
}

fn require_sha256(field: &'static str, value: &str) -> Result<(), IdentityDeny> {
    require_non_empty(field, value)?;
    if !value.starts_with("sha256:") || value.len() <= "sha256:".len() {
        return Err(IdentityDeny::Invalid(format!(
            "{field} must be a sha256:... reference"
        )));
    }
    Ok(())
}

fn node_identity_preimage(node: &NodeIdentityV1) -> Result<Vec<u8>, CanonicalError> {
    let mut record = CanonicalRecord::new("node-identity")
        .field("schema_version", CanonicalValue::U64(node.schema_version as u64))
        .field("task_tree_id", CanonicalValue::str(&node.task_tree_id))
        .field("node_id", CanonicalValue::str(&node.node_id))
        .field("root_session_id", CanonicalValue::str(&node.root_session_id))
        .field(
            "immediate_parent_id",
            match &node.immediate_parent_id {
                Some(p) => CanonicalValue::str(p),
                None => CanonicalValue::Null,
            },
        )
        .field(
            "lineage_path",
            CanonicalValue::Seq(
                node.lineage_path
                    .iter()
                    .map(|p| CanonicalValue::str(p))
                    .collect(),
            ),
        )
        .field(
            "immutable_assignment_hash",
            CanonicalValue::str(&node.immutable_assignment_hash),
        );
    record.canonical_bytes()
}

/// Compute the canonical identity hash for a node (public, deterministic).
/// Returns a `sha256:...` reference — the same shape as every other
/// hash-bearing reference in the identity layer.
pub fn compute_node_identity_hash(node: &NodeIdentityV1) -> Result<String, CanonicalError> {
    Ok(format!(
        "sha256:{}",
        hex_encode(&node_identity_preimage(node)?)
    ))
}

fn hex_encode(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push_str(&format!("{byte:02x}"));
    }
    out
}

impl NodeIdentityV1 {
    /// Validate every invariant and recompute the identity hash.
    pub fn validate(&self) -> Result<(), IdentityDeny> {
        if self.schema_version != IDENTITY_ENVELOPE_SCHEMA_VERSION {
            return Err(IdentityDeny::Invalid("schema_version mismatch".into()));
        }
        require_non_empty("task_tree_id", &self.task_tree_id)?;
        require_non_empty("node_id", &self.node_id)?;
        require_non_empty("root_session_id", &self.root_session_id)?;
        require_sha256("immutable_assignment_hash", &self.immutable_assignment_hash)?;
        if self.lineage_path.is_empty() {
            return Err(IdentityDeny::BadLineage("empty lineage path".into()));
        }
        if self.lineage_path.last().map(String::as_str) != Some(self.node_id.as_str()) {
            return Err(IdentityDeny::BadLineage(
                "lineage path must end at node_id".into(),
            ));
        }
        match (&self.immediate_parent_id, self.lineage_path.len()) {
            (Some(parent), len) if len >= 2 => {
                if self.lineage_path[len - 2] != *parent {
                    return Err(IdentityDeny::BadLineage(
                        "immediate_parent_id must equal lineage_path[len-2]".into(),
                    ));
                }
            }
            (Some(_), _) => {
                return Err(IdentityDeny::BadLineage(
                    "parent requires a lineage path of length >= 2".into(),
                ));
            }
            (None, len) if len != 1 => {
                return Err(IdentityDeny::BadLineage(
                    "root node must have a single-element lineage path".into(),
                ));
            }
            (None, _) => {}
        }
        let recomputed = compute_node_identity_hash(self)
            .map_err(|e| IdentityDeny::Invalid(e.to_string()))?;
        if recomputed != self.identity_hash {
            return Err(IdentityDeny::HashMismatch("node identity_hash"));
        }
        Ok(())
    }
}

/// Issue a validated node identity from a checked request.
pub fn issue_node_identity(
    task_tree_id: impl Into<String>,
    node_id: impl Into<String>,
    root_session_id: impl Into<String>,
    immediate_parent_id: Option<String>,
    lineage_path: Vec<String>,
    immutable_assignment_hash: impl Into<String>,
) -> Result<NodeIdentityV1, IdentityDeny> {
    let node = NodeIdentityV1 {
        schema_version: IDENTITY_ENVELOPE_SCHEMA_VERSION,
        task_tree_id: task_tree_id.into(),
        node_id: node_id.into(),
        root_session_id: root_session_id.into(),
        immediate_parent_id,
        lineage_path,
        immutable_assignment_hash: immutable_assignment_hash.into(),
        identity_hash: String::new(),
    };
    let hash = compute_node_identity_hash(&node)
        .map_err(|e| IdentityDeny::Invalid(e.to_string()))?;
    let mut node = node;
    node.identity_hash = hash;
    node.validate()?;
    Ok(node)
}

fn grant_revision_preimage(grant: &GrantRevisionV1) -> Result<Vec<u8>, CanonicalError> {
    CanonicalRecord::new("grant-revision")
        .field("schema_version", CanonicalValue::U64(grant.schema_version as u64))
        .field("revision", CanonicalValue::U64(grant.revision))
        .field(
            "accepted_snapshot_hash",
            CanonicalValue::str(&grant.accepted_snapshot_hash),
        )
        .field(
            "context_manifest_hash",
            CanonicalValue::str(&grant.context_manifest_hash),
        )
        .field(
            "capability_grant_id",
            CanonicalValue::str(&grant.capability_grant_id),
        )
        .field("policy_revision", CanonicalValue::U64(grant.policy_revision))
        .field("sandbox_id", CanonicalValue::str(&grant.sandbox_id))
        .field(
            "write_scope_lease_id",
            match &grant.write_scope_lease_id {
                Some(lease) => CanonicalValue::str(lease),
                None => CanonicalValue::Null,
            },
        )
        .field("valid_until_unix", CanonicalValue::U64(grant.valid_until_unix))
        .canonical_bytes()
}

impl GrantRevisionV1 {
    pub fn validate(&self) -> Result<(), IdentityDeny> {
        if self.schema_version != IDENTITY_ENVELOPE_SCHEMA_VERSION {
            return Err(IdentityDeny::Invalid("schema_version mismatch".into()));
        }
        if self.revision == 0 {
            return Err(IdentityDeny::Invalid("grant revision must start at 1".into()));
        }
        if self.policy_revision == 0 {
            return Err(IdentityDeny::Invalid("policy_revision must start at 1".into()));
        }
        require_sha256("accepted_snapshot_hash", &self.accepted_snapshot_hash)?;
        require_sha256("context_manifest_hash", &self.context_manifest_hash)?;
        require_non_empty("capability_grant_id", &self.capability_grant_id)?;
        require_non_empty("sandbox_id", &self.sandbox_id)?;
        if self.valid_until_unix == 0 {
            return Err(IdentityDeny::Invalid(
                "grant valid_until_unix is mandatory; zero means invalid".into(),
            ));
        }
        let recomputed = grant_revision_preimage(self)
            .map_err(|e| IdentityDeny::Invalid(e.to_string()))?;
        if hex_encode(&recomputed) != self.grant_hash {
            return Err(IdentityDeny::HashMismatch("grant revision grant_hash"));
        }
        Ok(())
    }
}

pub fn issue_grant_revision(
    revision: u64,
    accepted_snapshot_hash: impl Into<String>,
    context_manifest_hash: impl Into<String>,
    capability_grant_id: impl Into<String>,
    policy_revision: u64,
    sandbox_id: impl Into<String>,
    write_scope_lease_id: Option<String>,
    valid_until_unix: u64,
) -> Result<GrantRevisionV1, IdentityDeny> {
    let grant = GrantRevisionV1 {
        schema_version: IDENTITY_ENVELOPE_SCHEMA_VERSION,
        revision,
        accepted_snapshot_hash: accepted_snapshot_hash.into(),
        context_manifest_hash: context_manifest_hash.into(),
        capability_grant_id: capability_grant_id.into(),
        policy_revision,
        sandbox_id: sandbox_id.into(),
        write_scope_lease_id,
        valid_until_unix,
        grant_hash: String::new(),
    };
    let hash = grant_revision_preimage(&grant)
        .map_err(|e| IdentityDeny::Invalid(e.to_string()))?;
    let mut grant = grant;
    grant.grant_hash = hex_encode(&hash);
    grant.validate()?;
    Ok(grant)
}

fn attempt_preimage(attempt: &AttemptContextV1) -> Result<Vec<u8>, CanonicalError> {
    CanonicalRecord::new("attempt-context")
        .field("schema_version", CanonicalValue::U64(attempt.schema_version as u64))
        .field("attempt_id", CanonicalValue::str(&attempt.attempt_id))
        .field(
            "budget_reservation_id",
            CanonicalValue::str(&attempt.budget_reservation_id),
        )
        .field(
            "model_selection_receipt",
            CanonicalValue::str(&attempt.model_selection_receipt),
        )
        .field(
            "idempotency_key",
            match &attempt.idempotency_key {
                Some(key) => CanonicalValue::str(key),
                None => CanonicalValue::Null,
            },
        )
        .field("deadline_unix", CanonicalValue::U64(attempt.deadline_unix))
        .field(
            "observed_grant_revision",
            CanonicalValue::U64(attempt.observed_grant_revision),
        )
        .canonical_bytes()
}

impl AttemptContextV1 {
    pub fn validate(&self) -> Result<(), IdentityDeny> {
        if self.schema_version != IDENTITY_ENVELOPE_SCHEMA_VERSION {
            return Err(IdentityDeny::Invalid("schema_version mismatch".into()));
        }
        require_non_empty("attempt_id", &self.attempt_id)?;
        require_non_empty("budget_reservation_id", &self.budget_reservation_id)?;
        require_non_empty("model_selection_receipt", &self.model_selection_receipt)?;
        if self.deadline_unix == 0 {
            return Err(IdentityDeny::Invalid(
                "attempt deadline_unix is mandatory; zero means invalid".into(),
            ));
        }
        if self.observed_grant_revision == 0 {
            return Err(IdentityDeny::Invalid(
                "observed_grant_revision must start at 1".into(),
            ));
        }
        let recomputed = attempt_preimage(self)
            .map_err(|e| IdentityDeny::Invalid(e.to_string()))?;
        if hex_encode(&recomputed) != self.attempt_hash {
            return Err(IdentityDeny::HashMismatch("attempt attempt_hash"));
        }
        Ok(())
    }
}

pub fn issue_attempt_context(
    attempt_id: impl Into<String>,
    budget_reservation_id: impl Into<String>,
    model_selection_receipt: impl Into<String>,
    idempotency_key: Option<String>,
    deadline_unix: u64,
    observed_grant_revision: u64,
) -> Result<AttemptContextV1, IdentityDeny> {
    let attempt = AttemptContextV1 {
        schema_version: IDENTITY_ENVELOPE_SCHEMA_VERSION,
        attempt_id: attempt_id.into(),
        budget_reservation_id: budget_reservation_id.into(),
        model_selection_receipt: model_selection_receipt.into(),
        idempotency_key,
        deadline_unix,
        observed_grant_revision,
        attempt_hash: String::new(),
    };
    let hash = attempt_preimage(&attempt).map_err(|e| IdentityDeny::Invalid(e.to_string()))?;
    let mut attempt = attempt;
    attempt.attempt_hash = hex_encode(&hash);
    attempt.validate()?;
    Ok(attempt)
}

fn envelope_preimage(envelope: &GovernedRunEnvelopeV1) -> Result<Vec<u8>, CanonicalError> {
    CanonicalRecord::new("governed-run-envelope")
        .field("schema_version", CanonicalValue::U64(envelope.schema_version as u64))
        .field("run_id", CanonicalValue::str(&envelope.run_id))
        .field("identity_hash", CanonicalValue::str(&envelope.identity_hash))
        .field("grant_revision", CanonicalValue::U64(envelope.grant_revision))
        .field("attempt_id", CanonicalValue::str(&envelope.attempt_id))
        .field(
            "lease_id",
            match &envelope.lease_id {
                Some(lease) => CanonicalValue::str(lease),
                None => CanonicalValue::Null,
            },
        )
        .field(
            "operation_class",
            CanonicalValue::str(match envelope.operation_class {
                OperationClass::ReadOnly => "read-only",
                OperationClass::ReversibleWrite => "reversible-write",
                OperationClass::ExternalEffect => "external-effect",
            }),
        )
        .field("evidence_sink", CanonicalValue::str(&envelope.evidence_sink))
        .field("created_at_unix", CanonicalValue::U64(envelope.created_at_unix))
        .canonical_bytes()
}

impl GovernedRunEnvelopeV1 {
    pub fn validate(&self) -> Result<(), IdentityDeny> {
        if self.schema_version != IDENTITY_ENVELOPE_SCHEMA_VERSION {
            return Err(IdentityDeny::Invalid("schema_version mismatch".into()));
        }
        require_non_empty("run_id", &self.run_id)?;
        require_sha256("identity_hash", &self.identity_hash)?;
        require_non_empty("attempt_id", &self.attempt_id)?;
        require_non_empty("evidence_sink", &self.evidence_sink)?;
        if self.grant_revision == 0 {
            return Err(IdentityDeny::Invalid("grant_revision must start at 1".into()));
        }
        if self.created_at_unix == 0 {
            return Err(IdentityDeny::Invalid("created_at_unix must be set".into()));
        }
        let recomputed = envelope_preimage(self)
            .map_err(|e| IdentityDeny::Invalid(e.to_string()))?;
        if hex_encode(&recomputed) != self.envelope_hash {
            return Err(IdentityDeny::HashMismatch("envelope envelope_hash"));
        }
        Ok(())
    }

    /// Mint the immutable run envelope from a validated identity + grant +
    /// attempt. Fails closed on any binding mismatch or expiry at creation.
    pub fn mint(
        run_id: impl Into<String>,
        node: &NodeIdentityV1,
        grant: &GrantRevisionV1,
        attempt: &AttemptContextV1,
        lease_id: Option<String>,
        operation_class: OperationClass,
        evidence_sink: impl Into<String>,
        created_at_unix: u64,
    ) -> Result<Self, IdentityDeny> {
        node.validate()?;
        grant.validate()?;
        attempt.validate()?;
        if attempt.observed_grant_revision != grant.revision {
            return Err(IdentityDeny::StaleGrantRevision);
        }
        if grant.valid_until_unix < created_at_unix {
            return Err(IdentityDeny::ExpiredGrant);
        }
        if attempt.deadline_unix < created_at_unix {
            return Err(IdentityDeny::ExpiredDeadline);
        }
        let envelope = GovernedRunEnvelopeV1 {
            schema_version: IDENTITY_ENVELOPE_SCHEMA_VERSION,
            run_id: run_id.into(),
            identity_hash: node.identity_hash.clone(),
            grant_revision: grant.revision,
            attempt_id: attempt.attempt_id.clone(),
            lease_id,
            operation_class,
            evidence_sink: evidence_sink.into(),
            created_at_unix,
            envelope_hash: String::new(),
        };
        let hash = envelope_preimage(&envelope)
            .map_err(|e| IdentityDeny::Invalid(e.to_string()))?;
        let mut envelope = envelope;
        envelope.envelope_hash = hex_encode(&hash);
        envelope.validate()?;
        Ok(envelope)
    }

    /// Re-verify the envelope against live identity/grant/attempt at dispatch
    /// time (INV-12: actor re-checks grant revision, expiry and cancellation
    /// on every dispatch, never trusting the stored hash alone).
    pub fn verify(
        &self,
        node: &NodeIdentityV1,
        grant: &GrantRevisionV1,
        attempt: &AttemptContextV1,
        now_unix: u64,
    ) -> Result<(), IdentityDeny> {
        self.validate()?;
        node.validate()?;
        grant.validate()?;
        attempt.validate()?;
        if self.identity_hash != node.identity_hash {
            return Err(IdentityDeny::ForeignIdentity);
        }
        if self.grant_revision != grant.revision || attempt.observed_grant_revision != grant.revision
        {
            return Err(IdentityDeny::StaleGrantRevision);
        }
        if self.attempt_id != attempt.attempt_id {
            return Err(IdentityDeny::ForeignAttempt);
        }
        if grant.valid_until_unix < now_unix {
            return Err(IdentityDeny::ExpiredGrant);
        }
        if attempt.deadline_unix < now_unix {
            return Err(IdentityDeny::ExpiredDeadline);
        }
        if self.created_at_unix > now_unix {
            return Err(IdentityDeny::EnvelopeCreatedInFuture);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_node() -> NodeIdentityV1 {
        issue_node_identity(
            "tree-1",
            "node-2",
            "sess-root",
            Some("node-1".to_string()),
            vec!["node-1".to_string(), "node-2".to_string()],
            "sha256:assignment",
        )
        .expect("node")
    }

    fn sample_grant() -> GrantRevisionV1 {
        issue_grant_revision(
            1,
            "sha256:snapshot",
            "sha256:manifest",
            "grant-1",
            1,
            "sandbox-1",
            None,
            2_000_000_000,
        )
        .expect("grant")
    }

    fn sample_attempt() -> AttemptContextV1 {
        issue_attempt_context(
            "attempt-1",
            "res-1",
            "model-receipt-1",
            None,
            2_000_000_000,
            1,
        )
        .expect("attempt")
    }

    #[test]
    fn node_identity_roundtrip_hash_stable_and_valid() {
        let node = sample_node();
        node.validate().expect("valid");
        let recomputed = compute_node_identity_hash(&node).expect("hash");
        assert_eq!(recomputed, node.identity_hash);
        let node2 = issue_node_identity(
            "tree-1",
            "node-2",
            "sess-root",
            Some("node-1".into()),
            vec!["node-1".into(), "node-2".into()],
            "sha256:assignment",
        )
        .expect("node2");
        assert_eq!(node.identity_hash, node2.identity_hash);
    }

    #[test]
    fn node_identity_rejects_empty_fields() {
        let err = issue_node_identity(
            "",
            "node-2",
            "sess-root",
            Some("node-1".into()),
            vec!["node-1".into(), "node-2".into()],
            "sha256:assignment",
        )
        .unwrap_err();
        assert_eq!(err.code(), "identity.empty_field");
        let err = issue_node_identity(
            "tree-1",
            "node-2",
            "sess-root",
            Some("node-1".into()),
            vec!["node-1".into(), "node-2".into()],
            "not-a-hash",
        )
        .unwrap_err();
        assert_eq!(err.code(), "identity.invalid");
    }

    #[test]
    fn node_identity_rejects_bad_lineage() {
        // Parent declared but lineage has a single element.
        let err = issue_node_identity(
            "tree-1",
            "node-2",
            "sess-root",
            Some("node-1".into()),
            vec!["node-2".into()],
            "sha256:assignment",
        )
        .unwrap_err();
        assert!(matches!(err, IdentityDeny::BadLineage(_)));
        // Lineage does not end at node_id.
        let err = issue_node_identity(
            "tree-1",
            "node-2",
            "sess-root",
            Some("node-1".into()),
            vec!["node-1".into(), "node-3".into()],
            "sha256:assignment",
        )
        .unwrap_err();
        assert!(matches!(err, IdentityDeny::BadLineage(_)));
        // Root must have single-element lineage.
        let err = issue_node_identity(
            "tree-1",
            "node-1",
            "sess-root",
            None,
            vec!["node-1".into(), "node-2".into()],
            "sha256:assignment",
        )
        .unwrap_err();
        assert!(matches!(err, IdentityDeny::BadLineage(_)));
    }

    #[test]
    fn node_identity_hash_tamper_detected() {
        let mut node = sample_node();
        node.immutable_assignment_hash = "sha256:other".into();
        assert_eq!(
            node.validate().unwrap_err(),
            IdentityDeny::HashMismatch("node identity_hash")
        );
    }

    #[test]
    fn grant_revision_rejects_missing_expiry_and_empty_snapshot() {
        let err = issue_grant_revision(
            1,
            "sha256:snapshot",
            "sha256:manifest",
            "grant-1",
            1,
            "sandbox-1",
            None,
            0,
        )
        .unwrap_err();
        assert_eq!(err.code(), "identity.invalid");
        let err = issue_grant_revision(
            1,
            "",
            "sha256:manifest",
            "grant-1",
            1,
            "sandbox-1",
            None,
            100,
        )
        .unwrap_err();
        assert_eq!(err.code(), "identity.empty_field");
        let err = issue_grant_revision(
            0,
            "sha256:snapshot",
            "sha256:manifest",
            "grant-1",
            1,
            "sandbox-1",
            None,
            100,
        )
        .unwrap_err();
        assert_eq!(err.code(), "identity.invalid");
    }

    #[test]
    fn attempt_rejects_missing_deadline_and_revision_zero() {
        let err = issue_attempt_context("a", "r", "m", None, 0, 1).unwrap_err();
        assert_eq!(err.code(), "identity.invalid");
        let err = issue_attempt_context("a", "r", "m", None, 100, 0).unwrap_err();
        assert_eq!(err.code(), "identity.invalid");
        let err = issue_attempt_context("", "r", "m", None, 100, 1).unwrap_err();
        assert_eq!(err.code(), "identity.empty_field");
    }

    #[test]
    fn envelope_mint_binds_identity_grant_attempt_and_verifies() {
        let node = sample_node();
        let grant = sample_grant();
        let attempt = sample_attempt();
        let envelope = GovernedRunEnvelopeV1::mint(
            "run-1",
            &node,
            &grant,
            &attempt,
            None,
            OperationClass::ReadOnly,
            "evidence://sink",
            1_000,
        )
        .expect("mint");
        envelope.validate().expect("valid");
        envelope
            .verify(&node, &grant, &attempt, 1_500)
            .expect("verify at now < expiry");
    }

    #[test]
    fn envelope_rejects_foreign_identity() {
        let node = sample_node();
        let foreign = issue_node_identity(
            "tree-1",
            "node-3",
            "sess-root",
            Some("node-2".into()),
            vec!["node-1".into(), "node-2".into(), "node-3".into()],
            "sha256:assignment",
        )
        .expect("foreign");
        let grant = sample_grant();
        let attempt = sample_attempt();
        let envelope = GovernedRunEnvelopeV1::mint(
            "run-1",
            &node,
            &grant,
            &attempt,
            None,
            OperationClass::ReadOnly,
            "evidence://sink",
            1_000,
        )
        .expect("mint");
        assert_eq!(
            envelope.verify(&foreign, &grant, &attempt, 1_500).unwrap_err(),
            IdentityDeny::ForeignIdentity
        );
    }

    #[test]
    fn envelope_rejects_stale_grant_revision_and_foreign_attempt() {
        let node = sample_node();
        let grant = sample_grant();
        let stale_attempt = issue_attempt_context("attempt-1", "res-1", "m", None, 2_000_000_000, 2)
            .expect("stale attempt");
        assert_eq!(
            GovernedRunEnvelopeV1::mint(
                "run-1",
                &node,
                &grant,
                &stale_attempt,
                None,
                OperationClass::ReadOnly,
                "evidence://sink",
                1_000,
            )
            .unwrap_err(),
            IdentityDeny::StaleGrantRevision
        );
        let attempt = sample_attempt();
        let envelope = GovernedRunEnvelopeV1::mint(
            "run-1",
            &node,
            &grant,
            &attempt,
            None,
            OperationClass::ReadOnly,
            "evidence://sink",
            1_000,
        )
        .expect("mint");
        let foreign_attempt =
            issue_attempt_context("attempt-9", "res-1", "m", None, 2_000_000_000, 1).expect("fa");
        assert_eq!(
            envelope.verify(&node, &grant, &foreign_attempt, 1_500).unwrap_err(),
            IdentityDeny::ForeignAttempt
        );
    }

    #[test]
    fn envelope_verify_rejects_expired_grant_and_deadline() {
        let node = sample_node();
        let grant = issue_grant_revision(
            1,
            "sha256:snapshot",
            "sha256:manifest",
            "grant-1",
            1,
            "sandbox-1",
            None,
            1_000,
        )
        .expect("short grant");
        let attempt = sample_attempt();
        let envelope = GovernedRunEnvelopeV1::mint(
            "run-1",
            &node,
            &grant,
            &attempt,
            None,
            OperationClass::ReadOnly,
            "evidence://sink",
            100,
        )
        .expect("mint");
        assert_eq!(
            envelope.verify(&node, &grant, &attempt, 2_000).unwrap_err(),
            IdentityDeny::ExpiredGrant
        );
        // Long grant + short deadline: deadline expiry must surface as
        // ExpiredDeadline (grant outlives the attempt).
        let long_grant = sample_grant();
        let deadline_attempt =
            issue_attempt_context("attempt-1", "res-1", "m", None, 500, 1).expect("short attempt");
        let envelope2 = GovernedRunEnvelopeV1::mint(
            "run-1",
            &node,
            &long_grant,
            &deadline_attempt,
            None,
            OperationClass::ReadOnly,
            "evidence://sink",
            100,
        )
        .expect("mint");
        assert_eq!(
            envelope2
                .verify(&node, &long_grant, &deadline_attempt, 2_000)
                .unwrap_err(),
            IdentityDeny::ExpiredDeadline
        );
    }

    #[test]
    fn envelope_rejects_creation_in_the_future() {
        let node = sample_node();
        let grant = sample_grant();
        let attempt = sample_attempt();
        let envelope = GovernedRunEnvelopeV1::mint(
            "run-1",
            &node,
            &grant,
            &attempt,
            None,
            OperationClass::ReadOnly,
            "evidence://sink",
            1_000,
        )
        .expect("mint");
        assert_eq!(
            envelope.verify(&node, &grant, &attempt, 900).unwrap_err(),
            IdentityDeny::EnvelopeCreatedInFuture
        );
    }
}
