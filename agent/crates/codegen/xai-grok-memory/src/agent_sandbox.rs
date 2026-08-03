//! NG-04D-1/2 / S5 — `AgentSandboxV1` schema + accepted-only memory capability.
//!
//! Pure contract: no prompt rendering, no second runtime. SessionActor issues
//! the sandbox; consumers ask authorization questions. Assurance defaults to
//! [`SandboxAssuranceV1::HarnessPolicyOnly`] — the name "sandbox" never claims
//! OS isolation without a verified adapter path (INV-6 / book §3.4.1).

use serde::{Deserialize, Serialize};

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue};
use crate::task_ledger::AcceptedLedgerSnapshot;
use sha2::{Digest, Sha256};

/// Schema revision for the sandbox DTO (not the canonical encoding revision).
pub const AGENT_SANDBOX_SCHEMA_VERSION: u16 = 1;

/// Product hard ceiling: depth 3 is a leaf (matches tools `HARD_MAX_SUBAGENT_DEPTH`).
pub const SANDBOX_HARD_MAX_DEPTH: u8 = 3;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MemoryCapability {
    ReadAcceptedSnapshot,
    ProposeOwnBranch,
    /// Root-only: resolve/review claims into Accepted.
    RootResolve,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SandboxAssuranceV1 {
    /// Tool filter / policy only — never claim OS isolation.
    HarnessPolicyOnly,
    ToolAndPathEnforced,
    ProcessNetworkRestricted,
    /// Only when every launch path is verified; must not be default-issued.
    OsSandboxVerified,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AgentSandboxState {
    Active,
    Revoked,
    Expired,
    Frozen,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum FilesystemWriteMode {
    None,
    ScopedWrite,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum NetworkMode {
    Denied,
    Restricted,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentSandboxV1 {
    pub schema_version: u16,
    pub sandbox_id: String,
    pub task_tree_id: String,
    pub node_id: String,
    pub immediate_parent_id: Option<String>,
    pub depth: u8,
    pub branch_id: String,
    pub context_manifest_hash: String,
    pub accepted_snapshot_hash: String,
    pub memory_capabilities: Vec<MemoryCapability>,
    pub capability_grant_id: String,
    pub policy_revision: u64,
    pub budget_reservation_id: String,
    pub filesystem_write: FilesystemWriteMode,
    pub network: NetworkMode,
    pub may_spawn: bool,
    pub assurance: SandboxAssuranceV1,
    pub issued_at_unix: u64,
    pub expires_at_unix: u64,
    pub state: AgentSandboxState,
    pub revoke_reason: Option<String>,
    /// Canonical body hash (computed on issue / refresh).
    pub sandbox_hash: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SandboxDenyReason {
    NotActive,
    Expired,
    Revoked,
    Frozen,
    ForeignTree,
    ForeignNode,
    SnapshotMismatch,
    MissingCapability,
    SiblingIsolation,
    CrossBranchPropose,
    LeafSpawnDenied,
    LeafWriteDenied,
    LeafNetworkDenied,
    CallerSuppliedBypass,
    AssuranceOverclaim,
    Invalid(String),
}

impl SandboxDenyReason {
    pub fn code(&self) -> &'static str {
        match self {
            Self::NotActive => "sandbox.not_active",
            Self::Expired => "sandbox.expired",
            Self::Revoked => "sandbox.revoked",
            Self::Frozen => "sandbox.frozen",
            Self::ForeignTree => "sandbox.foreign_tree",
            Self::ForeignNode => "sandbox.foreign_node",
            Self::SnapshotMismatch => "sandbox.snapshot_mismatch",
            Self::MissingCapability => "sandbox.missing_capability",
            Self::SiblingIsolation => "sandbox.sibling_isolation",
            Self::CrossBranchPropose => "sandbox.cross_branch_propose",
            Self::LeafSpawnDenied => "sandbox.leaf_spawn_denied",
            Self::LeafWriteDenied => "sandbox.leaf_write_denied",
            Self::LeafNetworkDenied => "sandbox.leaf_network_denied",
            Self::CallerSuppliedBypass => "sandbox.caller_supplied_bypass",
            Self::AssuranceOverclaim => "sandbox.assurance_overclaim",
            Self::Invalid(_) => "sandbox.invalid",
        }
    }
}

impl std::fmt::Display for SandboxDenyReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            SandboxDenyReason::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

/// Parameters for actor-issued sandboxes. Callers must not supply parent
/// depth/permission/bypass tokens — depth is validated against lineage, and
/// root-only capabilities are granted only when `is_root` is true.
#[derive(Debug, Clone)]
pub struct IssueSandboxRequest {
    pub sandbox_id: String,
    pub task_tree_id: String,
    pub node_id: String,
    pub immediate_parent_id: Option<String>,
    pub depth: u8,
    pub branch_id: String,
    pub context_manifest_hash: String,
    pub accepted_snapshot_hash: String,
    pub capability_grant_id: String,
    pub policy_revision: u64,
    pub budget_reservation_id: String,
    pub is_root: bool,
    /// Explicit request for write scope (ignored for leaf depth).
    pub request_write: bool,
    /// Explicit request for network (ignored for leaf; never default-on for child).
    pub request_network: bool,
    pub request_spawn: bool,
    pub issued_at_unix: u64,
    pub ttl_secs: u64,
    /// Only actor-verified OS path may set OsSandboxVerified; default path
    /// refuses this value.
    pub assurance: SandboxAssuranceV1,
}

impl AgentSandboxV1 {
    pub fn issue(req: IssueSandboxRequest) -> Result<Self, SandboxDenyReason> {
        if req.sandbox_id.trim().is_empty()
            || req.task_tree_id.trim().is_empty()
            || req.node_id.trim().is_empty()
            || req.branch_id.trim().is_empty()
            || req.accepted_snapshot_hash.trim().is_empty()
            || req.context_manifest_hash.trim().is_empty()
            || req.capability_grant_id.trim().is_empty()
            || req.budget_reservation_id.trim().is_empty()
        {
            return Err(SandboxDenyReason::Invalid(
                "required identity field empty".into(),
            ));
        }
        if req.depth > SANDBOX_HARD_MAX_DEPTH {
            return Err(SandboxDenyReason::Invalid(format!(
                "depth {} exceeds hard max {SANDBOX_HARD_MAX_DEPTH}",
                req.depth
            )));
        }
        if matches!(req.assurance, SandboxAssuranceV1::OsSandboxVerified) {
            // Schema gate: refuse silent OS claims. A later enforcement gate
            // may re-issue after adapter proof; issue() never auto-promotes.
            return Err(SandboxDenyReason::AssuranceOverclaim);
        }
        if !req.is_root && req.immediate_parent_id.is_none() && req.depth > 0 {
            return Err(SandboxDenyReason::Invalid(
                "non-root child must name immediate_parent_id".into(),
            ));
        }

        let is_leaf = req.depth >= SANDBOX_HARD_MAX_DEPTH;
        let mut memory_capabilities = vec![
            MemoryCapability::ReadAcceptedSnapshot,
            MemoryCapability::ProposeOwnBranch,
        ];
        if req.is_root {
            memory_capabilities.push(MemoryCapability::RootResolve);
        }

        let may_spawn = req.request_spawn && !is_leaf && req.depth < SANDBOX_HARD_MAX_DEPTH;
        let filesystem_write = if is_leaf || !req.request_write {
            FilesystemWriteMode::None
        } else {
            FilesystemWriteMode::ScopedWrite
        };
        let network = if is_leaf || !req.request_network {
            NetworkMode::Denied
        } else {
            NetworkMode::Restricted
        };

        let mut sandbox = Self {
            schema_version: AGENT_SANDBOX_SCHEMA_VERSION,
            sandbox_id: req.sandbox_id,
            task_tree_id: req.task_tree_id,
            node_id: req.node_id,
            immediate_parent_id: req.immediate_parent_id,
            depth: req.depth,
            branch_id: req.branch_id,
            context_manifest_hash: req.context_manifest_hash,
            accepted_snapshot_hash: req.accepted_snapshot_hash,
            memory_capabilities,
            capability_grant_id: req.capability_grant_id,
            policy_revision: req.policy_revision,
            budget_reservation_id: req.budget_reservation_id,
            filesystem_write,
            network,
            may_spawn,
            assurance: req.assurance,
            issued_at_unix: req.issued_at_unix,
            expires_at_unix: req.issued_at_unix.saturating_add(req.ttl_secs.max(1)),
            state: AgentSandboxState::Active,
            revoke_reason: None,
            sandbox_hash: String::new(),
        };
        sandbox.sandbox_hash = sandbox
            .compute_sandbox_hash()
            .map_err(|e| SandboxDenyReason::Invalid(e.to_string()))?;
        Ok(sandbox)
    }

    pub fn compute_sandbox_hash(&self) -> Result<String, CanonicalError> {
        let caps: Vec<CanonicalValue> = self
            .memory_capabilities
            .iter()
            .map(|c| {
                CanonicalValue::str(match c {
                    MemoryCapability::ReadAcceptedSnapshot => "read_accepted_snapshot",
                    MemoryCapability::ProposeOwnBranch => "propose_own_branch",
                    MemoryCapability::RootResolve => "root_resolve",
                })
            })
            .collect();
        let record = CanonicalRecord::new("agent-sandbox")
            .field("schema_version", CanonicalValue::U64(u64::from(self.schema_version)))
            .field("sandbox_id", CanonicalValue::str(&self.sandbox_id))
            .field("task_tree_id", CanonicalValue::str(&self.task_tree_id))
            .field("node_id", CanonicalValue::str(&self.node_id))
            .field(
                "immediate_parent_id",
                self.immediate_parent_id
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field("depth", CanonicalValue::U64(u64::from(self.depth)))
            .field("branch_id", CanonicalValue::str(&self.branch_id))
            .field(
                "context_manifest_hash",
                CanonicalValue::str(&self.context_manifest_hash),
            )
            .field(
                "accepted_snapshot_hash",
                CanonicalValue::str(&self.accepted_snapshot_hash),
            )
            .field("memory_capabilities", CanonicalValue::Seq(caps))
            .field(
                "capability_grant_id",
                CanonicalValue::str(&self.capability_grant_id),
            )
            .field("policy_revision", CanonicalValue::U64(self.policy_revision))
            .field(
                "budget_reservation_id",
                CanonicalValue::str(&self.budget_reservation_id),
            )
            .field(
                "filesystem_write",
                CanonicalValue::str(match self.filesystem_write {
                    FilesystemWriteMode::None => "none",
                    FilesystemWriteMode::ScopedWrite => "scoped_write",
                }),
            )
            .field(
                "network",
                CanonicalValue::str(match self.network {
                    NetworkMode::Denied => "denied",
                    NetworkMode::Restricted => "restricted",
                }),
            )
            .field("may_spawn", CanonicalValue::Bool(self.may_spawn))
            .field(
                "assurance",
                CanonicalValue::str(match self.assurance {
                    SandboxAssuranceV1::HarnessPolicyOnly => "harness_policy_only",
                    SandboxAssuranceV1::ToolAndPathEnforced => "tool_and_path_enforced",
                    SandboxAssuranceV1::ProcessNetworkRestricted => "process_network_restricted",
                    SandboxAssuranceV1::OsSandboxVerified => "os_sandbox_verified",
                }),
            )
            .field("issued_at_unix", CanonicalValue::U64(self.issued_at_unix))
            .field("expires_at_unix", CanonicalValue::U64(self.expires_at_unix))
            .field(
                "state",
                CanonicalValue::str(match self.state {
                    AgentSandboxState::Active => "active",
                    AgentSandboxState::Revoked => "revoked",
                    AgentSandboxState::Expired => "expired",
                    AgentSandboxState::Frozen => "frozen",
                }),
            );
        let digest = Sha256::digest(record.canonical_bytes()?);
        Ok(format!("sha256:{digest:x}"))
    }

    fn ensure_live(&self, now_unix: u64) -> Result<(), SandboxDenyReason> {
        match self.state {
            AgentSandboxState::Revoked => return Err(SandboxDenyReason::Revoked),
            AgentSandboxState::Frozen => return Err(SandboxDenyReason::Frozen),
            AgentSandboxState::Expired => return Err(SandboxDenyReason::Expired),
            AgentSandboxState::Active => {}
        }
        if now_unix > self.expires_at_unix {
            return Err(SandboxDenyReason::Expired);
        }
        Ok(())
    }

    fn has_cap(&self, cap: MemoryCapability) -> bool {
        self.memory_capabilities.contains(&cap)
    }

    /// Read the shared accepted snapshot only — never sibling scratch.
    pub fn authorize_read_accepted_snapshot(
        &self,
        snapshot: &AcceptedLedgerSnapshot,
        now_unix: u64,
    ) -> Result<(), SandboxDenyReason> {
        self.ensure_live(now_unix)?;
        if !self.has_cap(MemoryCapability::ReadAcceptedSnapshot) {
            return Err(SandboxDenyReason::MissingCapability);
        }
        if snapshot.task_tree_id != self.task_tree_id {
            return Err(SandboxDenyReason::ForeignTree);
        }
        if snapshot.accepted_set_hash != self.accepted_snapshot_hash {
            return Err(SandboxDenyReason::SnapshotMismatch);
        }
        Ok(())
    }

    /// Propose only on this node's branch. Foreign branch_id → deny.
    pub fn authorize_propose_own_branch(
        &self,
        branch_id: &str,
        now_unix: u64,
    ) -> Result<(), SandboxDenyReason> {
        self.ensure_live(now_unix)?;
        if !self.has_cap(MemoryCapability::ProposeOwnBranch) {
            return Err(SandboxDenyReason::MissingCapability);
        }
        if branch_id != self.branch_id {
            return Err(SandboxDenyReason::CrossBranchPropose);
        }
        Ok(())
    }

    /// Sibling / private scratch is never readable through the sandbox.
    pub fn authorize_read_sibling_scratch(
        &self,
        _sibling_node_id: &str,
        _now_unix: u64,
    ) -> Result<(), SandboxDenyReason> {
        Err(SandboxDenyReason::SiblingIsolation)
    }

    pub fn authorize_spawn(&self, now_unix: u64) -> Result<(), SandboxDenyReason> {
        self.ensure_live(now_unix)?;
        if self.depth >= SANDBOX_HARD_MAX_DEPTH || !self.may_spawn {
            return Err(SandboxDenyReason::LeafSpawnDenied);
        }
        Ok(())
    }

    pub fn authorize_filesystem_write(&self, now_unix: u64) -> Result<(), SandboxDenyReason> {
        self.ensure_live(now_unix)?;
        if !matches!(self.filesystem_write, FilesystemWriteMode::ScopedWrite) {
            return Err(SandboxDenyReason::LeafWriteDenied);
        }
        Ok(())
    }

    pub fn authorize_network(&self, now_unix: u64) -> Result<(), SandboxDenyReason> {
        self.ensure_live(now_unix)?;
        if matches!(self.network, NetworkMode::Denied) {
            return Err(SandboxDenyReason::LeafNetworkDenied);
        }
        Ok(())
    }

    /// Refuse any caller-supplied "bypass" flag — sandbox is actor-issued only.
    pub fn authorize_bypass_token(&self, present: bool) -> Result<(), SandboxDenyReason> {
        if present {
            return Err(SandboxDenyReason::CallerSuppliedBypass);
        }
        Ok(())
    }

    pub fn revoke(&mut self, reason: impl Into<String>) {
        self.state = AgentSandboxState::Revoked;
        self.revoke_reason = Some(reason.into());
        if let Ok(hash) = self.compute_sandbox_hash() {
            self.sandbox_hash = hash;
        }
    }

    pub fn freeze(&mut self, reason: impl Into<String>) {
        self.state = AgentSandboxState::Frozen;
        self.revoke_reason = Some(reason.into());
        if let Ok(hash) = self.compute_sandbox_hash() {
            self.sandbox_hash = hash;
        }
    }

    /// Rebase onto a new accepted snapshot at a safe checkpoint. Generates a
    /// new hash; does not in-place replace without recompute.
    pub fn rebase_accepted_snapshot(
        &mut self,
        new_snapshot_hash: impl Into<String>,
        now_unix: u64,
    ) -> Result<(), SandboxDenyReason> {
        self.ensure_live(now_unix)?;
        self.accepted_snapshot_hash = new_snapshot_hash.into();
        self.sandbox_hash = self
            .compute_sandbox_hash()
            .map_err(|e| SandboxDenyReason::Invalid(e.to_string()))?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn snap(tree: &str, hash: &str) -> AcceptedLedgerSnapshot {
        AcceptedLedgerSnapshot {
            task_tree_id: tree.into(),
            record_count: 1,
            accepted_count: 1,
            accepted_set_hash: hash.into(),
            journal_hash: "sha256:journal".into(),
        }
    }

    fn child_req(node: &str, branch: &str, snap_hash: &str, depth: u8) -> IssueSandboxRequest {
        IssueSandboxRequest {
            sandbox_id: format!("sb-{node}"),
            task_tree_id: "tree".into(),
            node_id: node.into(),
            immediate_parent_id: Some("root".into()),
            depth,
            branch_id: branch.into(),
            context_manifest_hash: "sha256:manifest".into(),
            accepted_snapshot_hash: snap_hash.into(),
            capability_grant_id: "grant-1".into(),
            policy_revision: 1,
            budget_reservation_id: "budget-1".into(),
            is_root: false,
            request_write: true,
            request_network: true,
            request_spawn: true,
            issued_at_unix: 1_700_000_000,
            ttl_secs: 3600,
            assurance: SandboxAssuranceV1::HarnessPolicyOnly,
        }
    }

    #[test]
    fn two_siblings_share_snapshot_but_not_branch_or_scratch() {
        let snap_hash = "sha256:accepted-set";
        let a = AgentSandboxV1::issue(child_req("child-a", "branch-a", snap_hash, 1)).unwrap();
        let b = AgentSandboxV1::issue(child_req("child-b", "branch-b", snap_hash, 1)).unwrap();
        let snapshot = snap("tree", snap_hash);
        let now = 1_700_000_100;
        assert!(a.authorize_read_accepted_snapshot(&snapshot, now).is_ok());
        assert!(b.authorize_read_accepted_snapshot(&snapshot, now).is_ok());
        assert_eq!(a.accepted_snapshot_hash, b.accepted_snapshot_hash);
        assert_ne!(a.branch_id, b.branch_id);
        assert_eq!(
            a.authorize_propose_own_branch("branch-b", now).unwrap_err(),
            SandboxDenyReason::CrossBranchPropose
        );
        assert!(a.authorize_propose_own_branch("branch-a", now).is_ok());
        assert_eq!(
            a.authorize_read_sibling_scratch("child-b", now)
                .unwrap_err(),
            SandboxDenyReason::SiblingIsolation
        );
    }

    #[test]
    fn foreign_stale_snapshot_and_expiry_fail_closed() {
        let sb = AgentSandboxV1::issue(child_req("c", "br", "sha256:v1", 1)).unwrap();
        let now = 1_700_000_100;
        assert_eq!(
            sb.authorize_read_accepted_snapshot(&snap("other-tree", "sha256:v1"), now)
                .unwrap_err(),
            SandboxDenyReason::ForeignTree
        );
        assert_eq!(
            sb.authorize_read_accepted_snapshot(&snap("tree", "sha256:stale"), now)
                .unwrap_err(),
            SandboxDenyReason::SnapshotMismatch
        );
        assert_eq!(
            sb.authorize_read_accepted_snapshot(&snap("tree", "sha256:v1"), 9_999_999_999)
                .unwrap_err(),
            SandboxDenyReason::Expired
        );
        let mut revoked = sb.clone();
        revoked.revoke("parent cancelled");
        assert_eq!(
            revoked
                .authorize_propose_own_branch("br", now)
                .unwrap_err(),
            SandboxDenyReason::Revoked
        );
    }

    #[test]
    fn leaf_depth_cannot_spawn_write_or_network() {
        let leaf = AgentSandboxV1::issue(child_req("leaf", "br-leaf", "sha256:v1", 3)).unwrap();
        let now = 1_700_000_100;
        assert!(!leaf.may_spawn);
        assert_eq!(leaf.filesystem_write, FilesystemWriteMode::None);
        assert_eq!(leaf.network, NetworkMode::Denied);
        assert_eq!(
            leaf.authorize_spawn(now).unwrap_err(),
            SandboxDenyReason::LeafSpawnDenied
        );
        assert_eq!(
            leaf.authorize_filesystem_write(now).unwrap_err(),
            SandboxDenyReason::LeafWriteDenied
        );
        assert_eq!(
            leaf.authorize_network(now).unwrap_err(),
            SandboxDenyReason::LeafNetworkDenied
        );
    }

    #[test]
    fn os_assurance_cannot_be_self_issued_and_bypass_is_rejected() {
        let mut req = child_req("c", "br", "sha256:v1", 1);
        req.assurance = SandboxAssuranceV1::OsSandboxVerified;
        assert_eq!(
            AgentSandboxV1::issue(req).unwrap_err(),
            SandboxDenyReason::AssuranceOverclaim
        );
        let sb = AgentSandboxV1::issue(child_req("c", "br", "sha256:v1", 1)).unwrap();
        assert_eq!(
            sb.authorize_bypass_token(true).unwrap_err(),
            SandboxDenyReason::CallerSuppliedBypass
        );
        assert!(sb.authorize_bypass_token(false).is_ok());
        assert_eq!(sb.assurance, SandboxAssuranceV1::HarnessPolicyOnly);
    }

    #[test]
    fn rebase_updates_snapshot_hash_and_sandbox_hash() {
        let mut sb = AgentSandboxV1::issue(child_req("c", "br", "sha256:v1", 1)).unwrap();
        let before = sb.sandbox_hash.clone();
        let now = 1_700_000_100;
        sb.rebase_accepted_snapshot("sha256:v2", now).unwrap();
        assert_eq!(sb.accepted_snapshot_hash, "sha256:v2");
        assert_ne!(sb.sandbox_hash, before);
        assert!(sb
            .authorize_read_accepted_snapshot(&snap("tree", "sha256:v2"), now)
            .is_ok());
    }

    #[test]
    fn sandbox_hash_is_stable_for_same_body() {
        let a = AgentSandboxV1::issue(child_req("c", "br", "sha256:v1", 1)).unwrap();
        let b = AgentSandboxV1::issue(child_req("c", "br", "sha256:v1", 1)).unwrap();
        assert_eq!(a.sandbox_hash, b.sandbox_hash);
        assert!(a.sandbox_hash.starts_with("sha256:"));
    }

    #[test]
    fn root_receives_resolve_capability_child_does_not() {
        let mut root_req = child_req("root", "branch-root", "sha256:v1", 0);
        root_req.is_root = true;
        root_req.immediate_parent_id = None;
        root_req.node_id = "root".into();
        root_req.sandbox_id = "sb-root".into();
        let root = AgentSandboxV1::issue(root_req).unwrap();
        assert!(root.has_cap(MemoryCapability::RootResolve));
        let child = AgentSandboxV1::issue(child_req("child", "br", "sha256:v1", 1)).unwrap();
        assert!(!child.has_cap(MemoryCapability::RootResolve));
    }

    /// Drive the real ledger → accepted_snapshot → sandbox authorization path
    /// (SANDBOX_MEMORY_GATE): children only see accepted set after root review.
    #[test]
    fn accepted_only_path_uses_real_ledger_snapshot() {
        use crate::task_ledger::{WorkingMemoryFact, WorkingMemoryLedger, WorkingMemoryState};

        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        let mk = |fact_id: &str, rev: u64, branch: &str, author: &str, text: &str| WorkingMemoryFact {
            task_tree_id: "root".into(),
            branch_id: branch.into(),
            fact_id: fact_id.into(),
            revision: rev,
            kind: Default::default(),
            author_session_id: author.into(),
            evidence_ref: Some("test://evidence".into()),
            confidence: 80,
            state: WorkingMemoryState::Proposed,
            text: text.into(),
            derived_from: None,
        };
        ledger
            .propose(mk("fact-a", 1, "branch-a", "child-a", "unreviewed"))
            .unwrap();
        let empty = ledger.accepted_snapshot().unwrap();
        assert_eq!(empty.accepted_count, 0);

        ledger
            .review(
                "root",
                mk("fact-a", 2, "branch-a", "root", "accepted truth"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        let accepted = ledger.accepted_snapshot().unwrap();
        assert_eq!(accepted.accepted_count, 1);
        assert_eq!(accepted.task_tree_id, "root");

        let mut req_a = child_req("child-a", "branch-a", &accepted.accepted_set_hash, 1);
        req_a.task_tree_id = "root".into();
        let mut req_b = child_req("child-b", "branch-b", &accepted.accepted_set_hash, 1);
        req_b.task_tree_id = "root".into();
        let a = AgentSandboxV1::issue(req_a).unwrap();
        let b = AgentSandboxV1::issue(req_b).unwrap();
        let now = 1_700_000_100;
        assert!(a.authorize_read_accepted_snapshot(&accepted, now).is_ok());
        assert!(b.authorize_read_accepted_snapshot(&accepted, now).is_ok());
        assert_eq!(
            a.authorize_read_accepted_snapshot(&empty, now).unwrap_err(),
            SandboxDenyReason::SnapshotMismatch
        );
        assert_eq!(
            a.authorize_read_sibling_scratch("child-b", now)
                .unwrap_err(),
            SandboxDenyReason::SiblingIsolation
        );

        ledger
            .propose(mk("fact-b", 1, "branch-b", "child-b", "second proposal"))
            .unwrap();
        ledger
            .review(
                "root",
                mk("fact-b", 2, "branch-b", "root", "second accepted"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        let rebased_snap = ledger.accepted_snapshot().unwrap();
        assert!(rebased_snap.accepted_count >= 2);
        let mut a = a;
        a.rebase_accepted_snapshot(&rebased_snap.accepted_set_hash, now)
            .unwrap();
        assert!(a
            .authorize_read_accepted_snapshot(&rebased_snap, now)
            .is_ok());
        assert_eq!(
            b.authorize_read_accepted_snapshot(&rebased_snap, now)
                .unwrap_err(),
            SandboxDenyReason::SnapshotMismatch
        );
    }
}
