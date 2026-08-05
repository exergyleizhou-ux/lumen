//! EnvironmentFingerprintV1 — master plan §3.1.3.
//!
//! `HostVerified` evidence binds the environment that produced it:
//! toolchain, lockfile hash, target, executable hash, allowlisted env hash
//! and input artifact hashes. Reproduction strength is graded:
//!
//! - `RecomputedSameEnv` — recomputed inside the same task environment;
//! - `RecomputedCanonicalEnv` — required at least for cross-task promotion;
//! - `ThirdPartyReproducible` — an independent verifier recomputed the
//!   evidence bundle (required for LongTermMemory / release provenance).

pub const ENVIRONMENT_FINGERPRINT_SCHEMA_VERSION: u16 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ReproLevel {
    RecomputedSameEnv = 0,
    RecomputedCanonicalEnv = 1,
    ThirdPartyReproducible = 2,
}

impl ReproLevel {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::RecomputedSameEnv => "recomputed_same_env",
            Self::RecomputedCanonicalEnv => "recomputed_canonical_env",
            Self::ThirdPartyReproducible => "third_party_reproducible",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct EnvironmentFingerprintV1 {
    pub schema_version: u16,
    pub toolchain: String,
    pub lockfile_hash: String,
    pub target: String,
    pub executable_hash: String,
    /// Hash of the allowlisted environment variables that affect the run.
    pub allowlisted_env_hash: String,
    /// Sorted unique input artifact hashes the evidence consumed.
    pub input_artifact_hashes: Vec<String>,
    pub repro_level: ReproLevel,
    /// Canonical hash over all fields above.
    pub fingerprint_hash: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum FingerprintDeny {
    Invalid(String),
    EmptyField(&'static str),
    NotSha256(&'static str),
    HashMismatch,
    InsufficientReproLevel,
    SchemaMismatch,
}

impl FingerprintDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "env_fingerprint.invalid",
            Self::EmptyField(_) => "env_fingerprint.empty_field",
            Self::NotSha256(_) => "env_fingerprint.not_sha256",
            Self::HashMismatch => "env_fingerprint.hash_mismatch",
            Self::InsufficientReproLevel => "env_fingerprint.insufficient_repro_level",
            Self::SchemaMismatch => "env_fingerprint.schema_mismatch",
        }
    }
}

impl std::fmt::Display for FingerprintDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            Self::NotSha256(name) => write!(f, "{}: {name}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

fn require_non_empty(field: &'static str, value: &str) -> Result<(), FingerprintDeny> {
    if value.trim().is_empty() {
        return Err(FingerprintDeny::EmptyField(field));
    }
    Ok(())
}

fn require_sha256(field: &'static str, value: &str) -> Result<(), FingerprintDeny> {
    require_non_empty(field, value)?;
    if !value.starts_with("sha256:") || value.len() <= "sha256:".len() {
        return Err(FingerprintDeny::NotSha256(field));
    }
    Ok(())
}

fn fingerprint_preimage(
    fingerprint: &EnvironmentFingerprintV1,
) -> Result<Vec<u8>, crate::canonical::CanonicalError> {
    use crate::canonical::{CanonicalRecord, CanonicalValue};
    // Sort the string hashes first — CanonicalValue is not Ord.
    let mut sorted_hashes = fingerprint.input_artifact_hashes.clone();
    sorted_hashes.sort();
    let artifacts: Vec<CanonicalValue> = sorted_hashes
        .iter()
        .map(CanonicalValue::str)
        .collect();
    CanonicalRecord::new("environment-fingerprint")
        .field("schema_version", CanonicalValue::U64(fingerprint.schema_version as u64))
        .field("toolchain", CanonicalValue::str(&fingerprint.toolchain))
        .field("lockfile_hash", CanonicalValue::str(&fingerprint.lockfile_hash))
        .field("target", CanonicalValue::str(&fingerprint.target))
        .field("executable_hash", CanonicalValue::str(&fingerprint.executable_hash))
        .field(
            "allowlisted_env_hash",
            CanonicalValue::str(&fingerprint.allowlisted_env_hash),
        )
        .field("input_artifact_hashes", CanonicalValue::Seq(artifacts))
        .field("repro_level", CanonicalValue::str(fingerprint.repro_level.as_str()))
        .canonical_bytes()
}

impl EnvironmentFingerprintV1 {
    pub fn build(
        toolchain: impl Into<String>,
        lockfile_hash: impl Into<String>,
        target: impl Into<String>,
        executable_hash: impl Into<String>,
        allowlisted_env_hash: impl Into<String>,
        input_artifact_hashes: Vec<String>,
        repro_level: ReproLevel,
    ) -> Result<Self, FingerprintDeny> {
        let mut fingerprint = Self {
            schema_version: ENVIRONMENT_FINGERPRINT_SCHEMA_VERSION,
            toolchain: toolchain.into(),
            lockfile_hash: lockfile_hash.into(),
            target: target.into(),
            executable_hash: executable_hash.into(),
            allowlisted_env_hash: allowlisted_env_hash.into(),
            input_artifact_hashes,
            repro_level,
            fingerprint_hash: String::new(),
        };
        let hash = fingerprint_preimage(&fingerprint)
            .map_err(|e| FingerprintDeny::Invalid(e.to_string()))?;
        fingerprint.fingerprint_hash = format!("sha256:{}", blake3::hash(&hash).to_hex());
        fingerprint.validate()?;
        Ok(fingerprint)
    }

    pub fn validate(&self) -> Result<(), FingerprintDeny> {
        if self.schema_version != ENVIRONMENT_FINGERPRINT_SCHEMA_VERSION {
            return Err(FingerprintDeny::SchemaMismatch);
        }
        require_non_empty("toolchain", &self.toolchain)?;
        require_non_empty("target", &self.target)?;
        require_sha256("lockfile_hash", &self.lockfile_hash)?;
        require_sha256("executable_hash", &self.executable_hash)?;
        require_sha256("allowlisted_env_hash", &self.allowlisted_env_hash)?;
        for artifact in &self.input_artifact_hashes {
            require_sha256("input_artifact_hash", artifact)?;
        }
        let recomputed = fingerprint_preimage(self)
            .map_err(|e| FingerprintDeny::Invalid(e.to_string()))?;
        if format!("sha256:{}", blake3::hash(&recomputed).to_hex()) != self.fingerprint_hash {
            return Err(FingerprintDeny::HashMismatch);
        }
        Ok(())
    }

    /// Cross-task promotion requires at least `RecomputedCanonicalEnv`.
    pub fn authorize_promotion(&self) -> Result<(), FingerprintDeny> {
        self.validate()?;
        if self.repro_level < ReproLevel::RecomputedCanonicalEnv {
            return Err(FingerprintDeny::InsufficientReproLevel);
        }
        Ok(())
    }

    /// LongTermMemory / release provenance requires independent
    /// third-party reproduction.
    pub fn authorize_long_term_memory(&self) -> Result<(), FingerprintDeny> {
        self.validate()?;
        if self.repro_level < ReproLevel::ThirdPartyReproducible {
            return Err(FingerprintDeny::InsufficientReproLevel);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample(level: ReproLevel) -> EnvironmentFingerprintV1 {
        EnvironmentFingerprintV1::build(
            "rust-1.85",
            "sha256:lockfile",
            "x86_64-apple-darwin",
            "sha256:executable",
            "sha256:env",
            vec!["sha256:input-a".into(), "sha256:input-b".into()],
            level,
        )
        .expect("fingerprint")
    }

    #[test]
    fn build_validate_roundtrip() {
        let fingerprint = sample(ReproLevel::RecomputedSameEnv);
        fingerprint.validate().expect("valid");
        let same = sample(ReproLevel::RecomputedSameEnv);
        assert_eq!(fingerprint.fingerprint_hash, same.fingerprint_hash);
    }

    #[test]
    fn build_rejects_missing_fields_and_bad_hashes() {
        let err = EnvironmentFingerprintV1::build(
            "",
            "sha256:lock",
            "target",
            "sha256:exe",
            "sha256:env",
            vec![],
            ReproLevel::RecomputedSameEnv,
        )
        .unwrap_err();
        assert_eq!(err, FingerprintDeny::EmptyField("toolchain"));
        let err = EnvironmentFingerprintV1::build(
            "rust",
            "plain",
            "target",
            "sha256:exe",
            "sha256:env",
            vec![],
            ReproLevel::RecomputedSameEnv,
        )
        .unwrap_err();
        assert_eq!(err, FingerprintDeny::NotSha256("lockfile_hash"));
        let err = EnvironmentFingerprintV1::build(
            "rust",
            "sha256:lock",
            "target",
            "sha256:exe",
            "sha256:env",
            vec!["nope".into()],
            ReproLevel::RecomputedSameEnv,
        )
        .unwrap_err();
        assert_eq!(err, FingerprintDeny::NotSha256("input_artifact_hash"));
    }

    #[test]
    fn tamper_detected() {
        let mut fingerprint = sample(ReproLevel::RecomputedCanonicalEnv);
        fingerprint.toolchain = "other".into();
        assert_eq!(fingerprint.validate().unwrap_err(), FingerprintDeny::HashMismatch);
    }

    #[test]
    fn repro_level_gates_promotion_and_long_term_memory() {
        // SameEnv: cannot promote across tasks.
        let same_env = sample(ReproLevel::RecomputedSameEnv);
        assert_eq!(
            same_env.authorize_promotion().unwrap_err(),
            FingerprintDeny::InsufficientReproLevel
        );
        // CanonicalEnv: promotion ok, LongTermMemory still gated.
        let canonical = sample(ReproLevel::RecomputedCanonicalEnv);
        canonical.authorize_promotion().expect("promotion ok");
        assert_eq!(
            canonical.authorize_long_term_memory().unwrap_err(),
            FingerprintDeny::InsufficientReproLevel
        );
        // ThirdParty: both gates pass.
        let third_party = sample(ReproLevel::ThirdPartyReproducible);
        third_party.authorize_promotion().expect("promotion ok");
        third_party.authorize_long_term_memory().expect("memory ok");
    }
}
