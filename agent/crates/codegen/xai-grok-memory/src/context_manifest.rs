//! Versioned, hash-addressed context admission data for governed task trees.
//!
//! This module is intentionally a pure contract. It does not render prompts,
//! read session history, or grant capabilities. The SessionActor remains the
//! authority that supplies the already-validated references and decides whether
//! a manifest may be consumed.

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ContextManifestV1 {
    pub schema_version: u16,
    pub task_tree_id: String,
    pub node_id: String,
    pub root_session_id: String,
    pub immediate_parent_id: Option<String>,
    pub lineage_path: Vec<String>,
    pub immutable_assignment_ref: String,
    pub immutable_assignment_hash: String,
    pub user_objective_ref: String,
    pub task_contract_hash: String,
    pub accepted_snapshot_ref: String,
    pub accepted_snapshot_hash: String,
    pub tool_catalog_hash: String,
    pub permitted_tool_contract_hashes: Vec<String>,
    pub capability_grant_id: String,
    pub policy_revision: u64,
    pub admission_profile: String,
    pub budget_reservation_id: String,
    pub deadline_unix: u64,
    pub permitted_artifact_refs: Vec<String>,
    pub model_selection_ref: Option<String>,
    pub parent_compaction_hash: Option<String>,
    pub producer_version: String,
    pub created_at_unix: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ContextManifestError {
    Invalid(String),
    Serialization(String),
}

impl std::fmt::Display for ContextManifestError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(message) => write!(f, "invalid context manifest: {message}"),
            Self::Serialization(message) => {
                write!(f, "context manifest serialization failed: {message}")
            }
        }
    }
}

impl std::error::Error for ContextManifestError {}

impl ContextManifestV1 {
    pub const SCHEMA_VERSION: u16 = 1;

    pub fn validate(&self) -> Result<(), ContextManifestError> {
        if self.schema_version != Self::SCHEMA_VERSION {
            return Err(ContextManifestError::Invalid(format!(
                "unsupported schema version {}",
                self.schema_version
            )));
        }
        for (name, value) in [
            ("task_tree_id", self.task_tree_id.as_str()),
            ("node_id", self.node_id.as_str()),
            ("root_session_id", self.root_session_id.as_str()),
            (
                "immutable_assignment_ref",
                self.immutable_assignment_ref.as_str(),
            ),
            (
                "immutable_assignment_hash",
                self.immutable_assignment_hash.as_str(),
            ),
            ("user_objective_ref", self.user_objective_ref.as_str()),
            ("task_contract_hash", self.task_contract_hash.as_str()),
            ("accepted_snapshot_ref", self.accepted_snapshot_ref.as_str()),
            (
                "accepted_snapshot_hash",
                self.accepted_snapshot_hash.as_str(),
            ),
            ("tool_catalog_hash", self.tool_catalog_hash.as_str()),
            ("capability_grant_id", self.capability_grant_id.as_str()),
            ("admission_profile", self.admission_profile.as_str()),
            ("budget_reservation_id", self.budget_reservation_id.as_str()),
            ("producer_version", self.producer_version.as_str()),
        ] {
            if value.trim().is_empty() {
                return Err(ContextManifestError::Invalid(format!(
                    "{name} must not be empty"
                )));
            }
        }
        if self.lineage_path.is_empty() || self.lineage_path[0] != self.root_session_id {
            return Err(ContextManifestError::Invalid(
                "lineage must start at root_session_id".to_owned(),
            ));
        }
        if self.lineage_path.last() != Some(&self.node_id) {
            return Err(ContextManifestError::Invalid(
                "lineage must end at node_id".to_owned(),
            ));
        }
        if self.immediate_parent_id.as_ref() == Some(&self.node_id) {
            return Err(ContextManifestError::Invalid(
                "node cannot be its own immediate parent".to_owned(),
            ));
        }
        if self
            .permitted_tool_contract_hashes
            .windows(2)
            .any(|w| w[0] > w[1])
            || self.permitted_artifact_refs.windows(2).any(|w| w[0] > w[1])
        {
            return Err(ContextManifestError::Invalid(
                "permitted collections must be canonically sorted".to_owned(),
            ));
        }
        Ok(())
    }

    pub fn canonical_bytes(&self) -> Result<Vec<u8>, ContextManifestError> {
        self.validate()?;
        serde_json::to_vec(self)
            .map_err(|error| ContextManifestError::Serialization(error.to_string()))
    }

    pub fn manifest_hash(&self) -> Result<String, ContextManifestError> {
        let digest = Sha256::digest(self.canonical_bytes()?);
        Ok(format!("sha256:{digest:x}"))
    }

    /// Bind admission to the exact root-owned ledger snapshot. A manifest may
    /// reference a snapshot, but it cannot invent the tree or hash that the
    /// ledger actually produced.
    pub fn validate_against_snapshot(
        &self,
        snapshot: &crate::task_ledger::AcceptedLedgerSnapshot,
    ) -> Result<(), ContextManifestError> {
        self.validate()?;
        if self.task_tree_id != snapshot.task_tree_id {
            return Err(ContextManifestError::Invalid(
                "manifest task tree does not match accepted snapshot".to_owned(),
            ));
        }
        if self.accepted_snapshot_hash != snapshot.accepted_set_hash
            && self.accepted_snapshot_hash != snapshot.journal_hash
        {
            return Err(ContextManifestError::Invalid(
                "manifest accepted snapshot hash does not match ledger".to_owned(),
            ));
        }
        Ok(())
    }

    /// Rewrite snapshot binding fields from a ledger-produced snapshot. Callers
    /// cannot invent accepted hashes; they must take them from
    /// [`crate::task_ledger::WorkingMemoryLedger::accepted_snapshot`].
    pub fn bind_accepted_snapshot(
        &mut self,
        snapshot: &crate::task_ledger::AcceptedLedgerSnapshot,
        snapshot_ref: impl Into<String>,
    ) -> Result<(), ContextManifestError> {
        if self.task_tree_id != snapshot.task_tree_id {
            return Err(ContextManifestError::Invalid(
                "cannot bind snapshot from a foreign task tree".to_owned(),
            ));
        }
        self.accepted_snapshot_ref = snapshot_ref.into();
        self.accepted_snapshot_hash = snapshot.accepted_set_hash.clone();
        self.validate_against_snapshot(snapshot)
    }
}

/// How a ContextManifest may be consumed.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ManifestAdmissionMode {
    /// New governed child spawn — requires validated manifest + live snapshot.
    GovernedSpawn,
    /// Resume / completion reconciliation — requires hash match + live snapshot.
    GovernedResume,
    /// Legacy sessions with no manifest. Read/close only; never automatic
    /// re-admission into a governed tree.
    LegacyNoManifest,
}

/// Machine-readable denial for ContextManifest admission.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ManifestAdmissionDenyReason {
    LegacyNoManifestCannotAdmit,
    MissingManifest,
    MissingSnapshot,
    ManifestInvalid,
    SnapshotMismatch,
    ForgedManifestHash,
    EmptyManifestHash,
    ParentLineageMismatch,
    ForeignTaskTree,
}

impl ManifestAdmissionDenyReason {
    pub const fn code(self) -> &'static str {
        match self {
            Self::LegacyNoManifestCannotAdmit => "manifest.legacy_no_manifest_cannot_admit",
            Self::MissingManifest => "manifest.missing",
            Self::MissingSnapshot => "manifest.missing_snapshot",
            Self::ManifestInvalid => "manifest.invalid",
            Self::SnapshotMismatch => "manifest.snapshot_mismatch",
            Self::ForgedManifestHash => "manifest.forged_hash",
            Self::EmptyManifestHash => "manifest.empty_hash",
            Self::ParentLineageMismatch => "manifest.parent_lineage_mismatch",
            Self::ForeignTaskTree => "manifest.foreign_task_tree",
        }
    }
}

impl std::fmt::Display for ManifestAdmissionDenyReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.code())
    }
}

/// Inputs for a single ContextManifest admission decision.
#[derive(Debug, Clone)]
pub struct ManifestAdmissionRequest<'a> {
    pub mode: ManifestAdmissionMode,
    pub manifest: Option<&'a ContextManifestV1>,
    pub live_snapshot: Option<&'a crate::task_ledger::AcceptedLedgerSnapshot>,
    /// Expected identity from the host (spawn receipt / resume source).
    pub expected_manifest_hash: Option<&'a str>,
    pub expected_root_session_id: Option<&'a str>,
    pub expected_node_id: Option<&'a str>,
    pub expected_parent_id: Option<&'a str>,
}

/// Admit a ContextManifest for spawn/resume, or fail closed.
///
/// [`ManifestAdmissionMode::LegacyNoManifest`] always returns
/// [`ManifestAdmissionDenyReason::LegacyNoManifestCannotAdmit`] when used for
/// automatic governed re-admission. Legacy sessions may still be read/closed
/// by host paths that never call this function.
pub fn admit_context_manifest(
    request: &ManifestAdmissionRequest<'_>,
) -> Result<String, ManifestAdmissionDenyReason> {
    match request.mode {
        ManifestAdmissionMode::LegacyNoManifest => {
            Err(ManifestAdmissionDenyReason::LegacyNoManifestCannotAdmit)
        }
        ManifestAdmissionMode::GovernedSpawn | ManifestAdmissionMode::GovernedResume => {
            admit_governed(request)
        }
    }
}

/// Production admission for the reduced host `GovernedSpawnAdmission` receipt
/// plus a live ledger snapshot. SessionActor spawn paths must call this (or
/// [`admit_context_manifest`]) so LegacyNoManifest cannot auto re-admit.
pub fn admit_spawn_receipt(
    task_tree_id: &str,
    root_session_id: &str,
    node_id: &str,
    manifest_hash: &str,
    accepted_snapshot_hash: &str,
    live_snapshot: &crate::task_ledger::AcceptedLedgerSnapshot,
    expected_parent_id: Option<&str>,
    lineage_path: &[String],
) -> Result<(), ManifestAdmissionDenyReason> {
    if task_tree_id.trim().is_empty()
        || root_session_id.trim().is_empty()
        || node_id.trim().is_empty()
        || manifest_hash.trim().is_empty()
        || accepted_snapshot_hash.trim().is_empty()
    {
        return Err(ManifestAdmissionDenyReason::EmptyManifestHash);
    }
    if live_snapshot.task_tree_id != task_tree_id {
        return Err(ManifestAdmissionDenyReason::ForeignTaskTree);
    }
    if accepted_snapshot_hash != live_snapshot.accepted_set_hash
        && accepted_snapshot_hash != live_snapshot.journal_hash
    {
        return Err(ManifestAdmissionDenyReason::SnapshotMismatch);
    }
    if lineage_path.is_empty() || lineage_path[0] != root_session_id {
        return Err(ManifestAdmissionDenyReason::ParentLineageMismatch);
    }
    if lineage_path.last().map(String::as_str) != Some(node_id) {
        return Err(ManifestAdmissionDenyReason::ParentLineageMismatch);
    }
    if let Some(parent) = expected_parent_id
        && (lineage_path.len() < 2 || lineage_path[lineage_path.len() - 2] != parent)
    {
        return Err(ManifestAdmissionDenyReason::ParentLineageMismatch);
    }
    Ok(())
}

fn admit_governed(
    request: &ManifestAdmissionRequest<'_>,
) -> Result<String, ManifestAdmissionDenyReason> {
    let manifest = request
        .manifest
        .ok_or(ManifestAdmissionDenyReason::MissingManifest)?;
    let snapshot = request
        .live_snapshot
        .ok_or(ManifestAdmissionDenyReason::MissingSnapshot)?;
    manifest
        .validate()
        .map_err(|_| ManifestAdmissionDenyReason::ManifestInvalid)?;
    let computed = manifest
        .manifest_hash()
        .map_err(|_| ManifestAdmissionDenyReason::ManifestInvalid)?;
    match request.expected_manifest_hash.map(str::trim) {
        None | Some("") => return Err(ManifestAdmissionDenyReason::EmptyManifestHash),
        Some(expected) if expected != computed => {
            return Err(ManifestAdmissionDenyReason::ForgedManifestHash);
        }
        Some(_) => {}
    }
    if let Some(root) = request.expected_root_session_id
        && root != manifest.root_session_id
    {
        return Err(ManifestAdmissionDenyReason::ForeignTaskTree);
    }
    if manifest.task_tree_id != snapshot.task_tree_id {
        return Err(ManifestAdmissionDenyReason::ForeignTaskTree);
    }
    if let Some(node) = request.expected_node_id
        && node != manifest.node_id
    {
        return Err(ManifestAdmissionDenyReason::ParentLineageMismatch);
    }
    if let Some(parent) = request.expected_parent_id
        && manifest.immediate_parent_id.as_deref() != Some(parent)
    {
        return Err(ManifestAdmissionDenyReason::ParentLineageMismatch);
    }
    manifest
        .validate_against_snapshot(snapshot)
        .map_err(|_| ManifestAdmissionDenyReason::SnapshotMismatch)?;
    Ok(computed)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> ContextManifestV1 {
        ContextManifestV1 {
            schema_version: 1,
            task_tree_id: "tree-1".into(),
            node_id: "node-2".into(),
            root_session_id: "node-1".into(),
            immediate_parent_id: Some("node-1".into()),
            lineage_path: vec!["node-1".into(), "node-2".into()],
            immutable_assignment_ref: "artifact://assignment".into(),
            immutable_assignment_hash: "sha256:assignment".into(),
            user_objective_ref: "artifact://objective".into(),
            task_contract_hash: "sha256:contract".into(),
            accepted_snapshot_ref: "ledger://snapshot".into(),
            accepted_snapshot_hash: "sha256:snapshot".into(),
            tool_catalog_hash: "sha256:tools".into(),
            permitted_tool_contract_hashes: vec!["sha256:a".into(), "sha256:b".into()],
            capability_grant_id: "grant-1".into(),
            policy_revision: 3,
            admission_profile: "governed_tree_development".into(),
            budget_reservation_id: "budget-1".into(),
            deadline_unix: 2_000_000_000,
            permitted_artifact_refs: vec!["artifact://a".into(), "artifact://b".into()],
            model_selection_ref: None,
            parent_compaction_hash: None,
            producer_version: "2.0.0-alpha.1".into(),
            created_at_unix: 1_000_000_000,
        }
    }

    #[test]
    fn context_manifest_v1_canonical_hash_is_stable_for_same_manifest() {
        let manifest = fixture();
        assert_eq!(
            manifest.manifest_hash().unwrap(),
            manifest.manifest_hash().unwrap()
        );
        assert!(manifest.manifest_hash().unwrap().starts_with("sha256:"));
    }

    #[test]
    fn context_manifest_v1_rejects_forged_lineage_and_unsorted_contracts() {
        let mut manifest = fixture();
        manifest.lineage_path[0] = "foreign-root".into();
        assert!(manifest.validate().is_err());
        let mut manifest = fixture();
        manifest.permitted_tool_contract_hashes.reverse();
        assert!(manifest.validate().is_err());
    }

    #[test]
    fn context_manifest_v1_rejects_unknown_schema_and_empty_authority_refs() {
        let mut manifest = fixture();
        manifest.schema_version = 2;
        assert!(manifest.validate().is_err());
        let mut manifest = fixture();
        manifest.capability_grant_id.clear();
        assert!(manifest.validate().is_err());
    }

    #[test]
    fn context_manifest_v1_snapshot_binding_rejects_foreign_tree_or_hash() {
        let manifest = fixture();
        let snapshot = crate::task_ledger::AcceptedLedgerSnapshot {
            task_tree_id: "tree-1".into(),
            record_count: 1,
            accepted_count: 1,
            accepted_set_hash: "sha256:snapshot".into(),
            journal_hash: "sha256:journal".into(),
        };
        assert!(manifest.validate_against_snapshot(&snapshot).is_ok());
        let mut foreign = snapshot.clone();
        foreign.task_tree_id = "tree-foreign".into();
        assert!(manifest.validate_against_snapshot(&foreign).is_err());
        foreign.task_tree_id = "tree-1".into();
        foreign.accepted_set_hash = "sha256:wrong".into();
        foreign.journal_hash = "sha256:wrong".into();
        assert!(manifest.validate_against_snapshot(&foreign).is_err());
    }

    #[test]
    fn context_manifest_v1_legacy_no_manifest_cannot_auto_admit() {
        let err = admit_context_manifest(&ManifestAdmissionRequest {
            mode: ManifestAdmissionMode::LegacyNoManifest,
            manifest: None,
            live_snapshot: None,
            expected_manifest_hash: None,
            expected_root_session_id: None,
            expected_node_id: None,
            expected_parent_id: None,
        })
        .unwrap_err();
        assert_eq!(
            err,
            ManifestAdmissionDenyReason::LegacyNoManifestCannotAdmit
        );
        assert_eq!(err.code(), "manifest.legacy_no_manifest_cannot_admit");
    }

    #[test]
    fn context_manifest_v1_forged_or_empty_manifest_hash_fail_closed() {
        let mut manifest = fixture();
        let snapshot = crate::task_ledger::AcceptedLedgerSnapshot {
            task_tree_id: "tree-1".into(),
            record_count: 1,
            accepted_count: 1,
            accepted_set_hash: "sha256:snapshot".into(),
            journal_hash: "sha256:journal".into(),
        };
        manifest
            .bind_accepted_snapshot(&snapshot, "ledger://snap")
            .unwrap();
        let hash = manifest.manifest_hash().unwrap();
        assert_eq!(
            admit_context_manifest(&ManifestAdmissionRequest {
                mode: ManifestAdmissionMode::GovernedSpawn,
                manifest: Some(&manifest),
                live_snapshot: Some(&snapshot),
                expected_manifest_hash: Some(""),
                expected_root_session_id: Some("node-1"),
                expected_node_id: Some("node-2"),
                expected_parent_id: Some("node-1"),
            })
            .unwrap_err(),
            ManifestAdmissionDenyReason::EmptyManifestHash
        );
        assert_eq!(
            admit_context_manifest(&ManifestAdmissionRequest {
                mode: ManifestAdmissionMode::GovernedResume,
                manifest: Some(&manifest),
                live_snapshot: Some(&snapshot),
                expected_manifest_hash: Some("sha256:forged"),
                expected_root_session_id: Some("node-1"),
                expected_node_id: Some("node-2"),
                expected_parent_id: Some("node-1"),
            })
            .unwrap_err(),
            ManifestAdmissionDenyReason::ForgedManifestHash
        );
        let admitted = admit_context_manifest(&ManifestAdmissionRequest {
            mode: ManifestAdmissionMode::GovernedSpawn,
            manifest: Some(&manifest),
            live_snapshot: Some(&snapshot),
            expected_manifest_hash: Some(&hash),
            expected_root_session_id: Some("node-1"),
            expected_node_id: Some("node-2"),
            expected_parent_id: Some("node-1"),
        })
        .unwrap();
        assert_eq!(admitted, hash);
    }

    #[test]
    fn context_manifest_v1_admit_spawn_receipt_accepts_live_snapshot_and_denies_stale_foreign_empty() {
        let snapshot = crate::task_ledger::AcceptedLedgerSnapshot {
            task_tree_id: "root".into(),
            record_count: 1,
            accepted_count: 1,
            accepted_set_hash: "sha256:accepted".into(),
            journal_hash: "sha256:journal".into(),
        };
        let lineage = vec!["root".into(), "child".into()];
        assert!(
            admit_spawn_receipt(
                "root",
                "root",
                "child",
                "sha256:manifest",
                "sha256:accepted",
                &snapshot,
                Some("root"),
                &lineage,
            )
            .is_ok()
        );
        // Empty identity
        assert_eq!(
            admit_spawn_receipt(
                "root",
                "root",
                "child",
                "",
                "sha256:accepted",
                &snapshot,
                Some("root"),
                &lineage,
            )
            .unwrap_err(),
            ManifestAdmissionDenyReason::EmptyManifestHash
        );
        // Stale snapshot hash
        assert_eq!(
            admit_spawn_receipt(
                "root",
                "root",
                "child",
                "sha256:manifest",
                "sha256:stale",
                &snapshot,
                Some("root"),
                &lineage,
            )
            .unwrap_err(),
            ManifestAdmissionDenyReason::SnapshotMismatch
        );
        // Foreign tree
        let mut foreign = snapshot.clone();
        foreign.task_tree_id = "other".into();
        assert_eq!(
            admit_spawn_receipt(
                "root",
                "root",
                "child",
                "sha256:manifest",
                "sha256:accepted",
                &foreign,
                Some("root"),
                &lineage,
            )
            .unwrap_err(),
            ManifestAdmissionDenyReason::ForeignTaskTree
        );
        // Forged parent / lineage
        assert_eq!(
            admit_spawn_receipt(
                "root",
                "root",
                "child",
                "sha256:manifest",
                "sha256:accepted",
                &snapshot,
                Some("not-parent"),
                &lineage,
            )
            .unwrap_err(),
            ManifestAdmissionDenyReason::ParentLineageMismatch
        );
    }

    #[test]
    fn context_manifest_v1_stale_snapshot_denies_admission_after_journal_moves() {
        let mut manifest = fixture();
        let old = crate::task_ledger::AcceptedLedgerSnapshot {
            task_tree_id: "tree-1".into(),
            record_count: 1,
            accepted_count: 1,
            accepted_set_hash: "sha256:snapshot".into(),
            journal_hash: "sha256:journal".into(),
        };
        manifest
            .bind_accepted_snapshot(&old, "ledger://old")
            .unwrap();
        let hash = manifest.manifest_hash().unwrap();
        let newer = crate::task_ledger::AcceptedLedgerSnapshot {
            task_tree_id: "tree-1".into(),
            record_count: 2,
            accepted_count: 2,
            accepted_set_hash: "sha256:newer".into(),
            journal_hash: "sha256:newer-journal".into(),
        };
        assert_eq!(
            admit_context_manifest(&ManifestAdmissionRequest {
                mode: ManifestAdmissionMode::GovernedResume,
                manifest: Some(&manifest),
                live_snapshot: Some(&newer),
                expected_manifest_hash: Some(&hash),
                expected_root_session_id: Some("node-1"),
                expected_node_id: Some("node-2"),
                expected_parent_id: Some("node-1"),
            })
            .unwrap_err(),
            ManifestAdmissionDenyReason::SnapshotMismatch
        );
    }
}
