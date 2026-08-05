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
/// v2 (2026-08-05, DEBT-028 W0-1): obligations bind `tree_id` (mandatory),
/// `assignment_ref` (node-level obligations) and `discharged_by` (the receipt
/// that adjudicated the obligation). v1 records decode read-only with empty
/// bindings — they are legacy projections, never upgraded in place.
pub const OBLIGATION_SCHEMA_VERSION: u16 = 2;

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
    /// v2: the task tree this obligation belongs to. Empty on decoded v1
    /// legacy records (read-only projection).
    #[serde(default)]
    pub tree_id: String,
    /// v2: the immutable assignment this obligation derives from. Node-level
    /// obligations must carry it; tree-level obligations (parent=None) may not.
    #[serde(default)]
    pub assignment_ref: Option<String>,
    /// Host-checkable predicate reference (e.g. `verify:go-test:./...`).
    pub predicate: HostCheckablePredicate,
    pub state: ObligationState,
    pub parent: Option<String>,
    /// v2: the receipt that adjudicated this obligation (set once on
    /// `Discharged`/`Refuted`; never rewritten).
    #[serde(default)]
    pub discharged_by: Option<String>,
    /// Cap on approved refinement iterations before NeedsParentDecision.
    /// A *resource* limit only — refinement soundness is enforced by
    /// [`validate_conservative_refinement`], not by counting.
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
    MissingDischargeReceipt,
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
            Self::MissingDischargeReceipt => "checkpoint.missing_discharge_receipt",
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
        if let Some(parent) = self.causal_parent
            && !known_parents.contains(&parent)
        {
            return Err(EnvelopeDeny::UnknownCausalParent);
        }
        Ok(())
    }
}

impl ObligationV1 {
    /// v2 constructor. `tree_id` is mandatory; `assignment_ref` is mandatory
    /// for node-level obligations (parent is Some) and optional for
    /// tree-level ones (parent is None).
    pub fn new(
        obligation_id: impl Into<String>,
        tree_id: impl Into<String>,
        predicate: HostCheckablePredicate,
        parent: Option<String>,
        assignment_ref: Option<String>,
        approved_refinement_limit: u16,
    ) -> Result<Self, EnvelopeDeny> {
        let obligation_id = obligation_id.into();
        let tree_id = tree_id.into();
        if obligation_id.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("obligation_id"));
        }
        if tree_id.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("tree_id"));
        }
        if parent.is_some() && assignment_ref.is_none() {
            return Err(EnvelopeDeny::Invalid(
                "node-level obligations must carry an assignment_ref".into(),
            ));
        }
        Ok(Self {
            schema_version: OBLIGATION_SCHEMA_VERSION,
            obligation_id,
            tree_id,
            assignment_ref,
            predicate,
            state: ObligationState::Open,
            parent,
            discharged_by: None,
            approved_refinement_limit,
        })
    }

    pub fn validate(&self) -> Result<(), EnvelopeDeny> {
        if self.schema_version == 1 {
            // v1 legacy projection: readable, never upgraded in place
            // (master plan §0.1.9 read-old semantics).
            return Ok(());
        }
        if self.schema_version != OBLIGATION_SCHEMA_VERSION {
            return Err(EnvelopeDeny::SchemaMismatch);
        }
        if self.tree_id.trim().is_empty() {
            return Err(EnvelopeDeny::EmptyField("tree_id"));
        }
        if self.parent.is_some() && self.assignment_ref.is_none() {
            return Err(EnvelopeDeny::Invalid(
                "node-level obligations must carry an assignment_ref".into(),
            ));
        }
        if self.state.is_terminal() && self.discharged_by.is_none() {
            return Err(EnvelopeDeny::MissingDischargeReceipt);
        }
        Ok(())
    }

    /// `Open → Discharged/Refuted` exactly once; terminal states are
    /// immutable. Terminal transitions require the adjudicating receipt ref.
    pub fn transition(
        &mut self,
        to: ObligationState,
        receipt_ref: Option<&str>,
    ) -> Result<(), EnvelopeDeny> {
        if self.state.is_terminal() {
            return Err(EnvelopeDeny::TerminalObligation);
        }
        if to == ObligationState::Open {
            return Err(EnvelopeDeny::Invalid("open is the initial state only".into()));
        }
        let receipt = match receipt_ref {
            Some(r) if !r.trim().is_empty() => r.to_string(),
            _ => return Err(EnvelopeDeny::MissingDischargeReceipt),
        };
        self.discharged_by = Some(receipt);
        self.state = to;
        Ok(())
    }

    /// Refinements are bounded by the approved limit — a *resource* limit
    /// that prevents unbounded splitting; soundness comes from
    /// [`validate_conservative_refinement`], not from counting.
    pub fn authorize_refinement(&self, refinements_used: u16) -> Result<(), EnvelopeDeny> {
        if refinements_used >= self.approved_refinement_limit {
            return Err(EnvelopeDeny::RefinementLimitExceeded);
        }
        Ok(())
    }
}

/// Stable canonical hash of a host-checkable predicate, used to prove that a
/// refinement never replaces the parent predicate.
pub fn predicate_hash(predicate: &HostCheckablePredicate) -> String {
    format!("sha256:{}", blake3::hash(predicate.as_str().as_bytes()).to_hex())
}

/// One approved conservative refinement (master plan §NG-04E, DEBT-028 W0-2).
///
/// The only legal form of `refine(P → {C₁..Cₙ})`:
/// - `P` never leaves the obligation set — it must still be `Open` and its
///   predicate must be unchanged (`parent_predicate_hash`);
/// - the children are *strategies*, not replacements — each child's `parent`
///   must point back at `P`;
/// - `P`'s discharge still requires `P`'s own predicate to be adjudicated.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct ObligationRefinementV1 {
    pub parent: String,
    pub children: Vec<String>,
    pub parent_predicate_hash: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RefinementDeny {
    /// The parent obligation is already terminal — it left the set.
    ParentRemoved,
    /// The parent predicate hash does not match the recorded one — the
    /// parent was replaced by the refinement.
    ParentPredicateReplaced,
    /// A child's `parent` field does not point at the refined parent.
    OrphanChild,
    /// The parent lists itself as its own child.
    ChildIsParent,
    /// Duplicate child ids in the refinement record.
    DuplicateChild,
}

impl RefinementDeny {
    pub fn code(self) -> &'static str {
        match self {
            Self::ParentRemoved => "checkpoint.refinement.parent_removed",
            Self::ParentPredicateReplaced => "checkpoint.refinement.parent_predicate_replaced",
            Self::OrphanChild => "checkpoint.refinement.orphan_child",
            Self::ChildIsParent => "checkpoint.refinement.child_is_parent",
            Self::DuplicateChild => "checkpoint.refinement.duplicate_child",
        }
    }
}

impl std::fmt::Display for RefinementDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.code())
    }
}

/// Pure enforcement of conservative refinement. The parent obligation stays
/// in the set with an unchanged predicate; children are strategies that point
/// back at it. Splitting can never escape the parent predicate.
pub fn validate_conservative_refinement(
    parent: &ObligationV1,
    children: &[ObligationV1],
    record: &ObligationRefinementV1,
) -> Result<(), RefinementDeny> {
    if parent.state.is_terminal() {
        return Err(RefinementDeny::ParentRemoved);
    }
    if predicate_hash(&parent.predicate) != record.parent_predicate_hash {
        return Err(RefinementDeny::ParentPredicateReplaced);
    }
    if record.children.iter().any(|c| c == &parent.obligation_id) {
        return Err(RefinementDeny::ChildIsParent);
    }
    let mut seen: Vec<&str> = Vec::new();
    for child in children {
        if seen.contains(&child.obligation_id.as_str()) {
            return Err(RefinementDeny::DuplicateChild);
        }
        seen.push(&child.obligation_id);
        if child.parent.as_deref() != Some(parent.obligation_id.as_str()) {
            return Err(RefinementDeny::OrphanChild);
        }
        if child.state.is_terminal() {
            // A strategy may already be discharged; that is fine. Only the
            // parent's presence and predicate are invariant here.
        }
    }
    if record.children.len() != children.len() {
        return Err(RefinementDeny::OrphanChild);
    }
    for child_id in &record.children {
        if !children.iter().any(|c| &c.obligation_id == child_id) {
            return Err(RefinementDeny::OrphanChild);
        }
    }
    Ok(())
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
        let mut obligation =
            ObligationV1::new("obl-1", "tree-1", predicate, None, None, 2).expect("obligation");
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
            .transition(
                ObligationState::Discharged,
                Some("verify:go-test:./...:PASS:receipt-1"),
            )
            .expect("discharge");
        assert_eq!(
            obligation.discharged_by.as_deref(),
            Some("verify:go-test:./...:PASS:receipt-1"),
            "the adjudicating receipt is bound exactly once"
        );
        assert_eq!(
            obligation.transition(ObligationState::Refuted, None).unwrap_err(),
            EnvelopeDeny::TerminalObligation,
            "terminal states are immutable"
        );
        let mut obligation = ObligationV1::new(
            "obl-2",
            "tree-1",
            HostCheckablePredicate::parse("test:unit").expect("p"),
            None,
            None,
            1,
        )
        .expect("obligation");
        obligation
            .transition(ObligationState::Refuted, Some("test:unit:FAIL:receipt-2"))
            .expect("refute");
        assert!(obligation.state.is_terminal());
    }

    #[test]
    fn obligation_v2_binds_tree_assignment_and_receipt() {
        // v2 construction: tree_id mandatory; node-level obligations (with a
        // parent) must carry an assignment_ref.
        let err = ObligationV1::new(
            "obl-x",
            "",
            HostCheckablePredicate::parse("verify:x").expect("p"),
            None,
            None,
            1,
        )
        .unwrap_err();
        assert_eq!(err, EnvelopeDeny::EmptyField("tree_id"));
        let err = ObligationV1::new(
            "obl-x",
            "tree-1",
            HostCheckablePredicate::parse("verify:x").expect("p"),
            Some("obl-parent".into()),
            None,
            1,
        )
        .unwrap_err();
        assert_eq!(
            err.code(),
            "checkpoint.invalid",
            "node-level obligations must carry an assignment_ref"
        );
        let mut obligation = ObligationV1::new(
            "obl-x",
            "tree-1",
            HostCheckablePredicate::parse("verify:x").expect("p"),
            Some("obl-parent".into()),
            Some("assignment-9".into()),
            1,
        )
        .expect("node obligation");
        obligation.validate().expect("v2 valid");
        // Terminal transition without a receipt is fail-closed.
        assert_eq!(
            obligation
                .transition(ObligationState::Discharged, None)
                .unwrap_err(),
            EnvelopeDeny::MissingDischargeReceipt
        );
        // Empty-string receipt is treated as missing.
        assert_eq!(
            obligation
                .transition(ObligationState::Discharged, Some(""))
                .unwrap_err(),
            EnvelopeDeny::MissingDischargeReceipt
        );
        obligation
            .transition(ObligationState::Discharged, Some("verify:x:PASS:r"))
            .expect("discharge with receipt");
        // The discharged_by binding is immutable.
        assert_eq!(
            obligation
                .transition(ObligationState::Refuted, Some("verify:x:FAIL:r2"))
                .unwrap_err(),
            EnvelopeDeny::TerminalObligation
        );
    }

    #[test]
    fn obligation_v1_legacy_decodes_read_only() {
        // v1 JSON (no tree_id / assignment_ref / discharged_by fields) must
        // decode as a read-only legacy projection, never upgraded in place.
        let legacy = r#"{
            "schema_version": 1,
            "obligation_id": "obl-legacy",
            "predicate": "verify:go-test:./...",
            "state": "open",
            "parent": null,
            "approved_refinement_limit": 2
        }"#;
        let obligation: ObligationV1 = serde_json::from_str(legacy).expect("v1 decode");
        assert_eq!(obligation.schema_version, 1);
        assert!(obligation.tree_id.is_empty(), "v1 legacy has no tree binding");
        assert_eq!(obligation.discharged_by, None);
        obligation.validate().expect("v1 read-only projection is valid");
        // v1 records cannot enter unattended loops (schema check).
        let mut upgraded = obligation.clone();
        upgraded.schema_version = OBLIGATION_SCHEMA_VERSION;
        assert_eq!(
            upgraded.validate().unwrap_err(),
            EnvelopeDeny::EmptyField("tree_id"),
            "a v1 record cannot masquerade as v2"
        );
    }

    #[test]
    fn conservative_refinement_keeps_parent_in_set() {
        let predicate = HostCheckablePredicate::parse("verify:go-test:./...").expect("p");
        let parent = ObligationV1::new("obl-p", "tree-1", predicate.clone(), None, None, 3)
            .expect("parent");
        let child_a = ObligationV1::new(
            "obl-a",
            "tree-1",
            HostCheckablePredicate::parse("test:unit-a").expect("a"),
            Some("obl-p".into()),
            Some("assignment-1".into()),
            1,
        )
        .expect("child a");
        let child_b = ObligationV1::new(
            "obl-b",
            "tree-1",
            HostCheckablePredicate::parse("test:unit-b").expect("b"),
            Some("obl-p".into()),
            Some("assignment-1".into()),
            1,
        )
        .expect("child b");
        let record = ObligationRefinementV1 {
            parent: "obl-p".into(),
            children: vec!["obl-a".into(), "obl-b".into()],
            parent_predicate_hash: predicate_hash(&parent.predicate),
        };
        validate_conservative_refinement(&parent, &[child_a, child_b], &record)
            .expect("conservative refinement is legal");
    }

    #[test]
    fn conservative_refinement_fails_closed() {
        let predicate = HostCheckablePredicate::parse("verify:go-test:./...").expect("p");
        let mut parent = ObligationV1::new("obl-p", "tree-1", predicate.clone(), None, None, 3)
            .expect("parent");
        let child = ObligationV1::new(
            "obl-a",
            "tree-1",
            HostCheckablePredicate::parse("test:unit-a").expect("a"),
            Some("obl-p".into()),
            Some("assignment-1".into()),
            1,
        )
        .expect("child");
        let record = ObligationRefinementV1 {
            parent: "obl-p".into(),
            children: vec!["obl-a".into()],
            parent_predicate_hash: predicate_hash(&parent.predicate),
        };
        // 1. Parent removed: the parent must still be Open in the set.
        parent
            .transition(ObligationState::Discharged, Some("verify:go-test:./...:PASS:r"))
            .expect("discharge");
        assert_eq!(
            validate_conservative_refinement(&parent, &[child.clone()], &record).unwrap_err(),
            RefinementDeny::ParentRemoved
        );
        // 2. Parent predicate replaced (fresh Open parent, tampered hash).
        let mut open_parent =
            ObligationV1::new("obl-p", "tree-1", predicate.clone(), None, None, 3).expect("p");
        let replaced = ObligationRefinementV1 {
            parent: "obl-p".into(),
            children: vec!["obl-a".into()],
            parent_predicate_hash: "sha256:deadbeef".into(),
        };
        assert_eq!(
            validate_conservative_refinement(&open_parent, &[child.clone()], &replaced)
                .unwrap_err(),
            RefinementDeny::ParentPredicateReplaced
        );
        // 3. Orphan child: child.parent does not point at the parent.
        let mut open_parent =
            ObligationV1::new("obl-p", "tree-1", predicate.clone(), None, None, 3).expect("p");
        let orphan = ObligationV1::new(
            "obl-orphan",
            "tree-1",
            HostCheckablePredicate::parse("test:unit-orphan").expect("o"),
            Some("obl-elsewhere".into()),
            Some("assignment-1".into()),
            1,
        )
        .expect("orphan");
        assert_eq!(
            validate_conservative_refinement(&open_parent, &[orphan], &record).unwrap_err(),
            RefinementDeny::OrphanChild
        );
        // 4. Child is the parent itself.
        open_parent = ObligationV1::new("obl-p", "tree-1", predicate, None, None, 3).expect("p");
        let self_ref = ObligationRefinementV1 {
            parent: "obl-p".into(),
            children: vec!["obl-p".into()],
            parent_predicate_hash: predicate_hash(&open_parent.predicate),
        };
        assert_eq!(
            validate_conservative_refinement(&open_parent, &[open_parent.clone()], &self_ref)
                .unwrap_err(),
            RefinementDeny::ChildIsParent
        );
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
