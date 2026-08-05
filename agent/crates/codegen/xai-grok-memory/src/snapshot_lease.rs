//! SnapshotLeaseV1 — master plan §3.4.1.
//!
//! A running iteration holds a snapshot lease so a single new Accepted claim
//! cannot make the whole tree churn. The lease may only **advance** (swap to
//! a newer accepted snapshot) at a safe checkpoint
//! (`safe_until_checkpoint`), and only under `NormalAdvance`.
//! `SecurityRevocation` / `GrantRevocation` / `EvidenceInvalidated` invalidate
//! the lease **immediately** — no waiting for a checkpoint. Rebase always
//! produces a new snapshot reference; nothing is rewritten in place.

pub const SNAPSHOT_LEASE_SCHEMA_VERSION: u16 = 1;

/// Why a lease moved / may move.
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum InvalidationClass {
    /// Ordinary progression: only allowed at a safe checkpoint.
    NormalAdvance,
    /// Security-triggered revocation: immediate.
    SecurityRevocation,
    /// Capability-grant revocation: immediate.
    GrantRevocation,
    /// A bound evidence artifact was invalidated: immediate.
    EvidenceInvalidated,
}

impl InvalidationClass {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::NormalAdvance => "normal_advance",
            Self::SecurityRevocation => "security_revocation",
            Self::GrantRevocation => "grant_revocation",
            Self::EvidenceInvalidated => "evidence_invalidated",
        }
    }

    /// Immediate invalidation classes (everything except NormalAdvance).
    pub fn is_immediate(self) -> bool {
        !matches!(self, Self::NormalAdvance)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SnapshotLeaseState {
    Active,
    /// Superseded by a NormalAdvance at a safe checkpoint.
    Superseded,
    /// Invalidated immediately by a revocation/evidence class.
    Invalidated,
}

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct SnapshotLeaseV1 {
    pub schema_version: u16,
    pub lease_id: String,
    pub tree_id: String,
    pub snapshot_hash: String,
    pub issued_sequence: u64,
    /// The first sequence at which a NormalAdvance may swap the snapshot.
    pub safe_until_checkpoint: u64,
    pub invalidation_class: InvalidationClass,
    pub state: SnapshotLeaseState,
    pub state_reason: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SnapshotLeaseDeny {
    Invalid(String),
    EmptyField(&'static str),
    NotSha256(&'static str),
    NotActive,
    CheckpointNotReached,
    NotNormalAdvance,
    CannotInvalidateWithNormalAdvance,
}

impl SnapshotLeaseDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "snapshot_lease.invalid",
            Self::EmptyField(_) => "snapshot_lease.empty_field",
            Self::NotSha256(_) => "snapshot_lease.not_sha256",
            Self::NotActive => "snapshot_lease.not_active",
            Self::CheckpointNotReached => "snapshot_lease.checkpoint_not_reached",
            Self::NotNormalAdvance => "snapshot_lease.not_normal_advance",
            Self::CannotInvalidateWithNormalAdvance => {
                "snapshot_lease.cannot_invalidate_with_normal_advance"
            }
        }
    }
}

impl std::fmt::Display for SnapshotLeaseDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            Self::NotSha256(name) => write!(f, "{}: {name}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

fn require_non_empty(field: &'static str, value: &str) -> Result<(), SnapshotLeaseDeny> {
    if value.trim().is_empty() {
        return Err(SnapshotLeaseDeny::EmptyField(field));
    }
    Ok(())
}

fn require_sha256(field: &'static str, value: &str) -> Result<(), SnapshotLeaseDeny> {
    require_non_empty(field, value)?;
    if !value.starts_with("sha256:") || value.len() <= "sha256:".len() {
        return Err(SnapshotLeaseDeny::NotSha256(field));
    }
    Ok(())
}

impl SnapshotLeaseV1 {
    pub fn issue(
        lease_id: impl Into<String>,
        tree_id: impl Into<String>,
        snapshot_hash: impl Into<String>,
        issued_sequence: u64,
        safe_until_checkpoint: u64,
        invalidation_class: InvalidationClass,
    ) -> Result<Self, SnapshotLeaseDeny> {
        let lease_id = lease_id.into();
        let tree_id = tree_id.into();
        let snapshot_hash = snapshot_hash.into();
        require_non_empty("lease_id", &lease_id)?;
        require_non_empty("tree_id", &tree_id)?;
        require_sha256("snapshot_hash", &snapshot_hash)?;
        if safe_until_checkpoint < issued_sequence {
            return Err(SnapshotLeaseDeny::Invalid(
                "safe_until_checkpoint must not precede issued_sequence".into(),
            ));
        }
        Ok(Self {
            schema_version: SNAPSHOT_LEASE_SCHEMA_VERSION,
            lease_id,
            tree_id,
            snapshot_hash,
            issued_sequence,
            safe_until_checkpoint,
            invalidation_class,
            state: SnapshotLeaseState::Active,
            state_reason: None,
        })
    }

    pub fn validate(&self) -> Result<(), SnapshotLeaseDeny> {
        if self.schema_version != SNAPSHOT_LEASE_SCHEMA_VERSION {
            return Err(SnapshotLeaseDeny::Invalid("schema_version mismatch".into()));
        }
        require_non_empty("lease_id", &self.lease_id)?;
        require_non_empty("tree_id", &self.tree_id)?;
        require_sha256("snapshot_hash", &self.snapshot_hash)?;
        if self.safe_until_checkpoint < self.issued_sequence {
            return Err(SnapshotLeaseDeny::Invalid(
                "safe_until_checkpoint must not precede issued_sequence".into(),
            ));
        }
        if self.state == SnapshotLeaseState::Active
            && self.invalidation_class.is_immediate()
            && self.invalidation_class != InvalidationClass::NormalAdvance
        {
            // An immediate class issued as the *current* class is legal only
            // when it has already invalidated the lease; an Active lease must
            // carry NormalAdvance.
            return Err(SnapshotLeaseDeny::Invalid(
                "active lease must carry normal_advance".into(),
            ));
        }
        Ok(())
    }

    /// NormalAdvance: swap the snapshot at a safe checkpoint. Anything else
    /// (wrong class, checkpoint not reached, lease not active) denies.
    pub fn advance(
        &mut self,
        at_sequence: u64,
        new_snapshot_hash: impl Into<String>,
        new_safe_until_checkpoint: u64,
    ) -> Result<(), SnapshotLeaseDeny> {
        if self.state != SnapshotLeaseState::Active {
            return Err(SnapshotLeaseDeny::NotActive);
        }
        if self.invalidation_class != InvalidationClass::NormalAdvance {
            return Err(SnapshotLeaseDeny::NotNormalAdvance);
        }
        if at_sequence < self.safe_until_checkpoint {
            return Err(SnapshotLeaseDeny::CheckpointNotReached);
        }
        let new_snapshot_hash = new_snapshot_hash.into();
        require_sha256("new_snapshot_hash", &new_snapshot_hash)?;
        if new_safe_until_checkpoint < at_sequence {
            return Err(SnapshotLeaseDeny::Invalid(
                "new safe_until_checkpoint must not precede the advance sequence".into(),
            ));
        }
        self.snapshot_hash = new_snapshot_hash;
        self.issued_sequence = at_sequence;
        self.safe_until_checkpoint = new_safe_until_checkpoint;
        Ok(())
    }

    /// Immediate invalidation under Security/Grant/Evidence classes. A
    /// NormalAdvance class can never invalidate.
    pub fn invalidate(
        &mut self,
        class: InvalidationClass,
        reason: impl Into<String>,
    ) -> Result<(), SnapshotLeaseDeny> {
        if !class.is_immediate() {
            return Err(SnapshotLeaseDeny::CannotInvalidateWithNormalAdvance);
        }
        if self.state != SnapshotLeaseState::Active {
            return Err(SnapshotLeaseDeny::NotActive);
        }
        self.invalidation_class = class;
        self.state = SnapshotLeaseState::Invalidated;
        self.state_reason = Some(reason.into());
        Ok(())
    }

    pub fn is_usable(&self) -> bool {
        self.state == SnapshotLeaseState::Active
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_lease() -> SnapshotLeaseV1 {
        SnapshotLeaseV1::issue(
            "lease-1",
            "tree-1",
            "sha256:snap-a",
            10,
            20,
            InvalidationClass::NormalAdvance,
        )
        .expect("lease")
    }

    #[test]
    fn issue_and_validate_positive() {
        let lease = sample_lease();
        lease.validate().expect("valid");
        assert!(lease.is_usable());
        assert_eq!(lease.snapshot_hash, "sha256:snap-a");
    }

    #[test]
    fn issue_rejects_missing_fields_and_bad_ordering() {
        let err = SnapshotLeaseV1::issue("", "tree-1", "sha256:s", 1, 2, InvalidationClass::NormalAdvance)
            .unwrap_err();
        assert_eq!(err, SnapshotLeaseDeny::EmptyField("lease_id"));
        let err = SnapshotLeaseV1::issue("l", "tree-1", "plain", 1, 2, InvalidationClass::NormalAdvance)
            .unwrap_err();
        assert_eq!(err, SnapshotLeaseDeny::NotSha256("snapshot_hash"));
        let err = SnapshotLeaseV1::issue("l", "tree-1", "sha256:s", 5, 4, InvalidationClass::NormalAdvance)
            .unwrap_err();
        assert_eq!(err.code(), "snapshot_lease.invalid");
    }

    #[test]
    fn advance_only_at_safe_checkpoint() {
        let mut lease = sample_lease();
        assert_eq!(
            lease.advance(15, "sha256:snap-b", 30).unwrap_err(),
            SnapshotLeaseDeny::CheckpointNotReached,
            "advance before safe_until_checkpoint must deny"
        );
        lease
            .advance(20, "sha256:snap-b", 30)
            .expect("advance at the safe checkpoint");
        assert_eq!(lease.snapshot_hash, "sha256:snap-b");
        assert_eq!(lease.issued_sequence, 20);
        assert_eq!(lease.safe_until_checkpoint, 30);
        assert!(lease.is_usable());
    }

    #[test]
    fn advance_denied_after_invalidation() {
        let mut lease = sample_lease();
        lease
            .invalidate(InvalidationClass::EvidenceInvalidated, "artifact revoked")
            .expect("invalidate");
        assert!(!lease.is_usable());
        assert_eq!(lease.state, SnapshotLeaseState::Invalidated);
        assert_eq!(
            lease.advance(20, "sha256:snap-b", 30).unwrap_err(),
            SnapshotLeaseDeny::NotActive
        );
    }

    #[test]
    fn immediate_classes_invalidate_and_are_not_advanceable() {
        for class in [
            InvalidationClass::SecurityRevocation,
            InvalidationClass::GrantRevocation,
            InvalidationClass::EvidenceInvalidated,
        ] {
            let mut lease = sample_lease();
            lease.invalidate(class, "immediate").expect("invalidate");
            assert!(!lease.is_usable());
            assert_eq!(lease.invalidation_class, class);
            assert_eq!(
                lease.advance(20, "sha256:snap-b", 30).unwrap_err(),
                SnapshotLeaseDeny::NotActive,
                "immediate invalidation must block all advances"
            );
        }
    }

    #[test]
    fn normal_advance_class_cannot_invalidate() {
        let mut lease = sample_lease();
        assert_eq!(
            lease
                .invalidate(InvalidationClass::NormalAdvance, "nope")
                .unwrap_err(),
            SnapshotLeaseDeny::CannotInvalidateWithNormalAdvance
        );
    }

    #[test]
    fn revoke_issued_immediate_class_is_invalid_when_active() {
        let lease = SnapshotLeaseV1::issue(
            "l",
            "tree-1",
            "sha256:s",
            1,
            2,
            InvalidationClass::SecurityRevocation,
        )
        .expect("issue");
        assert_eq!(
            lease.validate().unwrap_err().code(),
            "snapshot_lease.invalid",
            "an Active lease must carry normal_advance"
        );
    }
}
