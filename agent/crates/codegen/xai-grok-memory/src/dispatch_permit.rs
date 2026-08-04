//! NG-01/0.1.3 typed admission choke point — the linear admission chain
//! `RawIntent → IdentityBound → PolicyResolved → ContextAdmitted →
//! BudgetReserved → DispatchPermitV1`.
//!
//! Every stage holds **crate-private fields**: nothing outside this crate can
//! construct a stage or the final permit by hand. The only way to obtain a
//! [`DispatchPermitV1`] is to pass every prior stage through the chain
//! functions, each of which validates its invariant. [`PermitConsumer`] is a
//! closed enum of the registered dispatch surfaces (spawn / worktree apply /
//! terminal / process / provider), so an unregistered entry cannot request a
//! permit at all — there is no stringly-typed escape hatch.
//!
//! The type system cannot prove external processes, MCP, deserialization or
//! crash recovery correct: dispatch must still re-verify the live grant
//! revision, lease epoch and cancellation at the consumer (INV-12). The
//! permit's [`DispatchPermitV1::binding`] exposes the immutable binding
//! snapshot the actor compares against live state.

use crate::capability_grant::{CapabilityGrantState, CapabilityGrantV1};
use crate::identity_envelope::NodeIdentityV1;

pub const DISPATCH_PERMIT_SCHEMA_VERSION: u16 = 1;

/// Registered dispatch surfaces. Adding a consumer here is the only way to
/// make a new entry point eligible for permits; an unregistered adapter
/// cannot express a permit request.
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum PermitConsumer {
    SpawnAdapter,
    WorktreeApplyAdapter,
    TerminalAdapter,
    ProcessAdapter,
    ProviderAdapter,
}

impl PermitConsumer {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::SpawnAdapter => "spawn",
            Self::WorktreeApplyAdapter => "worktree_apply",
            Self::TerminalAdapter => "terminal",
            Self::ProcessAdapter => "process",
            Self::ProviderAdapter => "provider",
        }
    }
}

/// Stage 1 — raw intent before any identity binding. Crate-private fields.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RawIntent {
    assignment_hash: String,
    objective_ref: String,
}

/// Stage 2 — intent bound to a validated node identity.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IdentityBound {
    identity_hash: String,
    node_id: String,
    task_tree_id: String,
    grant_revision: u64,
}

/// Stage 3 — policy resolved against a live capability grant.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PolicyResolved {
    identity_hash: String,
    node_id: String,
    task_tree_id: String,
    grant_revision: u64,
    policy_revision: u64,
    capability_grant_id: String,
}

/// Stage 4 — context admitted (manifest + accepted snapshot bound).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ContextAdmitted {
    identity_hash: String,
    node_id: String,
    task_tree_id: String,
    grant_revision: u64,
    policy_revision: u64,
    capability_grant_id: String,
    manifest_hash: String,
    accepted_snapshot_hash: String,
}

/// Stage 5 — budget reserved (reservation id + expiry).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetReserved {
    identity_hash: String,
    node_id: String,
    task_tree_id: String,
    grant_revision: u64,
    policy_revision: u64,
    capability_grant_id: String,
    manifest_hash: String,
    accepted_snapshot_hash: String,
    reservation_id: String,
    budget_expiry_unix: u64,
}

/// Final permit — the only object a governed dispatch consumer may hold.
/// Crate-private fields; minted exclusively by [`mint_dispatch_permit`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DispatchPermitV1 {
    identity_hash: String,
    grant_revision: u64,
    manifest_hash: String,
    budget_reservation_id: String,
    policy_revision: u64,
    expiry_unix: u64,
    adapter_contract_hash: String,
    consumer: PermitConsumer,
    nonce: String,
    revoked: bool,
}

/// Immutable binding snapshot for actor-side re-verification at dispatch
/// (INV-12). Public read accessor — the permit itself stays opaque.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct PermitBinding {
    pub identity_hash: String,
    pub grant_revision: u64,
    pub manifest_hash: String,
    pub budget_reservation_id: String,
    pub policy_revision: u64,
    pub expiry_unix: u64,
    pub adapter_contract_hash: String,
    pub consumer: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PermitDeny {
    Invalid(String),
    EmptyField(&'static str),
    NotSha256(&'static str),
    IdentityMismatch,
    GrantNotActive,
    GrantExpired,
    GrantRevoked,
    ForeignNode,
    ForeignTree,
    MissingManifest,
    MissingSnapshot,
    MissingReservation,
    BudgetExpired,
    Expired,
    Revoked,
    ConsumerMismatch,
    StaleGrantRevision,
}

impl PermitDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "permit.invalid",
            Self::EmptyField(_) => "permit.empty_field",
            Self::NotSha256(_) => "permit.not_sha256",
            Self::IdentityMismatch => "permit.identity_mismatch",
            Self::GrantNotActive => "permit.grant_not_active",
            Self::GrantExpired => "permit.grant_expired",
            Self::GrantRevoked => "permit.grant_revoked",
            Self::ForeignNode => "permit.foreign_node",
            Self::ForeignTree => "permit.foreign_tree",
            Self::MissingManifest => "permit.missing_manifest",
            Self::MissingSnapshot => "permit.missing_snapshot",
            Self::MissingReservation => "permit.missing_reservation",
            Self::BudgetExpired => "permit.budget_expired",
            Self::Expired => "permit.expired",
            Self::Revoked => "permit.revoked",
            Self::ConsumerMismatch => "permit.consumer_mismatch",
            Self::StaleGrantRevision => "permit.stale_grant_revision",
        }
    }
}

impl std::fmt::Display for PermitDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            Self::NotSha256(name) => write!(f, "{}: {name}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

fn require_non_empty(field: &'static str, value: &str) -> Result<(), PermitDeny> {
    if value.trim().is_empty() {
        return Err(PermitDeny::EmptyField(field));
    }
    Ok(())
}

fn require_sha256(field: &'static str, value: &str) -> Result<(), PermitDeny> {
    require_non_empty(field, value)?;
    if !value.starts_with("sha256:") || value.len() <= "sha256:".len() {
        return Err(PermitDeny::NotSha256(field));
    }
    Ok(())
}

impl RawIntent {
    /// Create a raw intent. Public so the SessionActor (or its direct tool
    /// layer) can start a governed dispatch, but the fields are private and
    /// the object is only useful as the entry of the admission chain.
    pub fn new(
        assignment_hash: impl Into<String>,
        objective_ref: impl Into<String>,
    ) -> Result<Self, PermitDeny> {
        let assignment_hash = assignment_hash.into();
        let objective_ref = objective_ref.into();
        require_sha256("assignment_hash", &assignment_hash)?;
        require_non_empty("objective_ref", &objective_ref)?;
        Ok(Self {
            assignment_hash,
            objective_ref,
        })
    }
}

/// Stage 2: bind the intent to a validated node identity.
pub fn bind_identity(
    raw: RawIntent,
    node: &NodeIdentityV1,
    grant_revision: u64,
) -> Result<IdentityBound, PermitDeny> {
    node.validate().map_err(|e| PermitDeny::Invalid(e.to_string()))?;
    if raw.assignment_hash != node.immutable_assignment_hash {
        return Err(PermitDeny::IdentityMismatch);
    }
    if grant_revision == 0 {
        return Err(PermitDeny::StaleGrantRevision);
    }
    Ok(IdentityBound {
        identity_hash: node.identity_hash.clone(),
        node_id: node.node_id.clone(),
        task_tree_id: node.task_tree_id.clone(),
        grant_revision,
    })
}

/// Stage 3: resolve policy against the live capability grant.
pub fn resolve_policy(
    bound: IdentityBound,
    grant: &CapabilityGrantV1,
    policy_revision: u64,
    now_unix: u64,
) -> Result<PolicyResolved, PermitDeny> {
    if grant.target_node_id != bound.node_id {
        return Err(PermitDeny::ForeignNode);
    }
    if grant.task_tree_id != bound.task_tree_id {
        return Err(PermitDeny::ForeignTree);
    }
    match grant.state {
        CapabilityGrantState::Revoked => return Err(PermitDeny::GrantRevoked),
        CapabilityGrantState::Expired => return Err(PermitDeny::GrantExpired),
        CapabilityGrantState::Active => {}
    }
    if grant.expires_at_unix < now_unix {
        return Err(PermitDeny::GrantExpired);
    }
    if policy_revision == 0 {
        return Err(PermitDeny::Invalid("policy_revision must start at 1".into()));
    }
    Ok(PolicyResolved {
        identity_hash: bound.identity_hash,
        node_id: bound.node_id,
        task_tree_id: bound.task_tree_id,
        grant_revision: bound.grant_revision,
        policy_revision,
        capability_grant_id: grant.grant_id.clone(),
    })
}

/// Stage 4: admit context — manifest + accepted snapshot must be bound.
pub fn admit_context(
    resolved: PolicyResolved,
    manifest_hash: impl Into<String>,
    accepted_snapshot_hash: impl Into<String>,
) -> Result<ContextAdmitted, PermitDeny> {
    let manifest_hash = manifest_hash.into();
    let accepted_snapshot_hash = accepted_snapshot_hash.into();
    require_sha256("manifest_hash", &manifest_hash)?;
    require_sha256("accepted_snapshot_hash", &accepted_snapshot_hash)?;
    Ok(ContextAdmitted {
        identity_hash: resolved.identity_hash,
        node_id: resolved.node_id,
        task_tree_id: resolved.task_tree_id,
        grant_revision: resolved.grant_revision,
        policy_revision: resolved.policy_revision,
        capability_grant_id: resolved.capability_grant_id,
        manifest_hash,
        accepted_snapshot_hash,
    })
}

/// Stage 5: reserve budget — a valid reservation id with a future expiry.
pub fn reserve_budget(
    admitted: ContextAdmitted,
    reservation_id: impl Into<String>,
    budget_expiry_unix: u64,
    now_unix: u64,
) -> Result<BudgetReserved, PermitDeny> {
    let reservation_id = reservation_id.into();
    require_non_empty("reservation_id", &reservation_id)?;
    if budget_expiry_unix <= now_unix {
        return Err(PermitDeny::BudgetExpired);
    }
    Ok(BudgetReserved {
        identity_hash: admitted.identity_hash,
        node_id: admitted.node_id,
        task_tree_id: admitted.task_tree_id,
        grant_revision: admitted.grant_revision,
        policy_revision: admitted.policy_revision,
        capability_grant_id: admitted.capability_grant_id,
        manifest_hash: admitted.manifest_hash,
        accepted_snapshot_hash: admitted.accepted_snapshot_hash,
        reservation_id,
        budget_expiry_unix,
    })
}

/// Final stage: mint the dispatch permit for one registered consumer.
/// The permit expiry is capped by the budget expiry — a permit can never
/// outlive its reservation.
pub fn mint_dispatch_permit(
    reserved: BudgetReserved,
    adapter_contract_hash: impl Into<String>,
    consumer: PermitConsumer,
    now_unix: u64,
) -> Result<DispatchPermitV1, PermitDeny> {
    let adapter_contract_hash = adapter_contract_hash.into();
    require_sha256("adapter_contract_hash", &adapter_contract_hash)?;
    if reserved.budget_expiry_unix <= now_unix {
        return Err(PermitDeny::BudgetExpired);
    }
    // Deterministic nonce over the binding — stable across serialization,
    // unique per reservation/consumer/contract/instant.
    let nonce_input = format!(
        "{}|{}|{}|{}|{}",
        reserved.reservation_id,
        consumer.as_str(),
        adapter_contract_hash,
        now_unix,
        reserved.identity_hash
    );
    let nonce = format!("{}", blake3::hash(nonce_input.as_bytes()).to_hex());
    Ok(DispatchPermitV1 {
        identity_hash: reserved.identity_hash,
        grant_revision: reserved.grant_revision,
        manifest_hash: reserved.manifest_hash,
        budget_reservation_id: reserved.reservation_id,
        policy_revision: reserved.policy_revision,
        expiry_unix: reserved.budget_expiry_unix,
        adapter_contract_hash,
        consumer,
        nonce,
        revoked: false,
    })
}

impl DispatchPermitV1 {
    /// The immutable binding the actor compares against live grant revision,
    /// lease epoch and cancellation at dispatch time (INV-12).
    pub fn binding(&self) -> PermitBinding {
        PermitBinding {
            identity_hash: self.identity_hash.clone(),
            grant_revision: self.grant_revision,
            manifest_hash: self.manifest_hash.clone(),
            budget_reservation_id: self.budget_reservation_id.clone(),
            policy_revision: self.policy_revision,
            expiry_unix: self.expiry_unix,
            adapter_contract_hash: self.adapter_contract_hash.clone(),
            consumer: self.consumer.as_str().to_string(),
        }
    }

    pub fn nonce(&self) -> &str {
        &self.nonce
    }

    /// Authorize dispatch for a consumer at time `now`. The consumer must be
    /// the one the permit was minted for; a permit presented to a different
    /// adapter is rejected even if everything else matches.
    pub fn authorize(&self, consumer: PermitConsumer, now_unix: u64) -> Result<(), PermitDeny> {
        if self.revoked {
            return Err(PermitDeny::Revoked);
        }
        if self.expiry_unix < now_unix {
            return Err(PermitDeny::Expired);
        }
        if consumer != self.consumer {
            return Err(PermitDeny::ConsumerMismatch);
        }
        Ok(())
    }

    /// Actor-side revocation — makes all future dispatches fail closed.
    pub fn revoke(&mut self) {
        self.revoked = true;
    }
}

/// Convenience: mint a spawn-adapter permit from the fields a host genuinely
/// holds at child-spawn time (identity from lineage, grant from the
/// capability ceiling, manifest from the session context). Runs the full
/// linear chain internally — the permit is only minted when every stage
/// validates. The budget reservation reference is the host's attempt-scoped
/// id; the authoritative budget remains the coordinator's BudgetLedger
/// (DEBT-024(d): adapter-side integration).
pub fn mint_governed_spawn_permit(
    node: &crate::identity_envelope::NodeIdentityV1,
    grant: &crate::capability_grant::CapabilityGrantV1,
    manifest_hash: &str,
    accepted_snapshot_hash: &str,
    reservation_id: &str,
    deadline_unix: u64,
    now_unix: u64,
) -> Result<DispatchPermitV1, PermitDeny> {
    let raw = RawIntent::new(node.immutable_assignment_hash.clone(), "governed spawn")?;
    let bound = bind_identity(raw, node, 1)?;
    let resolved = resolve_policy(bound, grant, 1, now_unix)?;
    let admitted = admit_context(resolved, manifest_hash, accepted_snapshot_hash)?;
    let reserved = reserve_budget(admitted, reservation_id, deadline_unix, now_unix)?;
    mint_dispatch_permit(
        reserved,
        "sha256:spawn-adapter-contract",
        PermitConsumer::SpawnAdapter,
        now_unix,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::capability_grant::{
        CapabilityGrantState, GrantCapabilityClass, IssueGrantRequest,
    };
    use crate::identity_envelope::issue_node_identity;

    fn sample_node() -> NodeIdentityV1 {
        issue_node_identity(
            "tree-1",
            "node-1",
            "sess-root",
            None,
            vec!["node-1".into()],
            "sha256:assignment",
        )
        .expect("node")
    }

    fn sample_grant(ttl_secs: u64) -> CapabilityGrantV1 {
        CapabilityGrantV1::issue(IssueGrantRequest {
            grant_id: "grant-1".into(),
            issuer_root_session_id: "sess-root".into(),
            target_node_id: "node-1".into(),
            task_tree_id: "tree-1".into(),
            capabilities: vec![GrantCapabilityClass::ReadOnly],
            resource_scope_roots: vec!["/work".into()],
            issued_at_unix: 1_000,
            ttl_secs,
            reason: "gate".into(),
            approval_ref: "appr-1".into(),
            revoke_token: "tok-1".into(),
            parent: None,
        })
        .expect("grant")
    }

    fn run_chain(now_unix: u64) -> Result<DispatchPermitV1, PermitDeny> {
        let node = sample_node();
        let grant = sample_grant(1_999_999_000); // issued 1000 + ttl → expires 2_000_000_000
        let raw = RawIntent::new("sha256:assignment", "objective-1")?;
        let bound = bind_identity(raw, &node, 1)?;
        let resolved = resolve_policy(bound, &grant, 1, now_unix)?;
        let admitted = admit_context(resolved, "sha256:manifest", "sha256:snapshot")?;
        let reserved = reserve_budget(admitted, "res-1", 2_000_000_000, now_unix)?;
        mint_dispatch_permit(reserved, "sha256:adapter-contract", PermitConsumer::SpawnAdapter, now_unix)
    }

    #[test]
    fn full_chain_mints_and_authorizes_positive() {
        let permit = run_chain(500).expect("chain");
        permit
            .authorize(PermitConsumer::SpawnAdapter, 500)
            .expect("authorize");
        let binding = permit.binding();
        assert_eq!(binding.grant_revision, 1);
        assert_eq!(binding.manifest_hash, "sha256:manifest");
        assert_eq!(binding.budget_reservation_id, "res-1");
        assert_eq!(binding.consumer, "spawn");
        assert_eq!(binding.policy_revision, 1);
        assert!(!permit.nonce().is_empty());
    }

    #[test]
    fn raw_intent_rejects_empty_assignment_and_objective() {
        assert_eq!(
            RawIntent::new("", "obj").unwrap_err(),
            PermitDeny::EmptyField("assignment_hash")
        );
        assert_eq!(
            RawIntent::new("sha256:assignment", "").unwrap_err(),
            PermitDeny::EmptyField("objective_ref")
        );
        assert_eq!(
            RawIntent::new("plain", "obj").unwrap_err(),
            PermitDeny::NotSha256("assignment_hash")
        );
    }

    #[test]
    fn bind_rejects_foreign_assignment() {
        let node = sample_node();
        let raw = RawIntent::new("sha256:other", "obj").expect("raw");
        assert_eq!(
            bind_identity(raw, &node, 1).unwrap_err(),
            PermitDeny::IdentityMismatch
        );
        let raw = RawIntent::new("sha256:assignment", "obj").expect("raw");
        assert_eq!(
            bind_identity(raw, &node, 0).unwrap_err(),
            PermitDeny::StaleGrantRevision
        );
    }

    #[test]
    fn resolve_rejects_revoked_expired_and_foreign_grant() {
        let node = sample_node();
        let raw = RawIntent::new("sha256:assignment", "obj").expect("raw");
        let bound = bind_identity(raw, &node, 1).expect("bound");
        let mut revoked = sample_grant(1_999_999_000);
        revoked.state = CapabilityGrantState::Revoked;
        assert_eq!(
            resolve_policy(bound.clone(), &revoked, 1, 500).unwrap_err(),
            PermitDeny::GrantRevoked
        );
        let mut foreign_node = sample_grant(1_999_999_000);
        foreign_node.target_node_id = "node-9".into();
        assert_eq!(
            resolve_policy(bound.clone(), &foreign_node, 1, 500).unwrap_err(),
            PermitDeny::ForeignNode
        );
        let mut foreign_tree = sample_grant(1_999_999_000);
        foreign_tree.task_tree_id = "tree-9".into();
        assert_eq!(
            resolve_policy(bound.clone(), &foreign_tree, 1, 500).unwrap_err(),
            PermitDeny::ForeignTree
        );
        // Issued at 1000 with ttl=1 → expires at 1001; now=5000 → expired.
        let expired = sample_grant(1);
        assert_eq!(
            resolve_policy(bound, &expired, 1, 5_000).unwrap_err(),
            PermitDeny::GrantExpired
        );
    }

    #[test]
    fn admit_rejects_missing_manifest_or_snapshot() {
        let node = sample_node();
        let grant = sample_grant(2_000_000_000);
        let raw = RawIntent::new("sha256:assignment", "obj").expect("raw");
        let bound = bind_identity(raw, &node, 1).expect("bound");
        let resolved = resolve_policy(bound, &grant, 1, 500).expect("resolved");
        assert_eq!(
            admit_context(resolved.clone(), "", "sha256:snapshot").unwrap_err(),
            PermitDeny::EmptyField("manifest_hash")
        );
        assert_eq!(
            admit_context(resolved, "sha256:manifest", "plain").unwrap_err(),
            PermitDeny::NotSha256("accepted_snapshot_hash")
        );
    }

    #[test]
    fn reserve_rejects_empty_reservation_and_expired_budget() {
        let node = sample_node();
        let grant = sample_grant(2_000_000_000);
        let raw = RawIntent::new("sha256:assignment", "obj").expect("raw");
        let bound = bind_identity(raw, &node, 1).expect("bound");
        let resolved = resolve_policy(bound, &grant, 1, 500).expect("resolved");
        let admitted = admit_context(resolved, "sha256:manifest", "sha256:snapshot").expect("adm");
        assert_eq!(
            reserve_budget(admitted.clone(), "", 2_000_000_000, 500).unwrap_err(),
            PermitDeny::EmptyField("reservation_id")
        );
        assert_eq!(
            reserve_budget(admitted, "res-1", 400, 500).unwrap_err(),
            PermitDeny::BudgetExpired
        );
    }

    #[test]
    fn mint_rejects_missing_adapter_contract() {
        let node = sample_node();
        let grant = sample_grant(2_000_000_000);
        let raw = RawIntent::new("sha256:assignment", "obj").expect("raw");
        let bound = bind_identity(raw, &node, 1).expect("bound");
        let resolved = resolve_policy(bound, &grant, 1, 500).expect("resolved");
        let admitted = admit_context(resolved, "sha256:manifest", "sha256:snapshot").expect("adm");
        let reserved = reserve_budget(admitted, "res-1", 2_000_000_000, 500).expect("res");
        assert_eq!(
            mint_dispatch_permit(reserved, "", PermitConsumer::SpawnAdapter, 500).unwrap_err(),
            PermitDeny::EmptyField("adapter_contract_hash")
        );
    }

    #[test]
    fn permit_authorize_rejects_wrong_consumer_and_expiry() {
        let permit = run_chain(500).expect("chain");
        assert_eq!(
            permit
                .authorize(PermitConsumer::TerminalAdapter, 500)
                .unwrap_err(),
            PermitDeny::ConsumerMismatch
        );
        assert_eq!(
            permit.authorize(PermitConsumer::SpawnAdapter, 3_000_000_000).unwrap_err(),
            PermitDeny::Expired
        );
    }

    #[test]
    fn permit_revoke_then_authorize_denies() {
        let mut permit = run_chain(500).expect("chain");
        permit.revoke();
        assert_eq!(
            permit.authorize(PermitConsumer::SpawnAdapter, 500).unwrap_err(),
            PermitDeny::Revoked
        );
    }

    #[test]
    fn mint_governed_spawn_permit_full_chain() {
        use crate::capability_grant::{GrantCapabilityClass, IssueGrantRequest};
        use crate::identity_envelope::issue_node_identity;
        let node = issue_node_identity(
            "tree-1",
            "child-1",
            "sess-root",
            Some("root".into()),
            vec!["root".into(), "child-1".into()],
            "sha256:assignment",
        )
        .expect("node");
        let grant = crate::capability_grant::CapabilityGrantV1::issue(IssueGrantRequest {
            grant_id: "grant-1".into(),
            issuer_root_session_id: "sess-root".into(),
            target_node_id: "child-1".into(),
            task_tree_id: "tree-1".into(),
            capabilities: vec![GrantCapabilityClass::ReadOnly],
            resource_scope_roots: vec!["/work".into()],
            issued_at_unix: 1_000,
            ttl_secs: 86_400,
            reason: "spawn".into(),
            approval_ref: "appr-1".into(),
            revoke_token: "tok-1".into(),
            parent: None,
        })
        .expect("grant");
        let permit = mint_governed_spawn_permit(
            &node,
            &grant,
            "sha256:manifest",
            "sha256:snapshot",
            "spawn:child-1",
            2_000_000_000,
            500,
        )
        .expect("permit");
        permit
            .authorize(PermitConsumer::SpawnAdapter, 500)
            .expect("authorize");
        assert_eq!(permit.binding().budget_reservation_id, "spawn:child-1");
        assert_eq!(permit.binding().manifest_hash, "sha256:manifest");
        // A grant for a different node cannot resolve policy for this spawn.
        let mut foreign_grant = grant.clone();
        foreign_grant.target_node_id = "other-node".into();
        let err = mint_governed_spawn_permit(
            &node,
            &foreign_grant,
            "sha256:manifest",
            "sha256:snapshot",
            "spawn:child-1",
            2_000_000_000,
            500,
        )
        .unwrap_err();
        assert_eq!(err, PermitDeny::ForeignNode);
        // Missing manifest fails closed.
        let err = mint_governed_spawn_permit(
            &node,
            &grant,
            "",
            "sha256:snapshot",
            "spawn:child-1",
            2_000_000_000,
            500,
        )
        .unwrap_err();
        assert_eq!(err, PermitDeny::EmptyField("manifest_hash"));
    }

    #[test]
    fn binding_snapshot_roundtrip_serde() {
        let permit = run_chain(500).expect("chain");
        let binding = permit.binding();
        let json = serde_json::to_string(&binding).expect("ser");
        let back: PermitBinding = serde_json::from_str(&json).expect("de");
        assert_eq!(back, binding);
    }
}
