//! NG-10A `ReleaseSourceTupleV1` — release provenance contract (master plan
//! NG-10B~E / contract S13).
//!
//! A release tuple freezes the A/B tuple: clean **source commit A**, the
//! **evidence commit B** that carries lock/SBOM/readiness as an evidence-only
//! suffix, the **release tag** (which must peel to A — a tag pointing at B is
//! invalid), the **binary sha256** of the locked build and the **source-lock
//! sha256**. The write path refuses missing fields and a tag that does not
//! target the source commit; the read path re-validates, so a hand-edited or
//! torn provenance file fails closed.

use std::path::Path;

pub const RELEASE_SOURCE_TUPLE_SCHEMA_VERSION: u16 = 1;

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct ReleaseSourceTupleV1 {
    pub schema_version: u16,
    /// Product version, e.g. `2.0.0`.
    pub version: String,
    /// Full source commit A (40 hex) — the tag must peel to this.
    pub source_commit: String,
    /// Full evidence commit B (40 hex) — an evidence-only suffix of A.
    pub evidence_commit: String,
    /// Release tag ref, e.g. `v2.0.0`.
    pub tag_ref: String,
    /// Commit the tag peels to (must equal `source_commit`).
    pub tag_commit: String,
    /// `sha256:...` of the locked release binary.
    pub binary_sha256: String,
    /// `sha256:...` of SOURCE_LOCK.json at release time.
    pub source_lock_sha256: String,
    /// ISO-8601 UTC generation time.
    pub generated_at: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReleaseTupleDeny {
    Invalid(String),
    EmptyField(&'static str),
    TagNotAtSource,
    SameSourceAndEvidence,
    NotSha256(&'static str),
    SchemaMismatch,
    NotReadable(String),
}

impl ReleaseTupleDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "release_tuple.invalid",
            Self::EmptyField(_) => "release_tuple.empty_field",
            Self::TagNotAtSource => "release_tuple.tag_not_at_source",
            Self::SameSourceAndEvidence => "release_tuple.same_source_and_evidence",
            Self::NotSha256(_) => "release_tuple.not_sha256",
            Self::SchemaMismatch => "release_tuple.schema_mismatch",
            Self::NotReadable(_) => "release_tuple.not_readable",
        }
    }
}

impl std::fmt::Display for ReleaseTupleDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            Self::NotSha256(name) => write!(f, "{}: {name}", self.code()),
            Self::NotReadable(msg) => write!(f, "{}: {msg}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

fn require_non_empty(field: &'static str, value: &str) -> Result<(), ReleaseTupleDeny> {
    if value.trim().is_empty() {
        return Err(ReleaseTupleDeny::EmptyField(field));
    }
    Ok(())
}

fn require_full_commit(field: &'static str, value: &str) -> Result<(), ReleaseTupleDeny> {
    require_non_empty(field, value)?;
    if value.len() != 40 || !value.chars().all(|c| c.is_ascii_hexdigit()) {
        return Err(ReleaseTupleDeny::Invalid(format!(
            "{field} must be a full 40-char SHA-1"
        )));
    }
    Ok(())
}

fn require_sha256(field: &'static str, value: &str) -> Result<(), ReleaseTupleDeny> {
    require_non_empty(field, value)?;
    if !value.starts_with("sha256:") || value.len() <= "sha256:".len() {
        return Err(ReleaseTupleDeny::NotSha256(field));
    }
    Ok(())
}

impl ReleaseSourceTupleV1 {
    /// Validate every invariant:
    /// - all fields mandatory;
    /// - source A and evidence B are distinct full commits;
    /// - the tag peels to source A (a tag pointing at B is invalid);
    /// - binary and source-lock hashes are `sha256:` references.
    pub fn validate(&self) -> Result<(), ReleaseTupleDeny> {
        if self.schema_version != RELEASE_SOURCE_TUPLE_SCHEMA_VERSION {
            return Err(ReleaseTupleDeny::SchemaMismatch);
        }
        require_non_empty("version", &self.version)?;
        require_non_empty("tag_ref", &self.tag_ref)?;
        require_non_empty("generated_at", &self.generated_at)?;
        require_full_commit("source_commit", &self.source_commit)?;
        require_full_commit("evidence_commit", &self.evidence_commit)?;
        require_full_commit("tag_commit", &self.tag_commit)?;
        require_sha256("binary_sha256", &self.binary_sha256)?;
        require_sha256("source_lock_sha256", &self.source_lock_sha256)?;
        if self.source_commit == self.evidence_commit {
            return Err(ReleaseTupleDeny::SameSourceAndEvidence);
        }
        if self.tag_commit != self.source_commit {
            return Err(ReleaseTupleDeny::TagNotAtSource);
        }
        Ok(())
    }

    /// Write the tuple atomically (tempfile + rename). Refuses to write an
    /// invalid tuple — a hand-built provenance file can never land.
    pub fn write_to(&self, path: &Path) -> Result<(), ReleaseTupleDeny> {
        self.validate()?;
        let parent = path
            .parent()
            .ok_or_else(|| ReleaseTupleDeny::NotReadable("no parent dir".into()))?;
        std::fs::create_dir_all(parent)
            .map_err(|e| ReleaseTupleDeny::NotReadable(e.to_string()))?;
        let json = serde_json::to_string_pretty(self)
            .map_err(|e| ReleaseTupleDeny::Invalid(e.to_string()))?;
        let tmp = parent.join(format!(".release-tuple.tmp-{}", std::process::id()));
        std::fs::write(&tmp, json + "\n")
            .map_err(|e| ReleaseTupleDeny::NotReadable(e.to_string()))?;
        std::fs::rename(&tmp, path).map_err(|e| ReleaseTupleDeny::NotReadable(e.to_string()))
    }

    /// Read and validate an existing tuple. A torn, hand-edited or
    /// invalid tuple fails closed.
    pub fn read_from(path: &Path) -> Result<Self, ReleaseTupleDeny> {
        let raw = std::fs::read_to_string(path)
            .map_err(|e| ReleaseTupleDeny::NotReadable(e.to_string()))?;
        let tuple: ReleaseSourceTupleV1 = serde_json::from_str(&raw)
            .map_err(|e| ReleaseTupleDeny::NotReadable(e.to_string()))?;
        tuple.validate()?;
        Ok(tuple)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_tuple() -> ReleaseSourceTupleV1 {
        ReleaseSourceTupleV1 {
            schema_version: RELEASE_SOURCE_TUPLE_SCHEMA_VERSION,
            version: "2.0.0".into(),
            source_commit: "f51fb902a4c97ab26e4cff5f52c52c1b72b8708d".into(),
            evidence_commit: "d74db8e0a415911ad3c6eb859c8424888edf3499".into(),
            tag_ref: "v2.0.0".into(),
            tag_commit: "f51fb902a4c97ab26e4cff5f52c52c1b72b8708d".into(),
            binary_sha256: "sha256:c929e50f8ef7ddacb552e2ea14261b80a4ae8b36485c4713ab55fd2b6dd62c4d"
                .into(),
            source_lock_sha256: "sha256:2b7c5e8faabc241880da70b230bf7d5afe3a249616ffdad74d4b53514ebe69ba"
                .into(),
            generated_at: "2026-08-04T12:00:00Z".into(),
        }
    }

    #[test]
    fn tuple_roundtrip_write_read_validate() {
        let dir = tempfile::tempdir().expect("tmp");
        let path = dir.path().join("release-source-tuple.json");
        let tuple = sample_tuple();
        tuple.write_to(&path).expect("write");
        let back = ReleaseSourceTupleV1::read_from(&path).expect("read");
        assert_eq!(back, tuple);
    }

    #[test]
    fn tuple_rejects_tag_not_at_source() {
        let mut tuple = sample_tuple();
        tuple.tag_commit = tuple.evidence_commit.clone();
        assert_eq!(
            tuple.validate().unwrap_err(),
            ReleaseTupleDeny::TagNotAtSource
        );
        tuple.write_to(&tempfile::tempdir().unwrap().path().join("t.json"))
            .expect_err("tag at evidence must refuse to write");
    }

    #[test]
    fn tuple_rejects_same_source_and_evidence() {
        let mut tuple = sample_tuple();
        tuple.evidence_commit = tuple.source_commit.clone();
        assert_eq!(
            tuple.validate().unwrap_err(),
            ReleaseTupleDeny::SameSourceAndEvidence
        );
    }

    #[test]
    fn tuple_rejects_missing_fields_and_bad_hashes() {
        let mut tuple = sample_tuple();
        tuple.binary_sha256 = "".into();
        assert_eq!(
            tuple.validate().unwrap_err(),
            ReleaseTupleDeny::EmptyField("binary_sha256")
        );
        let mut tuple = sample_tuple();
        tuple.binary_sha256 = "plain".into();
        assert_eq!(
            tuple.validate().unwrap_err(),
            ReleaseTupleDeny::NotSha256("binary_sha256")
        );
        let mut tuple = sample_tuple();
        tuple.source_commit = "short".into();
        assert_eq!(tuple.validate().unwrap_err().code(), "release_tuple.invalid");
        let mut tuple = sample_tuple();
        tuple.version = "".into();
        assert_eq!(
            tuple.validate().unwrap_err(),
            ReleaseTupleDeny::EmptyField("version")
        );
    }
}
