//! RootBypassPermission — INV-5 root-only bypass.
//!
//! Every field of the master plan §2.1 contract is mandatory:
//! issuer = root SessionActor (type-level: [`BypassIssuer`] has a single
//! variant), exact action + resource scope, human-visible reason, mandatory
//! `expires_at_unix` (zero/omitted expiry is invalid, never "forever"),
//! nonce + audit_id, revocable, and **child inheritance forbidden** —
//! [`RootBypassPermission::derive_child_permission`] always denies.
//!
//! This is deliberately not the same thing as always-approve,
//! `PermissionHandle`, yolo or an environment variable; it cannot be mapped
//! onto a child, Advisor, Kairos or MCP tool.

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue};

pub const ROOT_BYPASS_SCHEMA_VERSION: u16 = 1;

/// The only issuer that may mint a bypass is the root SessionActor. A single
/// variant makes "issued by root" a type-level fact — no other issuer can be
/// expressed.
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum BypassIssuer {
    RootSessionActor,
}

impl BypassIssuer {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::RootSessionActor => "root_session_actor",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct RootBypassPermission {
    pub schema_version: u16,
    pub permission_id: String,
    pub issuer: BypassIssuer,
    pub root_session_id: String,
    /// Exact action string — a bypass for `edit:file` does not cover
    /// `shell:exec` (no wildcards, exact match).
    pub exact_action: String,
    /// Exact resource scope the action applies to.
    pub resource_scope: String,
    /// Human-visible reason, mandatory.
    pub reason: String,
    pub issued_at_unix: u64,
    /// Mandatory expiry. A zero/omitted expiry is an invalid permission.
    pub expires_at_unix: u64,
    /// Mandatory nonce — unique per issuance, part of audit trail.
    pub nonce: String,
    /// Mandatory audit id — the journal entry this bypass is traceable to.
    pub audit_id: String,
    pub revoked: bool,
    pub revoke_reason: Option<String>,
    /// Canonical hash over all fields above.
    pub permission_hash: String,
}

/// Secret-free projection for UI / journal (INV-17: no raw bypass details in
/// previews beyond what is needed for audit).
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct RootBypassProjection {
    pub permission_id: String,
    pub exact_action: String,
    pub resource_scope: String,
    pub reason: String,
    pub issued_at_unix: u64,
    pub expires_at_unix: u64,
    pub audit_id: String,
    pub revoked: bool,
    pub permission_hash: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BypassDeny {
    Invalid(String),
    EmptyField(&'static str),
    MissingExpiry,
    Expired,
    Revoked,
    ScopeMismatch,
    ChildInheritanceForbidden,
    HashMismatch,
}

impl BypassDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "bypass.invalid",
            Self::EmptyField(_) => "bypass.empty_field",
            Self::MissingExpiry => "bypass.missing_expiry",
            Self::Expired => "bypass.expired",
            Self::Revoked => "bypass.revoked",
            Self::ScopeMismatch => "bypass.scope_mismatch",
            Self::ChildInheritanceForbidden => "bypass.child_inheritance_forbidden",
            Self::HashMismatch => "bypass.hash_mismatch",
        }
    }
}

impl std::fmt::Display for BypassDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

#[derive(Debug, Clone)]
pub struct IssueBypassRequest {
    pub permission_id: String,
    pub root_session_id: String,
    pub exact_action: String,
    pub resource_scope: String,
    pub reason: String,
    pub issued_at_unix: u64,
    pub expires_at_unix: u64,
    pub nonce: String,
    pub audit_id: String,
}

fn require_non_empty(field: &'static str, value: &str) -> Result<(), BypassDeny> {
    if value.trim().is_empty() {
        return Err(BypassDeny::EmptyField(field));
    }
    Ok(())
}

fn permission_preimage(permission: &RootBypassPermission) -> Result<Vec<u8>, CanonicalError> {
    CanonicalRecord::new("root-bypass-permission")
        .field("schema_version", CanonicalValue::U64(permission.schema_version as u64))
        .field("permission_id", CanonicalValue::str(&permission.permission_id))
        .field("issuer", CanonicalValue::str(permission.issuer.as_str()))
        .field("root_session_id", CanonicalValue::str(&permission.root_session_id))
        .field("exact_action", CanonicalValue::str(&permission.exact_action))
        .field("resource_scope", CanonicalValue::str(&permission.resource_scope))
        .field("reason", CanonicalValue::str(&permission.reason))
        .field("issued_at_unix", CanonicalValue::U64(permission.issued_at_unix))
        .field("expires_at_unix", CanonicalValue::U64(permission.expires_at_unix))
        .field("nonce", CanonicalValue::str(&permission.nonce))
        .field("audit_id", CanonicalValue::str(&permission.audit_id))
        .field("revoked", CanonicalValue::Bool(permission.revoked))
        .field(
            "revoke_reason",
            match &permission.revoke_reason {
                Some(reason) => CanonicalValue::str(reason),
                None => CanonicalValue::Null,
            },
        )
        .canonical_bytes()
}

/// Issue a root bypass. Every INV-5 field is mandatory; the issuer is
/// type-fixed to [`BypassIssuer::RootSessionActor`].
pub fn issue_root_bypass(req: IssueBypassRequest) -> Result<RootBypassPermission, BypassDeny> {
    require_non_empty("permission_id", &req.permission_id)?;
    require_non_empty("root_session_id", &req.root_session_id)?;
    require_non_empty("exact_action", &req.exact_action)?;
    require_non_empty("resource_scope", &req.resource_scope)?;
    require_non_empty("reason", &req.reason)?;
    require_non_empty("nonce", &req.nonce)?;
    require_non_empty("audit_id", &req.audit_id)?;
    if req.expires_at_unix == 0 || req.expires_at_unix <= req.issued_at_unix {
        return Err(BypassDeny::MissingExpiry);
    }
    let permission = RootBypassPermission {
        schema_version: ROOT_BYPASS_SCHEMA_VERSION,
        permission_id: req.permission_id,
        issuer: BypassIssuer::RootSessionActor,
        root_session_id: req.root_session_id,
        exact_action: req.exact_action,
        resource_scope: req.resource_scope,
        reason: req.reason,
        issued_at_unix: req.issued_at_unix,
        expires_at_unix: req.expires_at_unix,
        nonce: req.nonce,
        audit_id: req.audit_id,
        revoked: false,
        revoke_reason: None,
        permission_hash: String::new(),
    };
    let mut permission = permission;
    let hash = permission_preimage(&permission)
        .map_err(|e| BypassDeny::Invalid(e.to_string()))?;
    permission.permission_hash = format!("sha256:{}", blake3::hash(&hash).to_hex());
    permission.validate()?;
    Ok(permission)
}

impl RootBypassPermission {
    /// Recompute the canonical hash and check every invariant.
    pub fn validate(&self) -> Result<(), BypassDeny> {
        if self.schema_version != ROOT_BYPASS_SCHEMA_VERSION {
            return Err(BypassDeny::Invalid("schema_version mismatch".into()));
        }
        if self.issuer != BypassIssuer::RootSessionActor {
            return Err(BypassDeny::Invalid("issuer must be root session actor".into()));
        }
        require_non_empty("permission_id", &self.permission_id)?;
        require_non_empty("root_session_id", &self.root_session_id)?;
        require_non_empty("exact_action", &self.exact_action)?;
        require_non_empty("resource_scope", &self.resource_scope)?;
        require_non_empty("reason", &self.reason)?;
        require_non_empty("nonce", &self.nonce)?;
        require_non_empty("audit_id", &self.audit_id)?;
        if self.expires_at_unix == 0 || self.expires_at_unix <= self.issued_at_unix {
            return Err(BypassDeny::MissingExpiry);
        }
        let recomputed = permission_preimage(self)
            .map_err(|e| BypassDeny::Invalid(e.to_string()))?;
        if format!("sha256:{}", blake3::hash(&recomputed).to_hex()) != self.permission_hash {
            return Err(BypassDeny::HashMismatch);
        }
        Ok(())
    }

    /// Authorize an exact action + scope at time `now`.
    pub fn authorize(
        &self,
        action: &str,
        resource_scope: &str,
        now_unix: u64,
    ) -> Result<(), BypassDeny> {
        self.validate()?;
        if self.revoked {
            return Err(BypassDeny::Revoked);
        }
        if now_unix > self.expires_at_unix {
            return Err(BypassDeny::Expired);
        }
        if action != self.exact_action {
            return Err(BypassDeny::ScopeMismatch);
        }
        if resource_scope != self.resource_scope {
            return Err(BypassDeny::ScopeMismatch);
        }
        Ok(())
    }

    /// Revoke the bypass. All subsequent `authorize` calls fail closed.
    pub fn revoke(&mut self, reason: impl Into<String>) -> Result<(), BypassDeny> {
        if self.revoked {
            return Err(BypassDeny::Revoked);
        }
        self.revoked = true;
        self.revoke_reason = Some(reason.into());
        // Revocation is part of the canonical record: re-stamp the hash so
        // `validate` (and any journal replay) sees a consistent record.
        let hash = permission_preimage(self)
            .map_err(|e| BypassDeny::Invalid(e.to_string()))?;
        self.permission_hash = format!("sha256:{}", blake3::hash(&hash).to_hex());
        Ok(())
    }

    /// Child inheritance is forbidden (INV-5): a child can never derive a
    /// bypass from its parent. Always denies.
    pub fn derive_child_permission(&self) -> Result<RootBypassPermission, BypassDeny> {
        Err(BypassDeny::ChildInheritanceForbidden)
    }

    /// Secret-free projection for UI / journal.
    pub fn projection(&self) -> RootBypassProjection {
        RootBypassProjection {
            permission_id: self.permission_id.clone(),
            exact_action: self.exact_action.clone(),
            resource_scope: self.resource_scope.clone(),
            reason: self.reason.clone(),
            issued_at_unix: self.issued_at_unix,
            expires_at_unix: self.expires_at_unix,
            audit_id: self.audit_id.clone(),
            revoked: self.revoked,
            permission_hash: self.permission_hash.clone(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_request() -> IssueBypassRequest {
        IssueBypassRequest {
            permission_id: "bypass-1".into(),
            root_session_id: "sess-root".into(),
            exact_action: "edit:file".into(),
            resource_scope: "repo://crate/foo.rs".into(),
            reason: "user-approved manual override for release stamp".into(),
            issued_at_unix: 1_000,
            expires_at_unix: 2_000,
            nonce: "nonce-1".into(),
            audit_id: "audit-1".into(),
        }
    }

    #[test]
    fn issue_and_authorize_positive() {
        let permission = issue_root_bypass(sample_request()).expect("issue");
        permission.validate().expect("valid");
        permission
            .authorize("edit:file", "repo://crate/foo.rs", 1_500)
            .expect("authorize");
        let projection = permission.projection();
        assert_eq!(projection.permission_id, "bypass-1");
        assert_eq!(projection.audit_id, "audit-1");
        assert!(!projection.revoked);
        assert!(projection.permission_hash.starts_with("sha256:"));
    }

    #[test]
    fn issue_rejects_missing_expiry() {
        let mut req = sample_request();
        req.expires_at_unix = 0;
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::MissingExpiry
        );
        let mut req = sample_request();
        req.expires_at_unix = req.issued_at_unix;
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::MissingExpiry
        );
        let mut req = sample_request();
        req.expires_at_unix = req.issued_at_unix - 1;
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::MissingExpiry
        );
    }

    #[test]
    fn issue_rejects_missing_mandatory_fields() {
        let mut req = sample_request();
        req.reason = "".into();
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::EmptyField("reason")
        );
        let mut req = sample_request();
        req.nonce = "".into();
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::EmptyField("nonce")
        );
        let mut req = sample_request();
        req.audit_id = "".into();
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::EmptyField("audit_id")
        );
        let mut req = sample_request();
        req.exact_action = "".into();
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::EmptyField("exact_action")
        );
        let mut req = sample_request();
        req.resource_scope = "".into();
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::EmptyField("resource_scope")
        );
        let mut req = sample_request();
        req.root_session_id = "".into();
        assert_eq!(
            issue_root_bypass(req).unwrap_err(),
            BypassDeny::EmptyField("root_session_id")
        );
    }

    #[test]
    fn authorize_rejects_expired_and_revoked() {
        let permission = issue_root_bypass(sample_request()).expect("issue");
        assert_eq!(
            permission
                .authorize("edit:file", "repo://crate/foo.rs", 2_001)
                .unwrap_err(),
            BypassDeny::Expired
        );
        let mut permission = permission;
        permission.revoke("done").expect("revoke");
        assert_eq!(
            permission
                .authorize("edit:file", "repo://crate/foo.rs", 1_500)
                .unwrap_err(),
            BypassDeny::Revoked
        );
        assert_eq!(
            permission.revoke("again").unwrap_err(),
            BypassDeny::Revoked
        );
        assert_eq!(permission.revoke_reason.as_deref(), Some("done"));
    }

    #[test]
    fn authorize_rejects_scope_mismatch() {
        let permission = issue_root_bypass(sample_request()).expect("issue");
        assert_eq!(
            permission
                .authorize("shell:exec", "repo://crate/foo.rs", 1_500)
                .unwrap_err(),
            BypassDeny::ScopeMismatch
        );
        assert_eq!(
            permission
                .authorize("edit:file", "repo://crate/bar.rs", 1_500)
                .unwrap_err(),
            BypassDeny::ScopeMismatch
        );
    }

    #[test]
    fn child_inheritance_forbidden() {
        let permission = issue_root_bypass(sample_request()).expect("issue");
        assert_eq!(
            permission.derive_child_permission().unwrap_err(),
            BypassDeny::ChildInheritanceForbidden
        );
    }

    #[test]
    fn hash_tamper_detected() {
        let mut permission = issue_root_bypass(sample_request()).expect("issue");
        permission.exact_action = "shell:exec".into();
        assert_eq!(permission.validate().unwrap_err(), BypassDeny::HashMismatch);
    }
}
