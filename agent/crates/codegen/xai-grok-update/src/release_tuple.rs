//! NG-10A: `ReleaseSourceTupleV1` — the auditable source/evidence binding a
//! Lumen release install/upgrade must carry and validate.
//!
//! A formal Lumen release is a two-phase transaction, not a single commit:
//!
//! - `source_commit` (**A**): the clean product source the binaries, tag,
//!   and release source bind to.
//! - `evidence_commit` (**B**): A's allowlisted lock/SBOM/readiness
//!   successor. B must never carry source, version, Cargo.lock, or runtime
//!   changes — every intervening path must be evidence-only.
//! - `release_tag` names **A**, never B.
//! - `source_lock_sha256` is the SHA-256 digest of `SOURCE_LOCK.json` as
//!   recorded at B; it must match the lock that names A, or the tuple is
//!   refused.
//! - `release_contract_revision` pins the execution contract this tuple
//!   was produced under.
//!
//! Validation is pure and fail-closed: any missing/unknown schema, malformed
//! commit/tag/version, a tuple that conflates A with B, a lock digest
//! mismatch, or a revision that is not the pinned contract revision is
//! rejected. The git-backed [`ReleaseSourceTupleV1::verify_repo_relation`]
//! check additionally proves A → B ancestry, the evidence-only suffix, and
//! tag → A peeling inside a concrete repository.

use std::fmt::Write as _;
use std::path::Path;
use std::process::Command;

use semver::Version;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use thiserror::Error;

pub const RELEASE_TUPLE_SCHEMA_V1: &str = "lumen.release.source_tuple.v1";
pub const RELEASE_CONTRACT_REVISION: &str = "LUMEN-NEXTGEN-EXECUTION-CONTRACT-2026-08-03";

/// Hex SHA-256 digest (64 lowercase hex chars).
pub type Sha256Hex = String;

#[derive(Debug, Error, Clone, PartialEq, Eq)]
pub enum ReleaseTupleError {
    #[error("unsupported release tuple schema: {actual:?}")]
    InvalidSchema { actual: String },
    #[error("source_commit must be a 40/64-hex git sha, got {value:?}")]
    InvalidSourceCommit { value: String },
    #[error("evidence_commit must be a 40/64-hex git sha, got {value:?}")]
    InvalidEvidenceCommit { value: String },
    #[error("source_commit and evidence_commit must differ: A is the build source, B is its evidence successor")]
    SourceEqualsEvidence,
    #[error("lumen_version is not valid SemVer: {value:?}")]
    InvalidLumenVersion { value: String },
    #[error("release_tag must be v<lumen_version>; got {tag:?}, expected {expected:?}")]
    InvalidReleaseTag { tag: String, expected: String },
    #[error("source_lock_sha256 must be 64 lowercase hex chars, got {value:?}")]
    InvalidSourceLockDigest { value: String },
    #[error(
        "release_contract_revision must be {RELEASE_CONTRACT_REVISION}; got {actual:?}"
    )]
    InvalidContractRevision { actual: String },
    #[error("cannot run git in {repo}: {detail}")]
    GitInvocation { repo: String, detail: String },
    #[error("git {args} failed in {repo}: {stderr}")]
    GitCommand {
        repo: String,
        args: String,
        stderr: String,
    },
    #[error("commit {sha} (as {role}) does not exist in repository {repo}")]
    MissingCommit {
        role: &'static str,
        sha: String,
        repo: String,
    },
    #[error("evidence commit B is not a descendant of source commit A")]
    EvidenceNotDescendant,
    #[error("evidence suffix carries a non-evidence path: {path}")]
    NonEvidenceSuffix { path: String },
    #[error("release tag {tag} peels to {peeled}, not source commit {expected}")]
    TagDoesNotPeelToSource {
        tag: String,
        peeled: String,
        expected: String,
    },
    #[error("SOURCE_LOCK.json digest mismatch: tuple expected {expected}, lock is {actual}")]
    LockDigestMismatch { expected: String, actual: String },
}

/// The evidence-only suffix set a `B` commit may carry on top of `A`.
///
/// Mirrors `scripts/install-local.sh`'s accepted suffix set and
/// `scripts/source-lock.sh`'s ordering comment: lock, SBOM, and readiness
/// evidence — nothing else.
pub fn is_evidence_only_path(path: &str) -> bool {
    path == "SOURCE_LOCK.json"
        || path == "SBOM.spdx.json"
        || path.starts_with("artifacts/readiness/")
}

fn is_commit_sha(value: &str) -> bool {
    matches!(value.len(), 40 | 64) && value.chars().all(|c| c.is_ascii_hexdigit())
}

fn is_sha256_hex(value: &str) -> bool {
    value.len() == 64 && value.chars().all(|c| c.is_ascii_hexdigit())
}

fn sha256_hex(bytes: &[u8]) -> Sha256Hex {
    let digest = Sha256::digest(bytes);
    let mut out = String::with_capacity(64);
    for byte in digest {
        let _ = write!(out, "{byte:02x}");
    }
    out
}

/// NG-10A: the auditable `(source A, evidence B, tag → A)` release binding.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct ReleaseSourceTupleV1 {
    pub schema: String,
    /// Clean product source commit the binaries and tag bind to (A).
    pub source_commit: String,
    /// A's allowlisted lock/SBOM/readiness successor (B).
    pub evidence_commit: String,
    /// Release tag; must name A, never B.
    pub release_tag: String,
    /// Lumen product version (must match `VERSION` / `lumen --version`).
    pub lumen_version: String,
    /// SHA-256 of `SOURCE_LOCK.json` recorded at B.
    pub source_lock_sha256: Sha256Hex,
    /// Pinned execution contract revision.
    pub release_contract_revision: String,
}

impl ReleaseSourceTupleV1 {
    /// Structural validation. Fail-closed: anything malformed or internally
    /// inconsistent is rejected.
    pub fn validate(&self) -> Result<(), ReleaseTupleError> {
        if self.schema != RELEASE_TUPLE_SCHEMA_V1 {
            return Err(ReleaseTupleError::InvalidSchema {
                actual: self.schema.clone(),
            });
        }
        if !is_commit_sha(&self.source_commit) {
            return Err(ReleaseTupleError::InvalidSourceCommit {
                value: self.source_commit.clone(),
            });
        }
        if !is_commit_sha(&self.evidence_commit) {
            return Err(ReleaseTupleError::InvalidEvidenceCommit {
                value: self.evidence_commit.clone(),
            });
        }
        if self.source_commit == self.evidence_commit {
            return Err(ReleaseTupleError::SourceEqualsEvidence);
        }
        let version = Version::parse(&self.lumen_version).map_err(|_| {
            ReleaseTupleError::InvalidLumenVersion {
                value: self.lumen_version.clone(),
            }
        })?;
        let expected_tag = format!("v{version}");
        if self.release_tag != expected_tag {
            return Err(ReleaseTupleError::InvalidReleaseTag {
                tag: self.release_tag.clone(),
                expected: expected_tag,
            });
        }
        if !is_sha256_hex(&self.source_lock_sha256) {
            return Err(ReleaseTupleError::InvalidSourceLockDigest {
                value: self.source_lock_sha256.clone(),
            });
        }
        if self.release_contract_revision != RELEASE_CONTRACT_REVISION {
            return Err(ReleaseTupleError::InvalidContractRevision {
                actual: self.release_contract_revision.clone(),
            });
        }
        Ok(())
    }

    /// Verify the tuple against the on-disk `SOURCE_LOCK.json` bytes: the
    /// lock's SHA-256 must equal `source_lock_sha256`.
    pub fn verify_lock_digest(&self, source_lock_json: &[u8]) -> Result<(), ReleaseTupleError> {
        let actual = sha256_hex(source_lock_json);
        if actual != self.source_lock_sha256 {
            return Err(ReleaseTupleError::LockDigestMismatch {
                expected: self.source_lock_sha256.clone(),
                actual,
            });
        }
        Ok(())
    }

    /// Structural validation plus lock-digest verification.
    pub fn verify(&self, source_lock_json: &[u8]) -> Result<(), ReleaseTupleError> {
        self.validate()?;
        self.verify_lock_digest(source_lock_json)
    }

    /// Git-backed relation check inside a concrete repository:
    ///
    /// 1. A and B both resolve to commits.
    /// 2. B is a strict descendant of A (`merge-base --is-ancestor`).
    /// 3. Every path in `A..B` is evidence-only.
    /// 4. `release_tag` peels to A.
    ///
    /// This is the exact shape `scripts/install-local.sh` and
    /// `scripts/release.sh` enforce at the shell level; the tuple carries the
    /// same contract so installers/verifiers can assert it programmatically.
    pub fn verify_repo_relation(&self, repo: &Path) -> Result<(), ReleaseTupleError> {
        // 1. A and B both resolve to commits in this repository.
        resolve_commit(repo, &self.source_commit, "source_commit A")?;
        resolve_commit(repo, &self.evidence_commit, "evidence_commit B")?;
        // 2. B is a strict descendant of A.
        git(
            repo,
            &[
                "merge-base",
                "--is-ancestor",
                &self.source_commit,
                &self.evidence_commit,
            ],
        )
        .map_err(|_| ReleaseTupleError::EvidenceNotDescendant)?;
        // 3. Every path in A..B is evidence-only (no source/version/Cargo/runtime).
        let suffix =
            git(repo, &["diff", "--name-only", &self.source_commit, &self.evidence_commit])?;
        for path in suffix.lines() {
            if !is_evidence_only_path(path) {
                return Err(ReleaseTupleError::NonEvidenceSuffix {
                    path: path.to_string(),
                });
            }
        }
        // 4. The release tag peels to A, never B.
        let peeled = git(repo, &["rev-parse", &format!("{}^{{commit}}", self.release_tag)])?;
        if peeled != self.source_commit {
            return Err(ReleaseTupleError::TagDoesNotPeelToSource {
                tag: self.release_tag.clone(),
                peeled,
                expected: self.source_commit.clone(),
            });
        }
        Ok(())
    }
}

fn resolve_commit(
    repo: &Path,
    sha: &str,
    role: &'static str,
) -> Result<(), ReleaseTupleError> {
    git(repo, &["rev-parse", "--verify", &format!("{sha}^{{commit}}")]).map_err(|err| match err {
        ReleaseTupleError::GitCommand { repo, .. } => ReleaseTupleError::MissingCommit {
            role,
            sha: sha.to_string(),
            repo,
        },
        other => other,
    })?;
    Ok(())
}

fn git(repo: &Path, args: &[&str]) -> Result<String, ReleaseTupleError> {
    let output = Command::new("git")
        .arg("-C")
        .arg(repo)
        .args(args)
        .output()
        .map_err(|e| ReleaseTupleError::GitInvocation {
            repo: repo.display().to_string(),
            detail: e.to_string(),
        })?;
    if !output.status.success() {
        return Err(ReleaseTupleError::GitCommand {
            repo: repo.display().to_string(),
            args: args.join(" "),
            stderr: String::from_utf8_lossy(&output.stderr).trim().to_string(),
        });
    }
    Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;
    use std::process::Command;

    fn valid_tuple() -> ReleaseSourceTupleV1 {
        ReleaseSourceTupleV1 {
            schema: RELEASE_TUPLE_SCHEMA_V1.to_string(),
            source_commit: "a".repeat(40),
            evidence_commit: "b".repeat(40),
            release_tag: "v2.0.0-rc.1".to_string(),
            lumen_version: "2.0.0-rc.1".to_string(),
            source_lock_sha256: "c".repeat(64),
            release_contract_revision: RELEASE_CONTRACT_REVISION.to_string(),
        }
    }

    #[test]
    fn valid_tuple_passes_structural_validation() {
        assert_eq!(valid_tuple().validate(), Ok(()));
    }

    #[test]
    fn mismatch_tag_is_rejected() {
        let mut tuple = valid_tuple();
        tuple.release_tag = "v2.0.0".to_string();
        assert!(matches!(
            tuple.validate(),
            Err(ReleaseTupleError::InvalidReleaseTag { .. })
        ));
    }

    #[test]
    fn source_equals_evidence_is_rejected() {
        let mut tuple = valid_tuple();
        tuple.evidence_commit = tuple.source_commit.clone();
        assert_eq!(tuple.validate(), Err(ReleaseTupleError::SourceEqualsEvidence));
    }

    #[test]
    fn unknown_schema_is_rejected() {
        let mut tuple = valid_tuple();
        tuple.schema = "lumen.release.source_tuple.v0".to_string();
        assert!(matches!(
            tuple.validate(),
            Err(ReleaseTupleError::InvalidSchema { .. })
        ));
    }

    #[test]
    fn malformed_commit_shas_are_rejected() {
        let mut tuple = valid_tuple();
        tuple.source_commit = "not-a-sha".to_string();
        assert!(matches!(
            tuple.validate(),
            Err(ReleaseTupleError::InvalidSourceCommit { .. })
        ));
        tuple = valid_tuple();
        tuple.evidence_commit = "zz".repeat(20);
        assert!(matches!(
            tuple.validate(),
            Err(ReleaseTupleError::InvalidEvidenceCommit { .. })
        ));
    }

    #[test]
    fn malformed_lock_digest_and_revision_are_rejected() {
        let mut tuple = valid_tuple();
        tuple.source_lock_sha256 = "deadbeef".to_string();
        assert!(matches!(
            tuple.validate(),
            Err(ReleaseTupleError::InvalidSourceLockDigest { .. })
        ));
        tuple = valid_tuple();
        tuple.release_contract_revision = "docs/OLD.md".to_string();
        assert!(matches!(
            tuple.validate(),
            Err(ReleaseTupleError::InvalidContractRevision { .. })
        ));
    }

    #[test]
    fn invalid_lumen_version_is_rejected() {
        let mut tuple = valid_tuple();
        tuple.lumen_version = "alpha".to_string();
        assert!(matches!(
            tuple.validate(),
            Err(ReleaseTupleError::InvalidLumenVersion { .. })
        ));
    }

    #[test]
    fn lock_digest_verifies_and_mismatch_is_rejected() {
        let lock_bytes = b"{\"schema_version\":1,\"lumen_version\":\"2.0.0-rc.1\"}\n";
        let digest = sha256_hex(lock_bytes);
        let tuple = ReleaseSourceTupleV1 {
            source_lock_sha256: digest.clone(),
            ..valid_tuple()
        };
        assert_eq!(tuple.verify(lock_bytes), Ok(()));
        assert!(matches!(
            tuple.verify(b"tampered"),
            Err(ReleaseTupleError::LockDigestMismatch { .. })
        ));
        let wrong = ReleaseSourceTupleV1 {
            source_lock_sha256: "0".repeat(64),
            ..valid_tuple()
        };
        assert!(matches!(
            wrong.verify(lock_bytes),
            Err(ReleaseTupleError::LockDigestMismatch { .. })
        ));
    }

    #[test]
    fn evidence_path_predicate_matrix() {
        assert!(is_evidence_only_path("SOURCE_LOCK.json"));
        assert!(is_evidence_only_path("SBOM.spdx.json"));
        assert!(is_evidence_only_path("artifacts/readiness/status.json"));
        assert!(is_evidence_only_path("artifacts/readiness/nested/receipt.json"));
        assert!(!is_evidence_only_path("VERSION"));
        assert!(!is_evidence_only_path("CHANGELOG.md"));
        assert!(!is_evidence_only_path("agent/Cargo.lock"));
        assert!(!is_evidence_only_path("agent/crates/codegen/xai-grok-pager/Cargo.toml"));
        assert!(!is_evidence_only_path("artifacts/readiness-evil/x"));
    }

    // ── git-backed relation check on a real throwaway repository ──────────

    fn fixture_repo() -> (tempfile::TempDir, PathBuf, String, String, String) {
        let dir = tempfile::tempdir().unwrap();
        let repo = dir.path().to_path_buf();
        let git = |args: &[&str]| {
            let out = Command::new("git")
                .arg("-C")
                .arg(&repo)
                .args(args)
                .output()
                .unwrap();
            assert!(out.status.success(), "git {args:?}: {}", String::from_utf8_lossy(&out.stderr));
            String::from_utf8_lossy(&out.stdout).trim().to_string()
        };
        git(&["init", "-q", "-b", "main"]);
        git(&["config", "user.email", "fixture@example.invalid"]);
        git(&["config", "user.name", "Fixture"]);
        std::fs::write(repo.join("VERSION"), "2.0.0-rc.1\n").unwrap();
        std::fs::write(
            repo.join("SOURCE_LOCK.json"),
            "{\"schema_version\":1,\"lumen_version\":\"2.0.0-rc.1\"}\n",
        )
        .unwrap();
        git(&["add", "."]);
        git(&["commit", "-qm", "chore(release): prepare v2.0.0-rc.1 source candidate"]);
        let a = git(&["rev-parse", "HEAD"]);
        // evidence-only suffix: readiness artifact + lock refresh
        std::fs::create_dir_all(repo.join("artifacts/readiness")).unwrap();
        std::fs::write(
            repo.join("artifacts/readiness/status.json"),
            "{\"schema_version\":1,\"version\":\"2.0.0-rc.1\",\"ready\":false}\n",
        )
        .unwrap();
        std::fs::write(
            repo.join("SOURCE_LOCK.json"),
            format!(
                "{{\"schema_version\":1,\"lumen_version\":\"2.0.0-rc.1\",\"monorepo\":{{\"git_head\":\"{a}\"}}}}\n"
            ),
        )
        .unwrap();
        git(&["add", "SOURCE_LOCK.json", "artifacts/readiness/status.json"]);
        git(&["commit", "-qm", "chore(release): evidence for v2.0.0-rc.1"]);
        let b = git(&["rev-parse", "HEAD"]);
        git(&["tag", "-a", "v2.0.0-rc.1", "-m", "Lumen v2.0.0-rc.1", &a]);
        (dir, repo, a, b, "v2.0.0-rc.1".to_string())
    }

    #[test]
    fn repo_relation_accepts_clean_source_evidence_and_tag() {
        let (_dir, repo, a, b, tag) = fixture_repo();
        let tuple = ReleaseSourceTupleV1 {
            schema: RELEASE_TUPLE_SCHEMA_V1.to_string(),
            source_commit: a,
            evidence_commit: b,
            release_tag: tag,
            lumen_version: "2.0.0-rc.1".to_string(),
            source_lock_sha256: "0".repeat(64),
            release_contract_revision: RELEASE_CONTRACT_REVISION.to_string(),
        };
        assert_eq!(tuple.verify_repo_relation(&repo), Ok(()));
    }

    #[test]
    fn repo_relation_rejects_source_change_smuggled_into_evidence_suffix() {
        let (_dir, repo, a, _b, _tag) = fixture_repo();
        // A second evidence commit that smuggles a source change into B.
        let git = |args: &[&str]| {
            let out = Command::new("git")
                .arg("-C")
                .arg(&repo)
                .args(args)
                .output()
                .unwrap();
            assert!(out.status.success(), "git {args:?}");
            String::from_utf8_lossy(&out.stdout).trim().to_string()
        };
        std::fs::write(repo.join("VERSION"), "2.0.0-rc.2\n").unwrap();
        git(&["add", "VERSION"]);
        git(&["commit", "-qm", "sneak a version change into the evidence suffix"]);
        let b2 = git(&["rev-parse", "HEAD"]);
        let tuple = ReleaseSourceTupleV1 {
            schema: RELEASE_TUPLE_SCHEMA_V1.to_string(),
            source_commit: a,
            evidence_commit: b2,
            release_tag: "v2.0.0-rc.1".to_string(),
            lumen_version: "2.0.0-rc.1".to_string(),
            source_lock_sha256: "0".repeat(64),
            release_contract_revision: RELEASE_CONTRACT_REVISION.to_string(),
        };
        assert!(matches!(
            tuple.verify_repo_relation(&repo),
            Err(ReleaseTupleError::NonEvidenceSuffix { path }) if path == "VERSION"
        ));
    }

    #[test]
    fn repo_relation_rejects_tag_pointing_at_evidence_not_source() {
        let (_dir, repo, a, b, _tag) = fixture_repo();
        // A tuple that claims the tag points at B (the wrong target).
        let tuple = ReleaseSourceTupleV1 {
            schema: RELEASE_TUPLE_SCHEMA_V1.to_string(),
            source_commit: a,
            evidence_commit: b.clone(),
            release_tag: "v2.0.0-rc.1".to_string(),
            lumen_version: "2.0.0-rc.1".to_string(),
            source_lock_sha256: "0".repeat(64),
            release_contract_revision: RELEASE_CONTRACT_REVISION.to_string(),
        };
        // Move the tag to B so the peel check must fail.
        let out = Command::new("git")
            .arg("-C")
            .arg(&repo)
            .args(["tag", "-f", "-a", "v2.0.0-rc.1", "-m", "moved", &b])
            .output()
            .unwrap();
        assert!(out.status.success());
        assert!(matches!(
            tuple.verify_repo_relation(&repo),
            Err(ReleaseTupleError::TagDoesNotPeelToSource { .. })
        ));
    }

    #[test]
    fn repo_relation_rejects_evidence_not_descendant_of_source() {
        let (_dir, repo, _a, b, _tag) = fixture_repo();
        // A tuple with an unrelated "source" commit (the empty tree).
        let out = Command::new("git")
            .arg("-C")
            .arg(&repo)
            .args(["mktree"])
            .output()
            .unwrap();
        let tree = String::from_utf8_lossy(&out.stdout).trim().to_string();
        let out = Command::new("git")
            .arg("-C")
            .arg(&repo)
            .args(["commit-tree", &tree])
            .output()
            .unwrap();
        let unrelated = String::from_utf8_lossy(&out.stdout).trim().to_string();
        let tuple = ReleaseSourceTupleV1 {
            schema: RELEASE_TUPLE_SCHEMA_V1.to_string(),
            source_commit: unrelated,
            evidence_commit: b,
            release_tag: "v2.0.0-rc.1".to_string(),
            lumen_version: "2.0.0-rc.1".to_string(),
            source_lock_sha256: "0".repeat(64),
            release_contract_revision: RELEASE_CONTRACT_REVISION.to_string(),
        };
        assert_eq!(
            tuple.verify_repo_relation(&repo),
            Err(ReleaseTupleError::EvidenceNotDescendant)
        );
    }
}
