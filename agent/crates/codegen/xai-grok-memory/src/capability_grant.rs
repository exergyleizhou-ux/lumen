//! NG-02 CapabilityGrantV1 — TTL + revoke-token ceiling for task-tree nodes.
//!
//! [`AgentSandboxV1`] already enforces depth/leaf contraction and holds a
//! `capability_grant_id`. This module is the **grant object** that id names:
//! actor-issued, time-bounded, revocable by bearer token, and projectable to
//! UI/journal without leaking the revoke secret.
//!
//! Fail-closed rules (INV-style):
//! - Empty identity fields refuse issue.
//! - Expired / revoked / wrong-token always deny (no soft reopen).
//! - UI projection never includes [`CapabilityGrantV1::revoke_token`].
//! - Child grants must not widen ancestor ceilings (monotonic contraction
//!   checked at issue time against an optional parent grant).

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue};

pub const CAPABILITY_GRANT_SCHEMA_VERSION: u16 = 1;

/// Hard floor for TTL so a zero/omitted ttl cannot mean "forever".
pub const GRANT_MIN_TTL_SECS: u64 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CapabilityGrantState {
    Active,
    Revoked,
    Expired,
}

impl CapabilityGrantState {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Active => "active",
            Self::Revoked => "revoked",
            Self::Expired => "expired",
        }
    }
}

/// Coarse capability class the grant authorizes (orthogonal to ToolContract).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum GrantCapabilityClass {
    ReadOnly = 0,
    ScopedWrite = 1,
    SpawnChild = 2,
    NetworkRestricted = 3,
}

impl GrantCapabilityClass {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::ReadOnly => "read_only",
            Self::ScopedWrite => "scoped_write",
            Self::SpawnChild => "spawn_child",
            Self::NetworkRestricted => "network_restricted",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct CapabilityGrantV1 {
    pub schema_version: u16,
    pub grant_id: String,
    pub issuer_root_session_id: String,
    pub target_node_id: String,
    pub task_tree_id: String,
    /// Sorted unique classes; empty means read-only by default.
    pub capabilities: Vec<GrantCapabilityClass>,
    pub resource_scope_roots: Vec<String>,
    pub issued_at_unix: u64,
    pub expires_at_unix: u64,
    pub reason: String,
    pub approval_ref: String,
    /// Opaque bearer token required to revoke. Never projected to UI/journal.
    pub revoke_token: String,
    pub state: CapabilityGrantState,
    pub revoke_reason: Option<String>,
    /// Canonical body hash excluding the revoke token (identity is public).
    pub grant_hash: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum GrantDenyReason {
    Invalid(String),
    NotActive,
    Expired,
    Revoked,
    WrongRevokeToken,
    /// Child grant attempted to authorize a class the parent does not hold.
    WidensParent,
    ForeignTree,
    ForeignNode,
}

impl GrantDenyReason {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "grant.invalid",
            Self::NotActive => "grant.not_active",
            Self::Expired => "grant.expired",
            Self::Revoked => "grant.revoked",
            Self::WrongRevokeToken => "grant.wrong_revoke_token",
            Self::WidensParent => "grant.widens_parent",
            Self::ForeignTree => "grant.foreign_tree",
            Self::ForeignNode => "grant.foreign_node",
        }
    }
}

impl std::fmt::Display for GrantDenyReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            GrantDenyReason::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

/// Public, secret-free view for UI / ACP / journal projection.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct CapabilityGrantProjectionV1 {
    pub grant_id: String,
    pub target_node_id: String,
    pub task_tree_id: String,
    pub capabilities: Vec<String>,
    pub resource_scope_roots: Vec<String>,
    pub issued_at_unix: u64,
    pub expires_at_unix: u64,
    pub state: String,
    pub grant_hash: String,
    pub revoke_reason: Option<String>,
    /// Always false in projection — token is never exposed.
    pub has_revoke_token: bool,
}

#[derive(Debug, Clone)]
pub struct IssueGrantRequest {
    pub grant_id: String,
    pub issuer_root_session_id: String,
    pub target_node_id: String,
    pub task_tree_id: String,
    pub capabilities: Vec<GrantCapabilityClass>,
    pub resource_scope_roots: Vec<String>,
    pub issued_at_unix: u64,
    pub ttl_secs: u64,
    pub reason: String,
    pub approval_ref: String,
    pub revoke_token: String,
    /// When present, child must not exceed parent capability set / scope.
    pub parent: Option<CapabilityGrantV1>,
}

impl CapabilityGrantV1 {
    pub fn issue(req: IssueGrantRequest) -> Result<Self, GrantDenyReason> {
        if req.grant_id.trim().is_empty()
            || req.issuer_root_session_id.trim().is_empty()
            || req.target_node_id.trim().is_empty()
            || req.task_tree_id.trim().is_empty()
            || req.revoke_token.trim().is_empty()
            || req.approval_ref.trim().is_empty()
        {
            return Err(GrantDenyReason::Invalid(
                "required identity field empty".into(),
            ));
        }
        if req.ttl_secs < GRANT_MIN_TTL_SECS {
            return Err(GrantDenyReason::Invalid(format!(
                "ttl_secs must be >= {GRANT_MIN_TTL_SECS}"
            )));
        }

        let mut capabilities = req.capabilities;
        capabilities.sort();
        capabilities.dedup();
        if capabilities.is_empty() {
            capabilities.push(GrantCapabilityClass::ReadOnly);
        }

        let mut resource_scope_roots = req.resource_scope_roots;
        resource_scope_roots.sort();
        resource_scope_roots.dedup();

        if let Some(parent) = req.parent.as_ref() {
            if parent.task_tree_id != req.task_tree_id {
                return Err(GrantDenyReason::ForeignTree);
            }
            // Live parent required to mint a child under it.
            parent.ensure_live(req.issued_at_unix)?;
            for cap in &capabilities {
                if !parent.capabilities.contains(cap) {
                    return Err(GrantDenyReason::WidensParent);
                }
            }
            if !parent.resource_scope_roots.is_empty() {
                for root in &resource_scope_roots {
                    let under_parent = parent.resource_scope_roots.iter().any(|p| {
                        root == p || root.starts_with(&format!("{p}/"))
                    });
                    if !under_parent {
                        return Err(GrantDenyReason::WidensParent);
                    }
                }
            }
        }

        let mut grant = Self {
            schema_version: CAPABILITY_GRANT_SCHEMA_VERSION,
            grant_id: req.grant_id,
            issuer_root_session_id: req.issuer_root_session_id,
            target_node_id: req.target_node_id,
            task_tree_id: req.task_tree_id,
            capabilities,
            resource_scope_roots,
            issued_at_unix: req.issued_at_unix,
            expires_at_unix: req.issued_at_unix.saturating_add(req.ttl_secs),
            reason: req.reason,
            approval_ref: req.approval_ref,
            revoke_token: req.revoke_token,
            state: CapabilityGrantState::Active,
            revoke_reason: None,
            grant_hash: String::new(),
        };
        grant.grant_hash = grant
            .compute_grant_hash()
            .map_err(|e| GrantDenyReason::Invalid(e.to_string()))?;
        Ok(grant)
    }

    /// Public identity hash — excludes `revoke_token` so UI can verify
    /// integrity without holding the secret.
    pub fn compute_grant_hash(&self) -> Result<String, CanonicalError> {
        let caps: Vec<CanonicalValue> = self
            .capabilities
            .iter()
            .map(|c| CanonicalValue::str(c.as_str()))
            .collect();
        let scopes: Vec<CanonicalValue> = self
            .resource_scope_roots
            .iter()
            .map(|s| CanonicalValue::str(s.as_str()))
            .collect();
        CanonicalRecord::new("capability-grant")
            .field("schema_version", CanonicalValue::U64(u64::from(self.schema_version)))
            .field("grant_id", CanonicalValue::str(&self.grant_id))
            .field(
                "issuer_root_session_id",
                CanonicalValue::str(&self.issuer_root_session_id),
            )
            .field("target_node_id", CanonicalValue::str(&self.target_node_id))
            .field("task_tree_id", CanonicalValue::str(&self.task_tree_id))
            .field("capabilities", CanonicalValue::Seq(caps))
            .field("resource_scope_roots", CanonicalValue::Seq(scopes))
            .field("issued_at_unix", CanonicalValue::U64(self.issued_at_unix))
            .field("expires_at_unix", CanonicalValue::U64(self.expires_at_unix))
            .field("reason", CanonicalValue::str(&self.reason))
            .field("approval_ref", CanonicalValue::str(&self.approval_ref))
            .field("state", CanonicalValue::str(self.state.as_str()))
            .record_hash()
    }

    pub fn ensure_live(&self, now_unix: u64) -> Result<(), GrantDenyReason> {
        match self.state {
            CapabilityGrantState::Revoked => return Err(GrantDenyReason::Revoked),
            CapabilityGrantState::Expired => return Err(GrantDenyReason::Expired),
            CapabilityGrantState::Active => {}
        }
        if now_unix >= self.expires_at_unix {
            return Err(GrantDenyReason::Expired);
        }
        Ok(())
    }

    /// Authorize an action for `node_id` under this grant at `now_unix`.
    pub fn authorize(
        &self,
        task_tree_id: &str,
        node_id: &str,
        needed: GrantCapabilityClass,
        now_unix: u64,
    ) -> Result<(), GrantDenyReason> {
        self.ensure_live(now_unix)?;
        if self.task_tree_id != task_tree_id {
            return Err(GrantDenyReason::ForeignTree);
        }
        if self.target_node_id != node_id {
            return Err(GrantDenyReason::ForeignNode);
        }
        if needed == GrantCapabilityClass::ReadOnly {
            return Ok(());
        }
        if self.capabilities.contains(&needed) {
            Ok(())
        } else {
            Err(GrantDenyReason::NotActive)
        }
    }

    /// Revoke with the bearer token issued at creation. Wrong token fails
    /// closed without changing state.
    pub fn revoke(
        &mut self,
        token: &str,
        reason: impl Into<String>,
        now_unix: u64,
    ) -> Result<(), GrantDenyReason> {
        if self.state == CapabilityGrantState::Revoked {
            return Err(GrantDenyReason::Revoked);
        }
        // Expiry is still a terminal state, but wrong token must not flip it.
        if token != self.revoke_token {
            return Err(GrantDenyReason::WrongRevokeToken);
        }
        if now_unix >= self.expires_at_unix && self.state == CapabilityGrantState::Active {
            self.state = CapabilityGrantState::Expired;
            return Err(GrantDenyReason::Expired);
        }
        self.state = CapabilityGrantState::Revoked;
        self.revoke_reason = Some(reason.into());
        // Re-hash so projection reflects revoked state.
        self.grant_hash = self
            .compute_grant_hash()
            .map_err(|e| GrantDenyReason::Invalid(e.to_string()))?;
        Ok(())
    }

    /// Mark expired when observed past TTL (idempotent).
    pub fn observe_clock(&mut self, now_unix: u64) {
        if self.state == CapabilityGrantState::Active && now_unix >= self.expires_at_unix {
            self.state = CapabilityGrantState::Expired;
            if let Ok(h) = self.compute_grant_hash() {
                self.grant_hash = h;
            }
        }
    }

    /// Secret-free projection for UI/ACP/journal.
    pub fn project(&self) -> CapabilityGrantProjectionV1 {
        CapabilityGrantProjectionV1 {
            grant_id: self.grant_id.clone(),
            target_node_id: self.target_node_id.clone(),
            task_tree_id: self.task_tree_id.clone(),
            capabilities: self
                .capabilities
                .iter()
                .map(|c| c.as_str().to_owned())
                .collect(),
            resource_scope_roots: self.resource_scope_roots.clone(),
            issued_at_unix: self.issued_at_unix,
            expires_at_unix: self.expires_at_unix,
            state: self.state.as_str().to_owned(),
            grant_hash: self.grant_hash.clone(),
            revoke_reason: self.revoke_reason.clone(),
            has_revoke_token: !self.revoke_token.is_empty(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn base_req() -> IssueGrantRequest {
        IssueGrantRequest {
            grant_id: "grant-1".into(),
            issuer_root_session_id: "root".into(),
            target_node_id: "child".into(),
            task_tree_id: "tree".into(),
            capabilities: vec![
                GrantCapabilityClass::ReadOnly,
                GrantCapabilityClass::ScopedWrite,
            ],
            resource_scope_roots: vec!["src".into()],
            issued_at_unix: 1_000,
            ttl_secs: 60,
            reason: "test".into(),
            approval_ref: "approval-1".into(),
            revoke_token: "tok-secret".into(),
            parent: None,
        }
    }

    #[test]
    fn issue_active_grant_and_authorize_positive() {
        let g = CapabilityGrantV1::issue(base_req()).unwrap();
        assert_eq!(g.state, CapabilityGrantState::Active);
        assert_eq!(g.expires_at_unix, 1_060);
        assert!(g
            .authorize("tree", "child", GrantCapabilityClass::ScopedWrite, 1_010)
            .is_ok());
        assert!(g
            .authorize("tree", "child", GrantCapabilityClass::ReadOnly, 1_010)
            .is_ok());
    }

    #[test]
    fn ttl_expiry_and_observe_clock_fail_closed() {
        let mut g = CapabilityGrantV1::issue(base_req()).unwrap();
        assert_eq!(
            g.authorize("tree", "child", GrantCapabilityClass::ReadOnly, 1_060)
                .unwrap_err(),
            GrantDenyReason::Expired
        );
        g.observe_clock(1_060);
        assert_eq!(g.state, CapabilityGrantState::Expired);
    }

    #[test]
    fn revoke_requires_correct_token_and_blocks_authorize() {
        let mut g = CapabilityGrantV1::issue(base_req()).unwrap();
        assert_eq!(
            g.revoke("wrong", "nope", 1_010).unwrap_err(),
            GrantDenyReason::WrongRevokeToken
        );
        assert_eq!(g.state, CapabilityGrantState::Active);
        g.revoke("tok-secret", "user cancelled", 1_010).unwrap();
        assert_eq!(g.state, CapabilityGrantState::Revoked);
        assert_eq!(g.revoke_reason.as_deref(), Some("user cancelled"));
        assert_eq!(
            g.authorize("tree", "child", GrantCapabilityClass::ReadOnly, 1_010)
                .unwrap_err(),
            GrantDenyReason::Revoked
        );
    }

    #[test]
    fn child_grant_cannot_widen_parent_capability_or_scope() {
        let parent = CapabilityGrantV1::issue(base_req()).unwrap();
        let mut child_req = base_req();
        child_req.grant_id = "grant-child".into();
        child_req.target_node_id = "grandchild".into();
        child_req.capabilities = vec![
            GrantCapabilityClass::ReadOnly,
            GrantCapabilityClass::SpawnChild, // parent lacks this
        ];
        child_req.parent = Some(parent.clone());
        assert_eq!(
            CapabilityGrantV1::issue(child_req).unwrap_err(),
            GrantDenyReason::WidensParent
        );

        let mut child_req = base_req();
        child_req.grant_id = "grant-child2".into();
        child_req.target_node_id = "grandchild".into();
        child_req.resource_scope_roots = vec!["/etc".into()]; // outside parent src
        child_req.parent = Some(parent);
        assert_eq!(
            CapabilityGrantV1::issue(child_req).unwrap_err(),
            GrantDenyReason::WidensParent
        );
    }

    #[test]
    fn child_grant_under_parent_scope_is_allowed() {
        let parent = CapabilityGrantV1::issue(base_req()).unwrap();
        let mut child_req = base_req();
        child_req.grant_id = "grant-child".into();
        child_req.target_node_id = "grandchild".into();
        child_req.capabilities = vec![GrantCapabilityClass::ReadOnly];
        child_req.resource_scope_roots = vec!["src/lib".into()];
        child_req.parent = Some(parent);
        let child = CapabilityGrantV1::issue(child_req).unwrap();
        assert!(child
            .authorize("tree", "grandchild", GrantCapabilityClass::ReadOnly, 1_010)
            .is_ok());
    }

    #[test]
    fn projection_never_leaks_revoke_token() {
        let g = CapabilityGrantV1::issue(base_req()).unwrap();
        let proj = g.project();
        let json = serde_json::to_string(&proj).unwrap();
        assert!(!json.contains("tok-secret"), "secret must not appear: {json}");
        // `has_revoke_token` is the only public mention of "revoke_token"; the
        // secret field itself is never serialized.
        assert!(!json.contains("\"revoke_token\""));
        assert!(proj.has_revoke_token);
        assert_eq!(proj.state, "active");
        assert_eq!(proj.grant_hash, g.grant_hash);
    }

    #[test]
    fn grant_hash_stable_and_excludes_token() {
        let mut a = base_req();
        let mut b = base_req();
        b.revoke_token = "different-secret".into();
        let ga = CapabilityGrantV1::issue(a.clone()).unwrap();
        let gb = CapabilityGrantV1::issue(b).unwrap();
        // Same public fields → same hash even if tokens differ.
        assert_eq!(ga.grant_hash, gb.grant_hash);
        a.reason = "other".into();
        let gc = CapabilityGrantV1::issue(a).unwrap();
        assert_ne!(ga.grant_hash, gc.grant_hash);
    }

    #[test]
    fn empty_identity_and_zero_ttl_fail_closed() {
        let mut req = base_req();
        req.grant_id.clear();
        assert!(matches!(
            CapabilityGrantV1::issue(req).unwrap_err(),
            GrantDenyReason::Invalid(_)
        ));
        let mut req = base_req();
        req.ttl_secs = 0;
        assert!(matches!(
            CapabilityGrantV1::issue(req).unwrap_err(),
            GrantDenyReason::Invalid(_)
        ));
    }
}
