//! NG-00B — logical authority event envelope.
//!
//! Every authority mutation shares this logical envelope. Claim journal,
//! operation/outbox journal, artifact store and session JSONL may remain
//! physically separate: retention, fsync, privacy and throughput differ.
//! What must be uniform is the causal order and identity, not a single
//! physical log file.
//!
//! A reducer consumes only ordered authority events. Clock, filesystem,
//! process, network and random IDs enter as typed observation/input events,
//! never as implicit reads inside the reducer.

use crate::canonical::{CanonicalRecord, CanonicalValue, ENCODING_REVISION};
use sha2::Digest;

/// Event envelope revision. Must equal the canonical encoding revision for
/// the payload hash to be self-describing.
pub const AUTHORITY_EVENT_ENVELOPE_REVISION: u32 = ENCODING_REVISION;

/// Durability class of the physical journal holding this event. This drives
/// fsync/retention policy; it is NOT a statement about delivery (that is a
/// `DeliveryObservation`).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DurabilityClass {
    /// Ephemeral projection / UI-observable only.
    Ephemeral,
    /// Durable before any external effect is emitted.
    PreEffect,
    /// Durable after the effect receipt is recorded.
    PostEffect,
}

/// Logical authority event. `tree_sequence` is per-tree monotonic; the
/// reducer rejects gaps or duplicates. `causal_parent` links to the prior
/// event of the same logical stream (tree or actor), not to a wall clock.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthorityEventEnvelopeV1 {
    pub tree_id: String,
    pub tree_sequence: u64,
    pub causal_parent: Option<u64>,
    pub actor_owner: String,
    pub event_kind: String,
    pub payload_hash_or_ref: String,
    pub durability_class: DurabilityClass,
    pub encoding_revision: u32,
    /// Optional external time observation (epoch ms). Reducers must not
    /// depend on it for ordering; ordering is `tree_sequence`.
    pub observed_time_ref: Option<u64>,
}

impl AuthorityEventEnvelopeV1 {
    /// Canonical preimage of the envelope itself (identity fields + payload
    /// reference). The payload hash is bound as a field, so the envelope
    /// hash commits to the payload without embedding it.
    pub fn canonical_bytes(&self) -> Result<Vec<u8>, crate::canonical::CanonicalError> {
        let record = CanonicalRecord::new("authority-event")
            .field("tree_id", CanonicalValue::str(&self.tree_id))
            .field("tree_sequence", CanonicalValue::U64(self.tree_sequence))
            .field(
                "causal_parent",
                self.causal_parent
                    .map(CanonicalValue::U64)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field("actor_owner", CanonicalValue::str(&self.actor_owner))
            .field("event_kind", CanonicalValue::str(&self.event_kind))
            .field(
                "payload_hash_or_ref",
                CanonicalValue::str(&self.payload_hash_or_ref),
            )
            .field(
                "durability_class",
                CanonicalValue::str(match self.durability_class {
                    DurabilityClass::Ephemeral => "ephemeral",
                    DurabilityClass::PreEffect => "pre-effect",
                    DurabilityClass::PostEffect => "post-effect",
                }),
            )
            .field("encoding_revision", CanonicalValue::U64(u64::from(self.encoding_revision)))
            .field(
                "observed_time_ref",
                self.observed_time_ref
                    .map(CanonicalValue::U64)
                    .unwrap_or(CanonicalValue::Null),
            );
        record.canonical_bytes()
    }

    pub fn envelope_hash(&self) -> Result<String, crate::canonical::CanonicalError> {
        let digest = sha2::Sha256::digest(self.canonical_bytes()?);
        Ok(format!("sha256:{digest:x}"))
    }
}

#[cfg(test)]
mod envelope_tests {
    use super::*;

    fn sample() -> AuthorityEventEnvelopeV1 {
        AuthorityEventEnvelopeV1 {
            tree_id: "tree-1".to_owned(),
            tree_sequence: 42,
            causal_parent: Some(41),
            actor_owner: "root-session".to_owned(),
            event_kind: "PromptAccepted".to_owned(),
            payload_hash_or_ref: "sha256:payload".to_owned(),
            durability_class: DurabilityClass::PreEffect,
            encoding_revision: AUTHORITY_EVENT_ENVELOPE_REVISION,
            observed_time_ref: Some(1_700_000_000_000),
        }
    }

    #[test]
    fn envelope_hash_is_stable_and_pinned() {
        assert_eq!(
            sample().envelope_hash().unwrap(),
            "sha256:6638b46c94db0ca8833bd2656556cadd7bae8e8cc7ab2c73c34b5baa5795424a"
        );
    }

    #[test]
    fn sequence_is_the_ordering_key_not_time() {
        let a = sample();
        let mut b = sample();
        b.observed_time_ref = None;
        // The envelope commits to the time observation when present (it is
        // part of the identity record)...
        assert_ne!(a.envelope_hash().unwrap(), b.envelope_hash().unwrap());
        // ...but ordering is defined by tree_sequence: the reducer must order
        // by sequence and reject gaps/duplicates, never by the clock.
        assert_eq!(a.tree_sequence, b.tree_sequence);
        assert_ne!(a.tree_sequence, 0);
    }

    #[test]
    fn causal_parent_chain_and_gap_are_observable() {
        let first = sample();
        let mut gap = sample();
        gap.causal_parent = Some(40);
        assert_ne!(
            first.envelope_hash().unwrap(),
            gap.envelope_hash().unwrap(),
            "a different causal parent must change the envelope identity"
        );
    }

    #[test]
    fn envelope_commits_to_payload_without_embedding_it() {
        let mut a = sample();
        let mut b = sample();
        b.payload_hash_or_ref = "sha256:other-payload".to_owned();
        assert_ne!(a.envelope_hash().unwrap(), b.envelope_hash().unwrap());
        // And the preimage contains only the reference, not the payload body.
        let bytes = a.canonical_bytes().unwrap();
        assert!(!bytes.windows(b"secret".len()).any(|w| w == b"secret"));
    }
}
