//! CheckpointEnvelopeV1 + ObligationV1 — master plan §3.4.2.
//!
//! The governed evidence loop checkpoints travel in a typed envelope
//! (loop kind, tree/node/operation identity, sequence, causal parent,
//! payload hash, encoding revision). Progress is only real when an
//! [`ObligationV1`] moves `Open → Discharged/Refuted` (or a bounded, approved
//! refinement produces new evidence) — never when a model merely repeats
//! work. `progress_fingerprint` alone never defines progress.

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue};

pub const CHECKPOINT_ENVELOPE_SCHEMA_VERSION: u16 = 1;
pub const OBLIGATION_SCHEMA_VERSION: u16 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LoopKind {
    Node,
    Tree,
    Supervisor,
}

impl LoopKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Node => "node",
            Self::Tree => "tree",
            Self::Supervisor => "supervisor",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct CheckpointEnvelopeV1 {
    pub schema_version: u16,
    pub loop_kind: LoopKind,
    pub tree_id: String,
    pub node_or_operation_id: String,
    pub sequence: u64,
    pub causal_parent: Option<u64>,
    /// Canonical hash over the payload fields (excluding the hash itself).
    pub payload_hash: String,
    pub encoding_revision: u16,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ObligationState {
    Open,
    Discharged,
    Refuted,
}

impl ObligationState {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Open => "open",
            Self::Discharged => "discharged",
            Self::Refuted => "refuted",
        }
    }

    pub fn is_terminal(self) -> bool {
        matches!(self, Self::Discharged | Self::Refuted)
    }
}

/// Host-checkable predicate reference (master plan §3.4.2
/// `ObligationV1.predicate`), e.g. `verify:go-test:./...`. The scheme must be
/// one the host can actually evaluate; an obligation whose predicate cannot
/// be checked can never discharge, so parsing fails closed on anything
/// outside the allowlist.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct HostCheckablePredicate(String);

impl HostCheckablePredicate {
    pub fn parse(predicate: impl Into<String>) -> Result<Self, EnvelopeDeny> {
        let predicate = predicate.into();
        if predicate.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("predicate"));
        }
        let scheme_ok = ["verify:", "test:", "check:", "artifact:"]
            .iter()
            .any(|scheme| predicate.starts_with(scheme));
        if !scheme_ok {
            return Err(EnvelopeDeny::Invalid(format!(
                "predicate scheme not host-checkable: {predicate}"
            )));
        }
        Ok(Self(predicate))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl std::fmt::Display for HostCheckablePredicate {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct ObligationV1 {
    pub schema_version: u16,
    pub obligation_id: String,
    /// Host-checkable predicate reference (e.g. `verify:go-test:./...`).
    pub predicate: HostCheckablePredicate,
    pub state: ObligationState,
    pub parent: Option<String>,
    /// Cap on approved refinement iterations before NeedsParentDecision.
    pub approved_refinement_limit: u16,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum EnvelopeDeny {
    Invalid(String),
    EmptyField(&'static str),
    HashMismatch,
    SequenceGap,
    UnknownCausalParent,
    SchemaMismatch,
    TerminalObligation,
    RefinementLimitExceeded,
}

impl EnvelopeDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "checkpoint.invalid",
            Self::EmptyField(_) => "checkpoint.empty_field",
            Self::HashMismatch => "checkpoint.hash_mismatch",
            Self::SequenceGap => "checkpoint.sequence_gap",
            Self::UnknownCausalParent => "checkpoint.unknown_causal_parent",
            Self::SchemaMismatch => "checkpoint.schema_mismatch",
            Self::TerminalObligation => "checkpoint.terminal_obligation",
            Self::RefinementLimitExceeded => "checkpoint.refinement_limit_exceeded",
        }
    }
}

impl std::fmt::Display for EnvelopeDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

fn envelope_preimage(envelope: &CheckpointEnvelopeV1) -> Result<Vec<u8>, CanonicalError> {
    CanonicalRecord::new("checkpoint-envelope")
        .field("schema_version", CanonicalValue::U64(envelope.schema_version as u64))
        .field("loop_kind", CanonicalValue::str(envelope.loop_kind.as_str()))
        .field("tree_id", CanonicalValue::str(&envelope.tree_id))
        .field(
            "node_or_operation_id",
            CanonicalValue::str(&envelope.node_or_operation_id),
        )
        .field("sequence", CanonicalValue::U64(envelope.sequence))
        .field(
            "causal_parent",
            match envelope.causal_parent {
                Some(parent) => CanonicalValue::U64(parent),
                None => CanonicalValue::Null,
            },
        )
        .field(
            "encoding_revision",
            CanonicalValue::U64(envelope.encoding_revision as u64),
        )
        .canonical_bytes()
}

impl CheckpointEnvelopeV1 {
    pub fn build(
        loop_kind: LoopKind,
        tree_id: impl Into<String>,
        node_or_operation_id: impl Into<String>,
        sequence: u64,
        causal_parent: Option<u64>,
        encoding_revision: u16,
    ) -> Result<Self, EnvelopeDeny> {
        let tree_id = tree_id.into();
        let node_or_operation_id = node_or_operation_id.into();
        if tree_id.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("tree_id"));
        }
        if node_or_operation_id.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("node_or_operation_id"));
        }
        let mut envelope = Self {
            schema_version: CHECKPOINT_ENVELOPE_SCHEMA_VERSION,
            loop_kind,
            tree_id,
            node_or_operation_id,
            sequence,
            causal_parent,
            payload_hash: String::new(),
            encoding_revision,
        };
        let hash = envelope_preimage(&envelope)
            .map_err(|e| EnvelopeDeny::Invalid(e.to_string()))?;
        envelope.payload_hash = format!("sha256:{}", blake3::hash(&hash).to_hex());
        envelope.validate()?;
        Ok(envelope)
    }

    pub fn validate(&self) -> Result<(), EnvelopeDeny> {
        if self.schema_version != CHECKPOINT_ENVELOPE_SCHEMA_VERSION {
            return Err(EnvelopeDeny::SchemaMismatch);
        }
        if self.tree_id.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("tree_id"));
        }
        if self.node_or_operation_id.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("node_or_operation_id"));
        }
        let recomputed = envelope_preimage(self)
            .map_err(|e| EnvelopeDeny::Invalid(e.to_string()))?;
        if format!("sha256:{}", blake3::hash(&recomputed).to_hex()) != self.payload_hash {
            return Err(EnvelopeDeny::HashMismatch);
        }
        Ok(())
    }

    /// Append validation: exact next sequence and a known causal parent.
    pub fn validate_append(&self, last_sequence: u64, known_parents: &[u64]) -> Result<(), EnvelopeDeny> {
        self.validate()?;
        if self.sequence != last_sequence.saturating_add(1) {
            return Err(EnvelopeDeny::SequenceGap);
        }
        if let Some(parent) = self.causal_parent {
            if !known_parents.contains(&parent) {
                return Err(EnvelopeDeny::UnknownCausalParent);
            }
        }
        Ok(())
    }
}

impl ObligationV1 {
    pub fn new(
        obligation_id: impl Into<String>,
        predicate: HostCheckablePredicate,
        parent: Option<String>,
        approved_refinement_limit: u16,
    ) -> Result<Self, EnvelopeDeny> {
        let obligation_id = obligation_id.into();
        if obligation_id.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("obligation_id"));
        }
        Ok(Self {
            schema_version: OBLIGATION_SCHEMA_VERSION,
            obligation_id,
            predicate,
            state: ObligationState::Open,
            parent,
            approved_refinement_limit,
        })
    }

    /// `Open → Discharged/Refuted` exactly once; terminal states are immutable.
    pub fn transition(&mut self, to: ObligationState) -> Result<(), EnvelopeDeny> {
        if self.state.is_terminal() {
            return Err(EnvelopeDeny::TerminalObligation);
        }
        if to == ObligationState::Open {
            return Err(EnvelopeDeny::Invalid("open is the initial state only".into()));
        }
        self.state = to;
        Ok(())
    }

    /// Refinements are bounded by the approved limit.
    pub fn authorize_refinement(&self, refinements_used: u16) -> Result<(), EnvelopeDeny> {
        if refinements_used >= self.approved_refinement_limit {
            return Err(EnvelopeDeny::RefinementLimitExceeded);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn envelope_build_validate_roundtrip() {
        let envelope = CheckpointEnvelopeV1::build(
            LoopKind::Node,
            "tree-1",
            "node-1",
            1,
            None,
            1,
        )
        .expect("envelope");
        envelope.validate().expect("valid");
        let same = CheckpointEnvelopeV1::build(LoopKind::Node, "tree-1", "node-1", 1, None, 1)
            .expect("same");
        assert_eq!(envelope.payload_hash, same.payload_hash);
    }

    #[test]
    fn envelope_rejects_missing_fields_and_tamper() {
        let err = CheckpointEnvelopeV1::build(LoopKind::Tree, "", "op-1", 1, None, 1).unwrap_err();
        assert_eq!(err, EnvelopeDeny::EmptyField("tree_id"));
        let mut envelope = CheckpointEnvelopeV1::build(LoopKind::Node, "t", "n", 1, None, 1)
            .expect("envelope");
        envelope.tree_id = "other".into();
        assert_eq!(envelope.validate().unwrap_err(), EnvelopeDeny::HashMismatch);
    }

    #[test]
    fn envelope_append_validates_sequence_and_causal_parent() {
        let first = CheckpointEnvelopeV1::build(LoopKind::Node, "t", "n", 1, None, 1).expect("1");
        first.validate().expect("first valid");
        let second =
            CheckpointEnvelopeV1::build(LoopKind::Node, "t", "n", 3, Some(1), 1).expect("3");
        // Gap: next after 1 must be 2.
        assert_eq!(
            second.validate_append(1, &[1]).unwrap_err(),
            EnvelopeDeny::SequenceGap
        );
        let second =
            CheckpointEnvelopeV1::build(LoopKind::Node, "t", "n", 2, Some(1), 1).expect("2");
        second.validate_append(1, &[1]).expect("append ok");
        // Unknown causal parent.
        let orphan = CheckpointEnvelopeV1::build(LoopKind::Node, "t", "n", 2, Some(99), 1)
            .expect("orphan");
        assert_eq!(
            orphan.validate_append(1, &[1]).unwrap_err(),
            EnvelopeDeny::UnknownCausalParent
        );
    }

    #[test]
    fn obligation_transitions_once_and_refinement_bounded() {
        let predicate = HostCheckablePredicate::parse("verify:go-test:./...").expect("predicate");
        let mut obligation = ObligationV1::new("obl-1", predicate, None, 2).expect("obligation");
        assert_eq!(obligation.state, ObligationState::Open);
        assert_eq!(
            obligation.predicate.as_str(),
            "verify:go-test:./...",
            "host-checkable predicate reference is preserved"
        );
        obligation.authorize_refinement(1).expect("refine 1 ok");
        obligation.authorize_refinement(1).expect("refine 2 ok");
        assert_eq!(
            obligation.authorize_refinement(2).unwrap_err(),
            EnvelopeDeny::RefinementLimitExceeded
        );
        obligation
            .transition(ObligationState::Discharged)
            .expect("discharge");
        assert_eq!(
            obligation.transition(ObligationState::Refuted).unwrap_err(),
            EnvelopeDeny::TerminalObligation,
            "terminal states are immutable"
        );
        let mut obligation =
            ObligationV1::new("obl-2", HostCheckablePredicate::parse("test:unit").expect("p"), None, 1)
                .expect("obligation");
        obligation.transition(ObligationState::Refuted).expect("refute");
        assert!(obligation.state.is_terminal());
    }

    #[test]
    fn host_checkable_predicate_fails_closed_on_unknown_schemes() {
        assert_eq!(
            HostCheckablePredicate::parse("").unwrap_err(),
            EnvelopeDeny::EmptyField("predicate")
        );
        assert_eq!(
            HostCheckablePredicate::parse("model-says-done").unwrap_err().code(),
            "checkpoint.invalid",
            "an unevaluatable predicate can never discharge — fail closed"
        );
        HostCheckablePredicate::parse("check:disk-space").expect("check scheme ok");
        HostCheckablePredicate::parse("artifact:sbom.spdx.json").expect("artifact scheme ok");
    }
}
