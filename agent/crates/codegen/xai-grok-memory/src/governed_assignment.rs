//! Immutable, root-authorized assignment contract for one governed child.
//!
//! This is deliberately a data contract, not an agent or a second authority.
//! The root `SessionActor` must create and durably retain it; consumers can
//! derive a manifest and spawn admission only after validating its hash.

use std::collections::BTreeMap;
use std::io::ErrorKind;
use std::path::{Component, Path, PathBuf};
use std::sync::{Arc, Mutex};

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::context_manifest::{ContextManifestError, ContextManifestV1};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct RootGovernedAssignmentV1 {
    pub schema_version: u16,
    pub task_tree_id: String,
    pub root_session_id: String,
    pub node_id: String,
    pub immediate_parent_id: Option<String>,
    pub lineage_path: Vec<String>,
    pub assignment_ref: String,
    pub user_objective_ref: String,
    pub task_contract_hash: String,
    pub accepted_snapshot_ref: String,
    pub accepted_snapshot_hash: String,
    pub tool_catalog_hash: String,
    pub permitted_tool_contract_hashes: Vec<String>,
    pub capability_grant_id: String,
    pub policy_revision: u64,
    pub budget_reservation_id: String,
    pub deadline_unix: u64,
    pub permitted_artifact_refs: Vec<String>,
    pub write_scope_roots: Vec<PathBuf>,
    pub model_selection_ref: Option<String>,
    pub parent_compaction_hash: Option<String>,
    pub producer_version: String,
    pub created_at_unix: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RootGovernedAssignmentError {
    Invalid(String),
    Serialization(String),
    Manifest(ContextManifestError),
    Persistence(String),
    AssignmentConflict { node_id: String },
    NotFound { node_id: String },
}

impl std::fmt::Display for RootGovernedAssignmentError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(message) => write!(f, "invalid root governed assignment: {message}"),
            Self::Serialization(message) => write!(f, "assignment serialization failed: {message}"),
            Self::Manifest(error) => write!(f, "assignment manifest conversion failed: {error}"),
            Self::Persistence(error) => write!(f, "assignment persistence failed: {error}"),
            Self::AssignmentConflict { node_id } => {
                write!(
                    f,
                    "root assignment for node {node_id:?} conflicts with existing receipt"
                )
            }
            Self::NotFound { node_id } => {
                write!(f, "root assignment for node {node_id:?} was not found")
            }
        }
    }
}

/// Root-owned durable registry for immutable child assignments.
///
/// It is intentionally keyed by node id, not an opaque caller-supplied
/// request id: a restarted host can only reissue byte-identical authority for
/// that node. A changed task contract, scope, budget, or lineage is a conflict
/// requiring an explicit new node/assignment lifecycle.
#[derive(Debug, Clone)]
pub struct RootGovernedAssignmentStore {
    root_session_id: String,
    path: PathBuf,
    assignments: Arc<Mutex<BTreeMap<String, RootGovernedAssignmentV1>>>,
    healthy: Arc<Mutex<bool>>,
}

impl RootGovernedAssignmentStore {
    pub fn with_path(root_session_id: impl Into<String>, path: impl Into<PathBuf>) -> Self {
        let root_session_id = root_session_id.into();
        let path = path.into();
        let store = Self {
            root_session_id: root_session_id.clone(),
            path: path.clone(),
            assignments: Arc::new(Mutex::new(BTreeMap::new())),
            healthy: Arc::new(Mutex::new(true)),
        };
        match std::fs::read(&path) {
            Ok(bytes) => match serde_json::from_slice::<Vec<RootGovernedAssignmentV1>>(&bytes) {
                Ok(assignments) => {
                    let mut by_node = store.assignments.lock().expect("assignment store lock");
                    for assignment in assignments {
                        if assignment.validate().is_err()
                            || assignment.root_session_id != root_session_id
                            || by_node
                                .insert(assignment.node_id.clone(), assignment)
                                .is_some()
                        {
                            store.mark_unhealthy();
                            break;
                        }
                    }
                }
                Err(_) => store.mark_unhealthy(),
            },
            Err(error) if error.kind() == ErrorKind::NotFound => {}
            Err(_) => store.mark_unhealthy(),
        }
        store
    }

    pub fn issue(
        &self,
        assignment: RootGovernedAssignmentV1,
    ) -> Result<RootGovernedAssignmentV1, RootGovernedAssignmentError> {
        self.ensure_healthy()?;
        assignment.validate()?;
        if assignment.root_session_id != self.root_session_id {
            return Err(RootGovernedAssignmentError::Invalid(
                "assignment root does not match root-owned store".to_owned(),
            ));
        }
        let mut assignments = self.assignments.lock().expect("assignment store lock");
        if let Some(existing) = assignments.get(&assignment.node_id) {
            if existing.assignment_hash()? != assignment.assignment_hash()? {
                return Err(RootGovernedAssignmentError::AssignmentConflict {
                    node_id: assignment.node_id,
                });
            }
            return Ok(existing.clone());
        }
        assignments.insert(assignment.node_id.clone(), assignment.clone());
        if let Err(error) = self.persist(&assignments) {
            assignments.remove(&assignment.node_id);
            self.mark_unhealthy();
            return Err(error);
        }
        Ok(assignment)
    }

    pub fn get(
        &self,
        node_id: &str,
    ) -> Result<RootGovernedAssignmentV1, RootGovernedAssignmentError> {
        self.ensure_healthy()?;
        self.assignments
            .lock()
            .expect("assignment store lock")
            .get(node_id)
            .cloned()
            .ok_or_else(|| RootGovernedAssignmentError::NotFound {
                node_id: node_id.to_owned(),
            })
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    fn ensure_healthy(&self) -> Result<(), RootGovernedAssignmentError> {
        if *self.healthy.lock().expect("assignment health lock") {
            Ok(())
        } else {
            Err(RootGovernedAssignmentError::Persistence(
                "assignment journal is unavailable".to_owned(),
            ))
        }
    }

    fn mark_unhealthy(&self) {
        *self.healthy.lock().expect("assignment health lock") = false;
    }

    fn persist(
        &self,
        assignments: &BTreeMap<String, RootGovernedAssignmentV1>,
    ) -> Result<(), RootGovernedAssignmentError> {
        let parent = self.path.parent().ok_or_else(|| {
            RootGovernedAssignmentError::Persistence("assignment path has no parent".to_owned())
        })?;
        std::fs::create_dir_all(parent)
            .map_err(|error| RootGovernedAssignmentError::Persistence(error.to_string()))?;
        let bytes = serde_json::to_vec(&assignments.values().collect::<Vec<_>>())
            .map_err(|error| RootGovernedAssignmentError::Persistence(error.to_string()))?;
        let mut temp = tempfile::NamedTempFile::new_in(parent)
            .map_err(|error| RootGovernedAssignmentError::Persistence(error.to_string()))?;
        use std::io::Write;
        temp.write_all(&bytes)
            .map_err(|error| RootGovernedAssignmentError::Persistence(error.to_string()))?;
        temp.as_file()
            .sync_all()
            .map_err(|error| RootGovernedAssignmentError::Persistence(error.to_string()))?;
        temp.persist(&self.path)
            .map_err(|error| RootGovernedAssignmentError::Persistence(error.error.to_string()))?;
        Ok(())
    }
}

impl std::error::Error for RootGovernedAssignmentError {}

impl From<ContextManifestError> for RootGovernedAssignmentError {
    fn from(error: ContextManifestError) -> Self {
        Self::Manifest(error)
    }
}

impl RootGovernedAssignmentV1 {
    pub const SCHEMA_VERSION: u16 = 1;

    pub fn validate(&self) -> Result<(), RootGovernedAssignmentError> {
        if self.schema_version != Self::SCHEMA_VERSION {
            return Err(RootGovernedAssignmentError::Invalid(format!(
                "unsupported schema version {}",
                self.schema_version
            )));
        }
        for (name, value) in [
            ("task_tree_id", self.task_tree_id.as_str()),
            ("root_session_id", self.root_session_id.as_str()),
            ("node_id", self.node_id.as_str()),
            ("assignment_ref", self.assignment_ref.as_str()),
            ("user_objective_ref", self.user_objective_ref.as_str()),
            ("task_contract_hash", self.task_contract_hash.as_str()),
            ("accepted_snapshot_ref", self.accepted_snapshot_ref.as_str()),
            (
                "accepted_snapshot_hash",
                self.accepted_snapshot_hash.as_str(),
            ),
            ("tool_catalog_hash", self.tool_catalog_hash.as_str()),
            ("capability_grant_id", self.capability_grant_id.as_str()),
            ("budget_reservation_id", self.budget_reservation_id.as_str()),
            ("producer_version", self.producer_version.as_str()),
        ] {
            if value.trim().is_empty() {
                return Err(RootGovernedAssignmentError::Invalid(format!(
                    "{name} must not be empty"
                )));
            }
        }
        if self.task_tree_id != self.root_session_id {
            return Err(RootGovernedAssignmentError::Invalid(
                "task tree id must equal root session id".to_owned(),
            ));
        }
        if self.node_id == self.root_session_id
            || self.immediate_parent_id.is_none()
            || self.lineage_path.len() < 2
            || self.lineage_path.is_empty()
            || self.lineage_path[0] != self.root_session_id
            || self.lineage_path.last() != Some(&self.node_id)
        {
            return Err(RootGovernedAssignmentError::Invalid(
                "assignment must name a non-root child with lineage from root".to_owned(),
            ));
        }
        let depth = self.lineage_path.len().saturating_sub(1) as u32;
        if depth > xai_grok_tools::implementations::grok_build::task::HARD_MAX_SUBAGENT_DEPTH {
            return Err(RootGovernedAssignmentError::Invalid(
                "assignment may not exceed the hard task-tree depth ceiling".to_owned(),
            ));
        }
        if let Some(parent) = &self.immediate_parent_id {
            if parent == &self.node_id
                || self.lineage_path.len() < 2
                || self.lineage_path[self.lineage_path.len() - 2] != *parent
            {
                return Err(RootGovernedAssignmentError::Invalid(
                    "immediate parent must be the direct lineage predecessor".to_owned(),
                ));
            }
        }
        if self
            .permitted_tool_contract_hashes
            .windows(2)
            .any(|items| items[0] > items[1])
            || self
                .permitted_artifact_refs
                .windows(2)
                .any(|items| items[0] > items[1])
            || self
                .write_scope_roots
                .windows(2)
                .any(|items| items[0] > items[1])
        {
            return Err(RootGovernedAssignmentError::Invalid(
                "permitted collections and write roots must be canonically sorted".to_owned(),
            ));
        }
        if self.write_scope_roots.iter().any(|root| {
            root.as_os_str().is_empty()
                || root
                    .components()
                    .any(|component| matches!(component, Component::ParentDir))
        }) {
            return Err(RootGovernedAssignmentError::Invalid(
                "write roots must be non-empty and may not escape their workspace".to_owned(),
            ));
        }
        if depth == xai_grok_tools::implementations::grok_build::task::HARD_MAX_SUBAGENT_DEPTH
            && !self.write_scope_roots.is_empty()
        {
            return Err(RootGovernedAssignmentError::Invalid(
                "depth-three evidence leaves may not receive a write scope".to_owned(),
            ));
        }
        Ok(())
    }

    pub fn canonical_bytes(&self) -> Result<Vec<u8>, RootGovernedAssignmentError> {
        self.validate()?;
        serde_json::to_vec(self)
            .map_err(|error| RootGovernedAssignmentError::Serialization(error.to_string()))
    }

    pub fn assignment_hash(&self) -> Result<String, RootGovernedAssignmentError> {
        Ok(format!(
            "sha256:{:x}",
            Sha256::digest(self.canonical_bytes()?)
        ))
    }

    pub fn context_manifest(&self) -> Result<ContextManifestV1, RootGovernedAssignmentError> {
        let manifest = ContextManifestV1 {
            schema_version: ContextManifestV1::SCHEMA_VERSION,
            task_tree_id: self.task_tree_id.clone(),
            node_id: self.node_id.clone(),
            root_session_id: self.root_session_id.clone(),
            immediate_parent_id: self.immediate_parent_id.clone(),
            lineage_path: self.lineage_path.clone(),
            immutable_assignment_ref: self.assignment_ref.clone(),
            immutable_assignment_hash: self.assignment_hash()?,
            user_objective_ref: self.user_objective_ref.clone(),
            task_contract_hash: self.task_contract_hash.clone(),
            accepted_snapshot_ref: self.accepted_snapshot_ref.clone(),
            accepted_snapshot_hash: self.accepted_snapshot_hash.clone(),
            tool_catalog_hash: self.tool_catalog_hash.clone(),
            permitted_tool_contract_hashes: self.permitted_tool_contract_hashes.clone(),
            capability_grant_id: self.capability_grant_id.clone(),
            policy_revision: self.policy_revision,
            admission_profile: "governed_tree_development".to_owned(),
            budget_reservation_id: self.budget_reservation_id.clone(),
            deadline_unix: self.deadline_unix,
            permitted_artifact_refs: self.permitted_artifact_refs.clone(),
            model_selection_ref: self.model_selection_ref.clone(),
            parent_compaction_hash: self.parent_compaction_hash.clone(),
            producer_version: self.producer_version.clone(),
            created_at_unix: self.created_at_unix,
        };
        manifest.validate()?;
        Ok(manifest)
    }

    pub fn spawn_admission(
        &self,
    ) -> Result<
        xai_grok_tools::implementations::grok_build::task::types::GovernedSpawnAdmission,
        RootGovernedAssignmentError,
    > {
        let manifest = self.context_manifest()?;
        let mut admission =
            xai_grok_tools::implementations::grok_build::task::types::GovernedSpawnAdmission {
                task_tree_id: self.task_tree_id.clone(),
                root_session_id: self.root_session_id.clone(),
                node_id: self.node_id.clone(),
                manifest_hash: String::new(),
                accepted_snapshot_hash: self.accepted_snapshot_hash.clone(),
                immutable_assignment_hash: self.assignment_hash()?,
                tool_catalog_hash: self.tool_catalog_hash.clone(),
                policy_revision: self.policy_revision,
                budget_reservation_id: self.budget_reservation_id.clone(),
                write_scope_roots: self.write_scope_roots.clone(),
            };
        admission.manifest_hash = manifest.manifest_hash()?;
        Ok(admission)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn assignment() -> RootGovernedAssignmentV1 {
        RootGovernedAssignmentV1 {
            schema_version: 1,
            task_tree_id: "root".into(),
            root_session_id: "root".into(),
            node_id: "code".into(),
            immediate_parent_id: Some("root".into()),
            lineage_path: vec!["root".into(), "code".into()],
            assignment_ref: "artifact://assignment/code-v1".into(),
            user_objective_ref: "artifact://objective/root".into(),
            task_contract_hash: "sha256:contract".into(),
            accepted_snapshot_ref: "ledger://root/1".into(),
            accepted_snapshot_hash: "sha256:accepted".into(),
            tool_catalog_hash: "sha256:tools".into(),
            permitted_tool_contract_hashes: vec!["sha256:read".into(), "sha256:write".into()],
            capability_grant_id: "grant-code".into(),
            policy_revision: 7,
            budget_reservation_id: "budget-code".into(),
            deadline_unix: 2_000_000_000,
            permitted_artifact_refs: vec!["artifact://input".into()],
            write_scope_roots: vec![PathBuf::from("src"), PathBuf::from("tests")],
            model_selection_ref: None,
            parent_compaction_hash: None,
            producer_version: "lumen-nextgen".into(),
            created_at_unix: 1_700_000_000,
        }
    }

    #[test]
    fn root_assignment_binds_manifest_and_spawn_admission_to_same_hashes() {
        let assignment = assignment();
        let assignment_hash = assignment.assignment_hash().unwrap();
        let manifest = assignment.context_manifest().unwrap();
        let admission = assignment.spawn_admission().unwrap();
        assert_eq!(manifest.immutable_assignment_hash, assignment_hash);
        assert_eq!(admission.immutable_assignment_hash, assignment_hash);
        assert_eq!(admission.manifest_hash, manifest.manifest_hash().unwrap());
        assert_eq!(admission.write_scope_roots, assignment.write_scope_roots);
    }

    #[test]
    fn root_assignment_rejects_unsorted_or_escaping_write_scope() {
        let mut bad = assignment();
        bad.write_scope_roots = vec![PathBuf::from("tests"), PathBuf::from("src")];
        assert!(bad.validate().is_err());
        bad.write_scope_roots = vec![PathBuf::from("../escape")];
        assert!(bad.validate().is_err());
    }

    #[test]
    fn root_assignment_rejects_root_self_depth_four_and_writable_leaf() {
        let mut bad = assignment();
        bad.node_id = "root".into();
        bad.immediate_parent_id = None;
        bad.lineage_path = vec!["root".into()];
        assert!(bad.validate().is_err());

        let mut depth_four = assignment();
        depth_four.node_id = "depth-four".into();
        depth_four.immediate_parent_id = Some("evidence".into());
        depth_four.lineage_path = vec![
            "root".into(),
            "code".into(),
            "review".into(),
            "evidence".into(),
            "depth-four".into(),
        ];
        assert!(depth_four.validate().is_err());

        let mut writable_leaf = assignment();
        writable_leaf.node_id = "evidence".into();
        writable_leaf.immediate_parent_id = Some("review".into());
        writable_leaf.lineage_path = vec![
            "root".into(),
            "code".into(),
            "review".into(),
            "evidence".into(),
        ];
        writable_leaf.write_scope_roots = vec![PathBuf::from("src")];
        assert!(writable_leaf.validate().is_err());
    }

    #[test]
    fn durable_root_assignment_store_recovers_idempotently_and_rejects_replacement() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("assignments.json");
        let store = RootGovernedAssignmentStore::with_path("root", &path);
        let issued = store.issue(assignment()).unwrap();
        assert_eq!(store.issue(assignment()).unwrap(), issued);
        let recovered = RootGovernedAssignmentStore::with_path("root", &path);
        assert_eq!(recovered.get("code").unwrap(), issued);
        let mut changed = assignment();
        changed.write_scope_roots = vec![PathBuf::from("src")];
        assert!(matches!(
            recovered.issue(changed),
            Err(RootGovernedAssignmentError::AssignmentConflict { .. })
        ));
    }

    #[test]
    fn durable_root_assignment_store_keeps_distinct_nodes_in_one_tree_journal() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("assignments.json");
        let store = RootGovernedAssignmentStore::with_path("root", &path);
        let code = assignment();
        let mut review = assignment();
        review.node_id = "review".into();
        review.immediate_parent_id = Some("code".into());
        review.lineage_path = vec!["root".into(), "code".into(), "review".into()];
        review.assignment_ref = "artifact://assignment/review-v1".into();
        review.capability_grant_id = "grant-review".into();
        review.budget_reservation_id = "budget-review".into();
        review.write_scope_roots = vec![PathBuf::from("tests")];

        store.issue(code.clone()).unwrap();
        store.issue(review.clone()).unwrap();
        let recovered = RootGovernedAssignmentStore::with_path("root", &path);
        assert_eq!(recovered.get("code").unwrap(), code);
        assert_eq!(recovered.get("review").unwrap(), review);
    }

    #[test]
    fn corrupt_or_foreign_assignment_journal_fails_closed() {
        let temp = tempfile::tempdir().unwrap();
        let corrupt = temp.path().join("corrupt.json");
        std::fs::write(&corrupt, b"not json").unwrap();
        let store = RootGovernedAssignmentStore::with_path("root", corrupt);
        assert!(matches!(
            store.get("code"),
            Err(RootGovernedAssignmentError::Persistence(_))
        ));

        let foreign = temp.path().join("foreign.json");
        let mut other = assignment();
        other.task_tree_id = "other".into();
        other.root_session_id = "other".into();
        other.lineage_path[0] = "other".into();
        std::fs::write(&foreign, serde_json::to_vec(&vec![other]).unwrap()).unwrap();
        let store = RootGovernedAssignmentStore::with_path("root", foreign);
        assert!(matches!(
            store.get("code"),
            Err(RootGovernedAssignmentError::Persistence(_))
        ));
    }
}
