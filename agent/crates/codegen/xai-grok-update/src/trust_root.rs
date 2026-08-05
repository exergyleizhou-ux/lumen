//! TrustRootSet — DEBT-028 W2c-1: dual-key rotation for the update trust
//! root.
//!
//! A single pinned fingerprint (NG-10D v1) has a hard failure mode: on key
//! compromise or rotation, every already-shipped binary permanently loses
//! automatic upgrade. This module fixes that with a versioned root set:
//!
//! - the updater accepts a signature from ANY currently valid root;
//! - within a rotation window the new release is dual-signed (old key +
//!   new key);
//! - `set_revision` bumps are themselves signed by a currently valid root,
//!   so an attacker cannot inject their own root.
//!
//! Pure and offline: key material is represented by key ids + public-key
//! strings; signature verification itself stays in the platform/minisign
//! layer. Everything here is the policy that layer must enforce.

use serde::{Deserialize, Serialize};
use thiserror::Error;

pub const TRUST_ROOT_SET_SCHEMA_V1: &str = "lumen.update.trust_root_set.v1";

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TrustRoot {
    pub key_id: String,
    /// Minisign public-key representation (or equivalent pinned encoding).
    pub public_key: String,
    pub valid_from_unix: u64,
    /// None = valid indefinitely.
    pub valid_until_unix: Option<u64>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TrustRootSet {
    pub schema: String,
    pub set_revision: u32,
    pub roots: Vec<TrustRoot>,
}

#[derive(Debug, Clone, PartialEq, Eq, Error)]
pub enum TrustRootDeny {
    #[error("trust root set has an empty key_id")]
    EmptyKeyId,
    #[error("trust root {key_id} has an empty public key")]
    EmptyPublicKey { key_id: String },
    #[error("trust root {key_id} validity range is inverted (until <= from)")]
    InvertedValidity { key_id: String },
    #[error("duplicate key_id {key_id} in the root set")]
    DuplicateKeyId { key_id: String },
    #[error("no valid root exists at the given time")]
    NoValidRoot,
    #[error("signer {key_id} is not a currently valid root")]
    UnknownSigner { key_id: String },
    #[error("set update must be signed by a currently valid root, got none of {signers:?}")]
    UpdateNotSignedByCurrentRoot { signers: Vec<String> },
    #[error("rotation changes the key set and must be dual-signed (old + new), got {signers:?}")]
    RotationNotDualSigned { signers: Vec<String> },
    #[error("set revision must increase, got {next} after {current}")]
    RevisionNotMonotonic { current: u32, next: u32 },
}

impl TrustRoot {
    pub fn is_valid_at(&self, now_unix: u64) -> bool {
        self.valid_from_unix <= now_unix
            && self
                .valid_until_unix
                .map_or(true, |until| now_unix < until)
    }
}

impl TrustRootSet {
    pub fn new(set_revision: u32, roots: Vec<TrustRoot>) -> Result<Self, TrustRootDeny> {
        let set = Self {
            schema: TRUST_ROOT_SET_SCHEMA_V1.to_string(),
            set_revision,
            roots,
        };
        set.validate()?;
        Ok(set)
    }

    pub fn validate(&self) -> Result<(), TrustRootDeny> {
        if self.schema != TRUST_ROOT_SET_SCHEMA_V1 {
            // Unknown schema → frozen, never best-effort.
            return Err(TrustRootDeny::NoValidRoot);
        }
        let mut seen: Vec<&str> = Vec::new();
        for root in &self.roots {
            if root.key_id.trim().is_empty() {
                return Err(TrustRootDeny::EmptyKeyId);
            }
            if root.public_key.trim().is_empty() {
                return Err(TrustRootDeny::EmptyPublicKey {
                    key_id: root.key_id.clone(),
                });
            }
            if root
                .valid_until_unix
                .is_some_and(|until| until <= root.valid_from_unix)
            {
                return Err(TrustRootDeny::InvertedValidity {
                    key_id: root.key_id.clone(),
                });
            }
            if seen.contains(&root.key_id.as_str()) {
                return Err(TrustRootDeny::DuplicateKeyId {
                    key_id: root.key_id.clone(),
                });
            }
            seen.push(&root.key_id);
        }
        Ok(())
    }

    /// The updater accepts a signature whose key is ANY currently valid
    /// root in this set.
    pub fn accepts(&self, signer_key_id: &str, now_unix: u64) -> Result<(), TrustRootDeny> {
        self.validate()?;
        for root in &self.roots {
            if root.key_id == signer_key_id && root.is_valid_at(now_unix) {
                return Ok(());
            }
        }
        Err(TrustRootDeny::UnknownSigner {
            key_id: signer_key_id.to_string(),
        })
    }

    pub fn key_ids(&self) -> Vec<String> {
        self.roots.iter().map(|r| r.key_id.clone()).collect()
    }
}

/// Validate a `set_revision` update (DEBT-028 W2c-1 rules):
///
/// 1. the revision must increase monotonically;
/// 2. the update must be signed by a currently valid root of the CURRENT set
///    (no attacker-injected root);
/// 3. if the key set changes (rotation), the new release must be dual-signed
///    by both a currently valid old root and a valid root of the new set.
pub fn validate_set_update(
    current: &TrustRootSet,
    next: &TrustRootSet,
    signers: &[&str],
    now_unix: u64,
) -> Result<(), TrustRootDeny> {
    current.validate()?;
    next.validate()?;
    if next.set_revision <= current.set_revision {
        return Err(TrustRootDeny::RevisionNotMonotonic {
            current: current.set_revision,
            next: next.set_revision,
        });
    }
    let current_valid_signer = signers.iter().any(|key| {
        current
            .roots
            .iter()
            .any(|r| &r.key_id == key && r.is_valid_at(now_unix))
    });
    if !current_valid_signer {
        return Err(TrustRootDeny::UpdateNotSignedByCurrentRoot {
            signers: signers.iter().map(|s| s.to_string()).collect(),
        });
    }
    let old_keys: Vec<String> = current.key_ids();
    let new_keys: Vec<String> = next.key_ids();
    let keys_changed = old_keys != new_keys;
    if keys_changed {
        let next_valid_signer = signers.iter().any(|key| {
            next.roots
                .iter()
                .any(|r| &r.key_id == key && r.is_valid_at(now_unix))
        });
        if !next_valid_signer {
            return Err(TrustRootDeny::RotationNotDualSigned {
                signers: signers.iter().map(|s| s.to_string()).collect(),
            });
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn root(key_id: &str, from: u64, until: Option<u64>) -> TrustRoot {
        TrustRoot {
            key_id: key_id.into(),
            public_key: format!("pk:{key_id}"),
            valid_from_unix: from,
            valid_until_unix: until,
        }
    }

    fn set(revision: u32, roots: Vec<TrustRoot>) -> TrustRootSet {
        TrustRootSet::new(revision, roots).expect("set")
    }

    #[test]
    fn accepts_only_currently_valid_roots() {
        let roots = set(1, vec![root("old", 0, Some(1_000)), root("new", 500, None)]);
        roots.accepts("old", 600).expect("old valid at 600");
        roots.accepts("new", 600).expect("new valid at 600");
        assert_eq!(
            roots.accepts("old", 1_000).unwrap_err(),
            TrustRootDeny::UnknownSigner {
                key_id: "old".into()
            },
            "valid_until is exclusive"
        );
        assert_eq!(
            roots.accepts("attacker", 600).unwrap_err(),
            TrustRootDeny::UnknownSigner {
                key_id: "attacker".into()
            }
        );
    }

    #[test]
    fn set_rejects_invalid_roots() {
        assert_eq!(
            TrustRootSet::new(1, vec![root("", 0, None)]).unwrap_err(),
            TrustRootDeny::EmptyKeyId
        );
        let empty_pk = TrustRoot {
            key_id: "k".into(),
            public_key: String::new(),
            valid_from_unix: 0,
            valid_until_unix: None,
        };
        assert_eq!(
            TrustRootSet::new(1, vec![empty_pk]).unwrap_err(),
            TrustRootDeny::EmptyPublicKey {
                key_id: "k".into()
            }
        );
        assert_eq!(
            TrustRootSet::new(1, vec![root("k", 100, Some(50))]).unwrap_err(),
            TrustRootDeny::InvertedValidity {
                key_id: "k".into()
            }
        );
        assert_eq!(
            TrustRootSet::new(1, vec![root("k", 0, None), root("k", 0, None)]).unwrap_err(),
            TrustRootDeny::DuplicateKeyId {
                key_id: "k".into()
            }
        );
    }

    #[test]
    fn update_must_be_signed_by_current_root_and_monotonic() {
        let current = set(1, vec![root("old", 0, Some(1_000))]);
        let next = set(2, vec![root("new", 0, None)]);
        // Same key set, signed by the current root → ok.
        validate_set_update(&current, &set(2, vec![root("old", 0, Some(1_000))]), &["old"], 500)
            .expect("same-set update ok");
        // Not signed by a currently valid root → refused.
        assert_eq!(
            validate_set_update(&current, &next, &["attacker"], 500).unwrap_err(),
            TrustRootDeny::UpdateNotSignedByCurrentRoot {
                signers: vec!["attacker".into()]
            }
        );
        // Revision must increase.
        assert_eq!(
            validate_set_update(
                &current,
                &set(1, vec![root("old", 0, Some(1_000))]),
                &["old"],
                500
            )
            .unwrap_err(),
            TrustRootDeny::RevisionNotMonotonic {
                current: 1,
                next: 1
            }
        );
    }

    #[test]
    fn rotation_requires_dual_signature() {
        let current = set(1, vec![root("old", 0, Some(1_000))]);
        let next = set(2, vec![root("new", 0, None)]);
        // Key set changed; only the old key signs → rotation refused.
        assert_eq!(
            validate_set_update(&current, &next, &["old"], 500).unwrap_err(),
            TrustRootDeny::RotationNotDualSigned {
                signers: vec!["old".into()]
            }
        );
        // Dual-signed (old + new) → rotation accepted.
        validate_set_update(&current, &next, &["old", "new"], 500)
            .expect("dual-signed rotation ok");
        // New set has a valid root at signing time.
        let next_future = set(2, vec![root("new", 1_000, None)]);
        assert_eq!(
            validate_set_update(&current, &next_future, &["old", "new"], 500).unwrap_err(),
            TrustRootDeny::RotationNotDualSigned {
                signers: vec!["old".into(), "new".into()]
            },
            "a root that is not yet valid cannot co-sign"
        );
    }
}
