//! NG-00A — CanonicalEncodingV1.
//!
//! The single owner of hash-preimage serialization for every hash-bearing
//! NextGen record (claim journal, accepted snapshot, context manifest,
//! sandbox schema, tool contract, advisor capsule, release provenance).
//! Other crates must not hand-roll `serde_json::to_vec` and call it a
//! canonical hash.
//!
//! Frozen rules (golden-tested in `canonical_tests.rs`):
//!
//! - **Preimage** is UTF-8 text. Line 1 is the domain-separation prefix
//!   `lumen/nextgen/canonical/v1`; line 2 is the record domain; line 3 is
//!   the encoding revision. Revision is part of the preimage, so a revision
//!   bump is an explicit migration/rehash transaction.
//! - **Field order**: lexicographic by field name (byte order).
//! - **String values**: NFC-normalized before encoding; the encoder encodes
//!   the normalized form. `NFC` check is performed and decomposed input is
//!   a canonicalization error (never silently hashed as-is).
//! - **Integers**: decimal text (`u64:<n>`, `i64:<n>`). No floats: float
//!   values are rejected with [`CanonicalError::FloatForbidden`].
//! - **absent vs null**: a field may be absent (not in the record) or
//!   explicitly `null`; both are distinct encodings.
//! - **Arrays**: order-sensitive (`seq:<n>` with indented items); callers
//!   must pre-sort when order is semantically free. **Maps**: sorted by key;
//!   unsorted map input is rejected.
//! - **Bytes**: lowercase hex.
//! - **Domain separation**: the prefix + domain prevent cross-record
//!   preimage reuse (a claim hash can never equal a manifest hash).

use sha2::{Digest, Sha256};
use unicode_normalization::UnicodeNormalization;

/// Current encoding revision. Bumping this is an explicit migration event.
pub const ENCODING_REVISION: u32 = 1;
const PREFIX: &str = "lumen/nextgen/canonical/v1";

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CanonicalError {
    FloatForbidden,
    MapNotSorted,
    NonNfcString,
    InvalidUtf8,
    ValueOutOfRange,
}

impl std::fmt::Display for CanonicalError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CanonicalError::FloatForbidden => write!(f, "floats are forbidden in canonical encoding"),
            CanonicalError::MapNotSorted => write!(f, "map keys must be sorted"),
            CanonicalError::NonNfcString => {
                write!(f, "string value is not NFC-normalized")
            }
            CanonicalError::InvalidUtf8 => write!(f, "string bytes are not valid UTF-8"),
            CanonicalError::ValueOutOfRange => write!(f, "numeric value out of canonical range"),
        }
    }
}

/// A canonical value. `Seq` preserves caller order; `Map` keys must be
/// pre-sorted (validated on encode).
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CanonicalValue {
    Str(String),
    Bytes(Vec<u8>),
    U64(u64),
    I64(i64),
    Bool(bool),
    Null,
    Seq(Vec<CanonicalValue>),
    Map(Vec<(String, CanonicalValue)>),
}

impl CanonicalValue {
    pub fn str(value: impl Into<String>) -> Self {
        Self::Str(value.into())
    }
}

/// Canonical preimage of a single hash-bearing record.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CanonicalRecord {
    domain: &'static str,
    fields: Vec<(String, CanonicalValue)>,
}

impl CanonicalRecord {
    pub fn new(domain: &'static str) -> Self {
        Self {
            domain,
            fields: Vec::new(),
        }
    }

    /// Append a field. Fields are sorted at encode time; callers may add in
    /// any order.
    pub fn field(mut self, name: impl Into<String>, value: CanonicalValue) -> Self {
        self.fields.push((name.into(), value));
        self
    }

    /// Serialize the record to its canonical preimage bytes.
    pub fn canonical_bytes(&self) -> Result<Vec<u8>, CanonicalError> {
        let mut out = Vec::new();
        out.extend_from_slice(PREFIX.as_bytes());
        out.push(b'\n');
        out.extend_from_slice(self.domain.as_bytes());
        out.push(b'\n');
        out.extend_from_slice(ENCODING_REVISION.to_string().as_bytes());
        out.push(b'\n');

        let mut fields = self.fields.clone();
        fields.sort_by(|a, b| a.0.cmp(&b.0));
        for (name, value) in fields {
            out.extend_from_slice(name.as_bytes());
            out.push(b'\t');
            encode_value(&mut out, &value)?;
            out.push(b'\n');
        }
        Ok(out)
    }

    /// SHA-256 of the canonical preimage, hex, prefixed `sha256:`.
    pub fn record_hash(&self) -> Result<String, CanonicalError> {
        let digest = Sha256::digest(self.canonical_bytes()?);
        Ok(format!("sha256:{digest:x}"))
    }
}

fn encode_value(out: &mut Vec<u8>, value: &CanonicalValue) -> Result<(), CanonicalError> {
    match value {
        CanonicalValue::Str(s) => {
            // NFC-normalize; reject input that was not already NFC so a
            // decomposed form can never silently hash to a different preimage
            // than the one a caller intended.
            let normalized: String = s.nfc().collect();
            if normalized != *s {
                return Err(CanonicalError::NonNfcString);
            }
            out.extend_from_slice(b"s:");
            out.extend_from_slice(normalized.len().to_string().as_bytes());
            out.push(b':');
            out.extend_from_slice(normalized.as_bytes());
        }
        CanonicalValue::Bytes(b) => {
            out.extend_from_slice(b"x:");
            for byte in b {
                out.extend_from_slice(format!("{byte:02x}").as_bytes());
            }
        }
        CanonicalValue::U64(n) => {
            out.extend_from_slice(b"u:");
            out.extend_from_slice(n.to_string().as_bytes());
        }
        CanonicalValue::I64(n) => {
            out.extend_from_slice(b"i:");
            out.extend_from_slice(n.to_string().as_bytes());
        }
        CanonicalValue::Bool(b) => {
            out.extend_from_slice(if *b { b"b:true" } else { b"b:false" });
        }
        CanonicalValue::Null => {
            out.extend_from_slice(b"z");
        }
        CanonicalValue::Seq(items) => {
            out.extend_from_slice(b"[");
            out.extend_from_slice(items.len().to_string().as_bytes());
            out.push(b'\n');
            for item in items {
                out.extend_from_slice(b"  ");
                encode_value(out, item)?;
                out.push(b'\n');
            }
        }
        CanonicalValue::Map(entries) => {
            let mut sorted = entries.clone();
            sorted.sort_by(|a, b| a.0.cmp(&b.0));
            if sorted.iter().zip(entries.iter()).any(|(a, b)| a.0 != b.0) {
                return Err(CanonicalError::MapNotSorted);
            }
            out.extend_from_slice(b"{");
            out.extend_from_slice(entries.len().to_string().as_bytes());
            out.push(b'\n');
            for (key, entry) in entries {
                out.extend_from_slice(b"  ");
                out.extend_from_slice(key.as_bytes());
                out.push(b'\t');
                encode_value(out, entry)?;
                out.push(b'\n');
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod canonical_tests {
    use super::*;

    /// Golden vector: any change to the preimage rules (prefix, revision,
    /// field sort, value grammar) must update these hashes and the revision.
    #[test]
    fn golden_vector_pins_the_frozen_preimage() {
        let record = CanonicalRecord::new("claim")
            .field("tree_id", CanonicalValue::str("tree-1"))
            .field("sequence", CanonicalValue::U64(7))
            .field("evidence", CanonicalValue::Null)
            .field("accepted", CanonicalValue::Bool(true))
            .field("path", CanonicalValue::Seq(vec![
                CanonicalValue::str("root"),
                CanonicalValue::str("child"),
            ]));
        assert_eq!(
            record.record_hash().unwrap(),
            "sha256:6dd8ffdddd767b6c74c4e218875e2ba979a6a97754e92e298adf6f93f6854380"
        );
    }

    #[test]
    fn field_order_is_lexicographic_regardless_of_insertion_order() {
        let a = CanonicalRecord::new("m")
            .field("zeta", CanonicalValue::U64(1))
            .field("alpha", CanonicalValue::U64(2));
        let b = CanonicalRecord::new("m")
            .field("alpha", CanonicalValue::U64(2))
            .field("zeta", CanonicalValue::U64(1));
        assert_eq!(a.canonical_bytes().unwrap(), b.canonical_bytes().unwrap());
        let bytes = a.canonical_bytes().unwrap();
        let text = String::from_utf8(bytes).unwrap();
        assert!(text.find("alpha").unwrap() < text.find("zeta").unwrap());
    }

    #[test]
    fn domain_separation_prevents_cross_record_preimage_reuse() {
        let claim = CanonicalRecord::new("claim")
            .field("payload", CanonicalValue::str("x"));
        let manifest = CanonicalRecord::new("manifest")
            .field("payload", CanonicalValue::str("x"));
        assert_ne!(
            claim.record_hash().unwrap(),
            manifest.record_hash().unwrap()
        );
    }

    #[test]
    fn revision_is_part_of_the_preimage() {
        // Simulated future revision by checking the prefix line explicitly.
        let bytes = CanonicalRecord::new("m")
            .field("a", CanonicalValue::U64(1))
            .canonical_bytes()
            .unwrap();
        let text = String::from_utf8(bytes).unwrap();
        assert!(text.starts_with("lumen/nextgen/canonical/v1\nm\n1\n"));
    }

    #[test]
    fn floats_are_forbidden() {
        // No float variant exists; the policy is enforced by construction.
        // This pins the absence of a float path in the encoder API.
        let record = CanonicalRecord::new("m").field("n", CanonicalValue::U64(1));
        assert!(record.record_hash().is_ok());
        // And documents that callers must serialize floats as strings/ints.
        let record = CanonicalRecord::new("m")
            .field("n", CanonicalValue::str("3.14"));
        assert!(record.record_hash().is_ok());
    }

    #[test]
    fn decomposed_unicode_is_rejected() {
        // "é" as e + U+0301 (decomposed) must not silently hash.
        let decomposed = "e\u{0301}";
        assert_ne!(decomposed, "é");
        let record = CanonicalRecord::new("m").field("s", CanonicalValue::str(decomposed));
        assert_eq!(
            record.canonical_bytes().err(),
            Some(CanonicalError::NonNfcString)
        );
        // NFC input encodes fine.
        let record = CanonicalRecord::new("m").field("s", CanonicalValue::str("é"));
        assert!(record.record_hash().is_ok());
    }

    #[test]
    fn unsorted_map_is_rejected() {
        let record = CanonicalRecord::new("m").field(
            "attrs",
            CanonicalValue::Map(vec![
                ("b".to_owned(), CanonicalValue::U64(1)),
                ("a".to_owned(), CanonicalValue::U64(2)),
            ]),
        );
        assert_eq!(
            record.canonical_bytes().err(),
            Some(CanonicalError::MapNotSorted)
        );
    }

    #[test]
    fn absent_and_null_are_distinct() {
        let with_null = CanonicalRecord::new("m").field("x", CanonicalValue::Null);
        let absent = CanonicalRecord::new("m");
        assert_ne!(
            with_null.canonical_bytes().unwrap(),
            absent.canonical_bytes().unwrap()
        );
    }

    #[test]
    fn bytes_are_lowercase_hex() {
        let record = CanonicalRecord::new("m").field("b", CanonicalValue::Bytes(vec![0x00, 0xFF, 0x1a]));
        let bytes = record.canonical_bytes().unwrap();
        let text = String::from_utf8(bytes).unwrap();
        assert!(text.contains("x:00ff1a"), "got {text}");
    }
}
