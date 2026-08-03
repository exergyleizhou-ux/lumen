//! Root-signed, path-scoped write grant for nested agents (NG-03D / S3).
//!
//! This is not a second runtime: SessionActor / coordinator remains authority.
//! Child tools may only request paths that fall under a live, non-expired,
//! non-revoked grant that was issued for their node.
//!
//! Production path: host injects [`WriteScopeLeaseResource`] into tool
//! resources; `search_replace` (and other writers) call
//! [`WriteScopeLease::authorize_write`] before mutating files. Spawn-time
//! exclusivity uses [`write_scopes_overlap`] so two live children cannot share
//! a path prefix. Root handoff produces [`MergeReceiptV1`] — never auto-merge.

use std::path::{Component, Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};

use crate::register_resource;

/// Machine-readable denial for write-scope checks.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WriteScopeDenyReason {
    Expired,
    Revoked,
    ForeignNode,
    ForeignTree,
    PathOutsideGrant,
    EmptyGrant,
    AbsoluteEscape,
}

impl WriteScopeDenyReason {
    pub const fn code(self) -> &'static str {
        match self {
            Self::Expired => "write_scope.expired",
            Self::Revoked => "write_scope.revoked",
            Self::ForeignNode => "write_scope.foreign_node",
            Self::ForeignTree => "write_scope.foreign_tree",
            Self::PathOutsideGrant => "write_scope.path_outside_grant",
            Self::EmptyGrant => "write_scope.empty_grant",
            Self::AbsoluteEscape => "write_scope.absolute_escape",
        }
    }
}

/// Root-issued, path-scoped write lease for one task-tree node.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WriteScopeLease {
    pub grant_id: String,
    pub root_tree_id: String,
    pub owner_node_id: String,
    /// Absolute or workspace-relative roots the node may write under.
    pub allowed_roots: Vec<PathBuf>,
    pub deadline_unix: u64,
    pub revoked: bool,
}

impl WriteScopeLease {
    pub fn issue(
        grant_id: impl Into<String>,
        root_tree_id: impl Into<String>,
        owner_node_id: impl Into<String>,
        allowed_roots: Vec<PathBuf>,
        ttl_secs: u64,
    ) -> Result<Self, WriteScopeDenyReason> {
        if allowed_roots.is_empty() {
            return Err(WriteScopeDenyReason::EmptyGrant);
        }
        let now = now_unix();
        Ok(Self {
            grant_id: grant_id.into(),
            root_tree_id: root_tree_id.into(),
            owner_node_id: owner_node_id.into(),
            allowed_roots,
            deadline_unix: now.saturating_add(ttl_secs.max(1)),
            revoked: false,
        })
    }

    /// Create a live lease that authorizes no paths.
    ///
    /// This is intentionally distinct from a missing resource: absence keeps
    /// the legacy/root behavior, whereas a governed child without an explicit
    /// root-approved scope must fail closed for every writer.
    pub fn deny_all(
        grant_id: impl Into<String>,
        root_tree_id: impl Into<String>,
        owner_node_id: impl Into<String>,
        ttl_secs: u64,
    ) -> Self {
        let now = now_unix();
        Self {
            grant_id: grant_id.into(),
            root_tree_id: root_tree_id.into(),
            owner_node_id: owner_node_id.into(),
            allowed_roots: Vec::new(),
            deadline_unix: now.saturating_add(ttl_secs.max(1)),
            revoked: false,
        }
    }

    pub fn revoke(&mut self) {
        self.revoked = true;
    }

    /// Authorize a write path for `node_id` under this lease.
    pub fn authorize_write(
        &self,
        root_tree_id: &str,
        node_id: &str,
        path: &Path,
        now_unix_secs: u64,
    ) -> Result<(), WriteScopeDenyReason> {
        if self.revoked {
            return Err(WriteScopeDenyReason::Revoked);
        }
        if now_unix_secs > self.deadline_unix {
            return Err(WriteScopeDenyReason::Expired);
        }
        if self.root_tree_id != root_tree_id {
            return Err(WriteScopeDenyReason::ForeignTree);
        }
        if self.owner_node_id != node_id {
            return Err(WriteScopeDenyReason::ForeignNode);
        }
        if path_escapes(path) {
            return Err(WriteScopeDenyReason::AbsoluteEscape);
        }
        let candidate = normalize_rel(path);
        for root in &self.allowed_roots {
            let base = normalize_rel(root);
            if path_is_under(&candidate, &base) {
                return Ok(());
            }
        }
        Err(WriteScopeDenyReason::PathOutsideGrant)
    }
}

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

fn path_escapes(path: &Path) -> bool {
    path.components().any(|c| matches!(c, Component::ParentDir))
}

fn normalize_rel(path: &Path) -> PathBuf {
    let mut out = PathBuf::new();
    for c in path.components() {
        match c {
            Component::CurDir => {}
            Component::ParentDir => {
                let _ = out.pop();
            }
            other => out.push(other.as_os_str()),
        }
    }
    out
}

fn path_is_under(path: &Path, root: &Path) -> bool {
    if root.as_os_str().is_empty() {
        return false;
    }
    path == root || path.starts_with(root)
}

/// Normalize a host-issued write root for overlap comparison.
///
/// Rejects empty components and parent-dir escapes. Absolute roots are kept
/// absolute (after normalize); relative roots stay relative. Callers that
/// need symlink policy must use [`enforce_write_scope_if_present`] at write
/// time — spawn-time overlap deliberately uses lexical containment so a
/// mutable FS cannot flip admission after the host signed the receipt.
pub fn normalize_write_scope_root(root: &Path) -> Result<PathBuf, WriteScopeDenyReason> {
    if root.as_os_str().is_empty() {
        return Err(WriteScopeDenyReason::EmptyGrant);
    }
    if path_escapes(root) {
        return Err(WriteScopeDenyReason::AbsoluteEscape);
    }
    let normalized = normalize_rel(root);
    if normalized.as_os_str().is_empty() {
        return Err(WriteScopeDenyReason::EmptyGrant);
    }
    Ok(normalized)
}

/// True when two non-empty write-scope root lists share a path (prefix or
/// equal). Empty lists are the legacy "no narrowing" form and never conflict
/// via this detector — exclusivity only applies when both sides are explicit.
///
/// Examples that overlap: `src` vs `src/lib`, `tests` vs `tests`.
/// Examples that do not: `src` vs `tests`, empty vs anything.
pub fn write_scopes_overlap(left: &[PathBuf], right: &[PathBuf]) -> bool {
    if left.is_empty() || right.is_empty() {
        return false;
    }
    left.iter().any(|left_root| {
        let Ok(left_norm) = normalize_write_scope_root(left_root) else {
            // Un-normalizable roots fail closed as "conflicting" so a bad
            // receipt cannot slip past the spawn gate.
            return true;
        };
        right.iter().any(|right_root| {
            let Ok(right_norm) = normalize_write_scope_root(right_root) else {
                return true;
            };
            path_is_under(&left_norm, &right_norm) || path_is_under(&right_norm, &left_norm)
        })
    })
}

/// Result of a root-owned worktree/scope handoff (never auto-applied by child).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MergeApplyResult {
    Applied,
    Conflict,
    Rejected,
    Cancelled,
}

/// Root-only merge/handoff receipt. Children may produce patch candidates;
/// only root (non-empty `root_decision_ref`) may accept an Applied outcome.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct MergeReceiptV1 {
    pub write_lease_id: String,
    pub task_tree_id: String,
    pub node_id: String,
    pub observed_base_commit: String,
    pub expected_base_commit: String,
    pub changed_path_hashes: Vec<String>,
    pub apply_result: MergeApplyResult,
    pub verification_refs: Vec<String>,
    pub root_decision_ref: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MergeHandoffDenyReason {
    MissingRootDecision,
    StaleBase,
    EmptyChangeSet,
    LeaseNotActive,
    ForeignTree,
}

impl MergeHandoffDenyReason {
    pub const fn code(self) -> &'static str {
        match self {
            Self::MissingRootDecision => "merge.missing_root_decision",
            Self::StaleBase => "merge.stale_base",
            Self::EmptyChangeSet => "merge.empty_change_set",
            Self::LeaseNotActive => "merge.lease_not_active",
            Self::ForeignTree => "merge.foreign_tree",
        }
    }
}

/// Build a handoff receipt from observed worktree state. Fail-closed: stale
/// base, empty patch, revoked/expired lease, or missing root decision cannot
/// become Applied. Conflict lists yield Conflict even with a root decision
/// (root still must re-approve after resolve).
pub fn evaluate_merge_handoff(
    lease: &WriteScopeLease,
    task_tree_id: &str,
    observed_base_commit: impl Into<String>,
    expected_base_commit: impl Into<String>,
    changed_path_hashes: Vec<String>,
    conflict_paths: &[String],
    verification_refs: Vec<String>,
    root_decision_ref: impl Into<String>,
    now_unix_secs: u64,
) -> Result<MergeReceiptV1, MergeHandoffDenyReason> {
    if lease.revoked || now_unix_secs > lease.deadline_unix {
        return Err(MergeHandoffDenyReason::LeaseNotActive);
    }
    if lease.root_tree_id != task_tree_id {
        return Err(MergeHandoffDenyReason::ForeignTree);
    }
    let observed = observed_base_commit.into();
    let expected = expected_base_commit.into();
    let root_decision = root_decision_ref.into();
    if root_decision.trim().is_empty() {
        return Err(MergeHandoffDenyReason::MissingRootDecision);
    }
    if observed != expected {
        return Err(MergeHandoffDenyReason::StaleBase);
    }
    if changed_path_hashes.is_empty() && conflict_paths.is_empty() {
        return Err(MergeHandoffDenyReason::EmptyChangeSet);
    }
    let apply_result = if !conflict_paths.is_empty() {
        MergeApplyResult::Conflict
    } else {
        MergeApplyResult::Applied
    };
    Ok(MergeReceiptV1 {
        write_lease_id: lease.grant_id.clone(),
        task_tree_id: task_tree_id.to_owned(),
        node_id: lease.owner_node_id.clone(),
        observed_base_commit: observed,
        expected_base_commit: expected,
        changed_path_hashes,
        apply_result,
        verification_refs,
        root_decision_ref: root_decision,
    })
}

/// Host-injected write grant for the current session/node.
/// When absent, writers behave as unconstrained root (no extra path gate).
/// When present, every write must pass [`WriteScopeLease::authorize_write`].
#[derive(Debug, Clone)]
pub struct WriteScopeLeaseResource {
    pub lease: WriteScopeLease,
}

impl WriteScopeLeaseResource {
    pub fn authorize(&self, path: &Path, now_unix_secs: u64) -> Result<(), WriteScopeDenyReason> {
        self.lease.authorize_write(
            &self.lease.root_tree_id,
            &self.lease.owner_node_id,
            path,
            now_unix_secs,
        )
    }
}

register_resource!(
    "grok_build",
    "WriteScopeLeaseResource",
    WriteScopeLeaseResource
);

/// Check an optional write-scope resource against a candidate path.
///
/// Used by production writers (`search_replace`, …). Missing resource means
/// no additional grant gate (root SessionActor path). The stripped
/// workspace-relative candidate is tried first (relative grant roots), then
/// the full path (absolute grant roots such as a child worktree), so both
/// grant styles work; any live root match admits the write.
pub fn enforce_write_scope_if_present(
    resources: &crate::types::resources::Resources,
    path: &Path,
    cwd: &Path,
) -> Result<(), String> {
    let Some(scope) = resources.get::<WriteScopeLeaseResource>() else {
        return Ok(());
    };
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    // A lexical `src/file.rs` check is insufficient: `src` may be a symlink
    // to a location outside this child's worktree, and a new leaf may not yet
    // exist for direct canonicalization. Resolve the deepest existing parent
    // and reconstruct the non-existent suffix. If this cannot be done, deny
    // rather than treating an unverified spelling as an authorized path.
    let canonical_cwd = canonicalize_existing_ancestor(cwd).map_err(|error| {
        format!("write_scope.unresolvable_path: cannot resolve workspace root: {error}")
    })?;
    let absolute_path = if path.is_absolute() {
        path.to_path_buf()
    } else {
        cwd.join(path)
    };
    let canonical_path = canonicalize_existing_ancestor(&absolute_path).map_err(|error| {
        format!("write_scope.unresolvable_path: cannot resolve write target: {error}")
    })?;
    if let Ok(relative_path) = canonical_path.strip_prefix(&canonical_cwd)
        && scope.authorize(relative_path, now).is_ok()
    {
        return Ok(());
    }

    // Hosts may grant an isolated child worktree as an absolute root. Compare
    // canonical paths so an absolute grant cannot be bypassed through a
    // symlink either. Calling `authorize` on the root itself preserves every
    // identity/expiry/revocation check before the path comparison succeeds.
    for root in &scope.lease.allowed_roots {
        if !root.is_absolute() {
            continue;
        }
        let Ok(canonical_root) = canonicalize_existing_ancestor(root) else {
            continue;
        };
        if path_is_under(&canonical_path, &canonical_root) && scope.authorize(root, now).is_ok() {
            return Ok(());
        }
    }

    Err("write_scope.path_outside_grant: write path not authorized for this node".to_owned())
}

/// Canonicalize a path even if the final file has not been created yet.
///
/// `std::fs::canonicalize` only accepts existing paths.  For a prospective
/// write, walk up to the closest existing ancestor, canonicalize that (which
/// resolves every symlink), then append the missing suffix unchanged.
fn canonicalize_existing_ancestor(path: &Path) -> std::io::Result<PathBuf> {
    let mut cursor = path;
    let mut suffix = Vec::new();
    loop {
        match std::fs::canonicalize(cursor) {
            Ok(mut resolved) => {
                for component in suffix.iter().rev() {
                    resolved.push(component);
                }
                return Ok(resolved);
            }
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                let Some(name) = cursor.file_name() else {
                    return Err(error);
                };
                suffix.push(name.to_os_string());
                let Some(parent) = cursor.parent() else {
                    return Err(error);
                };
                cursor = parent;
            }
            Err(error) => return Err(error),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn authorizes_paths_under_grant_and_denies_outside_foreign_expired() {
        let mut lease = WriteScopeLease::issue(
            "g1",
            "root",
            "code",
            vec![PathBuf::from("src"), PathBuf::from("tests")],
            60,
        )
        .unwrap();
        let now = now_unix();
        assert!(
            lease
                .authorize_write("root", "code", Path::new("src/lib.rs"), now)
                .is_ok()
        );
        assert_eq!(
            lease
                .authorize_write("root", "code", Path::new("outside/x"), now)
                .unwrap_err(),
            WriteScopeDenyReason::PathOutsideGrant
        );
        assert_eq!(
            lease
                .authorize_write("root", "other", Path::new("src/a"), now)
                .unwrap_err(),
            WriteScopeDenyReason::ForeignNode
        );
        assert_eq!(
            lease
                .authorize_write("other-tree", "code", Path::new("src/a"), now)
                .unwrap_err(),
            WriteScopeDenyReason::ForeignTree
        );
        assert_eq!(
            lease
                .authorize_write("root", "code", Path::new("../escape"), now)
                .unwrap_err(),
            WriteScopeDenyReason::AbsoluteEscape
        );
        lease.revoke();
        assert_eq!(
            lease
                .authorize_write("root", "code", Path::new("src/a"), now)
                .unwrap_err(),
            WriteScopeDenyReason::Revoked
        );
        let expired = WriteScopeLease {
            grant_id: "g2".into(),
            root_tree_id: "root".into(),
            owner_node_id: "code".into(),
            allowed_roots: vec![PathBuf::from("src")],
            deadline_unix: now.saturating_sub(1),
            revoked: false,
        };
        assert_eq!(
            expired
                .authorize_write("root", "code", Path::new("src/a"), now)
                .unwrap_err(),
            WriteScopeDenyReason::Expired
        );
    }

    #[test]
    fn empty_grant_cannot_be_issued() {
        assert_eq!(
            WriteScopeLease::issue("g", "root", "n", vec![], 10).unwrap_err(),
            WriteScopeDenyReason::EmptyGrant
        );
    }

    #[test]
    fn deny_all_lease_fails_closed_without_becoming_a_missing_resource() {
        let lease = WriteScopeLease::deny_all("deny", "root", "child", 60);
        assert_eq!(
            lease
                .authorize_write("root", "child", Path::new("src/lib.rs"), now_unix())
                .unwrap_err(),
            WriteScopeDenyReason::PathOutsideGrant
        );
    }

    #[test]
    fn enforce_write_scope_if_present_gates_when_resource_injected() {
        use crate::types::resources::Resources;
        let mut resources = Resources::new();
        let temp = tempfile::tempdir().unwrap();
        let workspace = temp.path().join("ws");
        std::fs::create_dir_all(workspace.join("src")).unwrap();
        // No resource: root path unconstrained.
        assert!(
            enforce_write_scope_if_present(&resources, &workspace.join("src/a.rs"), &workspace,)
                .is_ok()
        );
        let lease =
            WriteScopeLease::issue("g1", "root", "code", vec![PathBuf::from("src")], 3600).unwrap();
        resources.insert(WriteScopeLeaseResource { lease });
        assert!(
            enforce_write_scope_if_present(&resources, &workspace.join("src/lib.rs"), &workspace,)
                .is_ok()
        );
        let err =
            enforce_write_scope_if_present(&resources, &workspace.join("outside/x.rs"), &workspace)
                .unwrap_err();
        assert!(err.contains("write_scope"), "{err}");
    }

    #[test]
    fn scope_resolves_nonexistent_target_under_symlinked_parent_before_authorizing() {
        use crate::types::resources::Resources;
        let temp = tempfile::tempdir().unwrap();
        let workspace = temp.path().join("workspace");
        let outside = temp.path().join("outside");
        std::fs::create_dir_all(&workspace).unwrap();
        std::fs::create_dir_all(&outside).unwrap();

        #[cfg(unix)]
        std::os::unix::fs::symlink(&outside, workspace.join("src")).unwrap();
        #[cfg(not(unix))]
        return;

        let lease =
            WriteScopeLease::issue("g1", "root", "code", vec![PathBuf::from("src")], 3600).unwrap();
        let mut resources = Resources::new();
        resources.insert(WriteScopeLeaseResource { lease });
        let escaped = workspace.join("src").join("new.rs");
        let error = enforce_write_scope_if_present(&resources, &escaped, &workspace).unwrap_err();
        assert!(error.contains("path_outside_grant"), "{error}");
    }

    #[test]
    fn scope_accepts_canonical_absolute_isolated_worktree_root() {
        use crate::types::resources::Resources;
        let temp = tempfile::tempdir().unwrap();
        let workspace = temp.path().join("workspace");
        let isolated = temp.path().join("isolated");
        std::fs::create_dir_all(&workspace).unwrap();
        std::fs::create_dir_all(&isolated).unwrap();
        let lease =
            WriteScopeLease::issue("g1", "root", "code", vec![isolated.clone()], 3600).unwrap();
        let mut resources = Resources::new();
        resources.insert(WriteScopeLeaseResource { lease });
        assert!(
            enforce_write_scope_if_present(&resources, &isolated.join("new.rs"), &workspace,)
                .is_ok()
        );
    }

    #[test]
    fn write_scopes_overlap_parent_child_and_disjoint() {
        let src = vec![PathBuf::from("src")];
        let src_lib = vec![PathBuf::from("src/lib")];
        let tests = vec![PathBuf::from("tests")];
        assert!(write_scopes_overlap(&src, &src_lib));
        assert!(write_scopes_overlap(&src_lib, &src));
        assert!(write_scopes_overlap(&src, &src));
        assert!(!write_scopes_overlap(&src, &tests));
        assert!(!write_scopes_overlap(&[], &src));
        assert!(!write_scopes_overlap(&src, &[]));
        // Escape roots fail closed as conflicting.
        assert!(write_scopes_overlap(
            &[PathBuf::from("../escape")],
            &[PathBuf::from("src")]
        ));
    }

    #[test]
    fn merge_handoff_requires_root_decision_and_rejects_stale_base() {
        let lease =
            WriteScopeLease::issue("lease-1", "tree", "code", vec![PathBuf::from("src")], 60)
                .unwrap();
        let now = now_unix();
        assert_eq!(
            evaluate_merge_handoff(
                &lease,
                "tree",
                "abc",
                "abc",
                vec!["sha256:p1".into()],
                &[],
                vec!["verify://1".into()],
                "",
                now,
            )
            .unwrap_err(),
            MergeHandoffDenyReason::MissingRootDecision
        );
        assert_eq!(
            evaluate_merge_handoff(
                &lease,
                "tree",
                "stale",
                "fresh",
                vec!["sha256:p1".into()],
                &[],
                vec![],
                "root-approve-1",
                now,
            )
            .unwrap_err(),
            MergeHandoffDenyReason::StaleBase
        );
        let applied = evaluate_merge_handoff(
            &lease,
            "tree",
            "base1",
            "base1",
            vec!["sha256:file".into()],
            &[],
            vec!["evidence://ok".into()],
            "root-approve-1",
            now,
        )
        .unwrap();
        assert_eq!(applied.apply_result, MergeApplyResult::Applied);
        assert_eq!(applied.root_decision_ref, "root-approve-1");

        let conflicted = evaluate_merge_handoff(
            &lease,
            "tree",
            "base1",
            "base1",
            vec!["sha256:file".into()],
            &["src/a.rs".into()],
            vec![],
            "root-approve-2",
            now,
        )
        .unwrap();
        assert_eq!(conflicted.apply_result, MergeApplyResult::Conflict);

        let mut revoked = lease.clone();
        revoked.revoke();
        assert_eq!(
            evaluate_merge_handoff(
                &revoked,
                "tree",
                "base1",
                "base1",
                vec!["sha256:file".into()],
                &[],
                vec![],
                "root-approve-3",
                now,
            )
            .unwrap_err(),
            MergeHandoffDenyReason::LeaseNotActive
        );
    }
}
