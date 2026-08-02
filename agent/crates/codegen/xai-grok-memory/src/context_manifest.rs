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
    fn canonical_hash_is_stable_for_same_manifest() {
        let manifest = fixture();
        assert_eq!(
            manifest.manifest_hash().unwrap(),
            manifest.manifest_hash().unwrap()
        );
        assert!(manifest.manifest_hash().unwrap().starts_with("sha256:"));
    }

    #[test]
    fn rejects_forged_lineage_and_unsorted_contracts() {
        let mut manifest = fixture();
        manifest.lineage_path[0] = "foreign-root".into();
        assert!(manifest.validate().is_err());
        let mut manifest = fixture();
        manifest.permitted_tool_contract_hashes.reverse();
        assert!(manifest.validate().is_err());
    }

    #[test]
    fn rejects_unknown_schema_and_empty_authority_refs() {
        let mut manifest = fixture();
        manifest.schema_version = 2;
        assert!(manifest.validate().is_err());
        let mut manifest = fixture();
        manifest.capability_grant_id.clear();
        assert!(manifest.validate().is_err());
    }
}
