//! ClaimDependencyIndex — master plan §3.1.3.
//!
//! The actor maintains a journal-rebuildable index from a claim to the
//! snapshot/manifest it lives in and the live nodes/attempts/operations that
//! consume it, together with `derived_from` causal edges. When a claim is
//! revoked the index decides each consumer's disposition **without freezing
//! the whole tree** and **without missing indirect consumers**:
//!
//! - undispatched node → re-admission;
//! - pure read node → cancel to a safe checkpoint and rebase;
//! - write / external-effect node → block new dispatch immediately and enter
//!   `RecoveryRequired` / `NeedsParentDecision` / `Frozen`;
//! - nodes that do not depend on the claim (directly or via `derived_from`)
//!   keep running.

use std::collections::HashMap;

pub const CLAIM_DEPENDENCY_INDEX_SCHEMA_VERSION: u16 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ConsumerKind {
    /// Node created but not yet dispatched.
    Undispatched,
    /// Pure read-only node.
    ReadOnly,
    /// Node that writes under a scope.
    Write,
    /// Node that produces external effects.
    ExternalEffect,
}

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct ConsumerNode {
    pub node_id: String,
    pub kind: ConsumerKind,
    /// Operation/attempt id when the consumer is mid-flight.
    pub operation_id: Option<String>,
}

/// Where a blocked write/effect consumer must land (master plan §3.1.3).
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum BlockedState {
    RecoveryRequired,
    NeedsParentDecision,
    Frozen,
}

/// Per-consumer disposition after a revocation.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RevocationDisposition {
    /// Undispatched node: re-admit on the next admission pass.
    ReAdmission,
    /// Pure read node: cancel to a safe checkpoint and rebase.
    CancelAndRebase,
    /// Write/effect node: block new dispatch immediately.
    BlockDispatch { state: BlockedState },
    /// Node unaffected by this revocation — keep running.
    Unaffected,
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct ClaimDependencyIndex {
    claim_snapshot: HashMap<String, String>,
    claim_manifest: HashMap<String, String>,
    claim_consumers: HashMap<String, Vec<ConsumerNode>>,
    /// (claim, source_claim) causal edges.
    derived_from_edges: Vec<(String, String)>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum IndexDeny {
    Invalid(String),
    EmptyField(&'static str),
    DanglingDerivedFrom,
    ForeignClaim,
    AlreadyRegistered,
}

impl IndexDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "claim_index.invalid",
            Self::EmptyField(_) => "claim_index.empty_field",
            Self::DanglingDerivedFrom => "claim_index.dangling_derived_from",
            Self::ForeignClaim => "claim_index.foreign_claim",
            Self::AlreadyRegistered => "claim_index.already_registered",
        }
    }
}

impl std::fmt::Display for IndexDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

fn require_non_empty(field: &'static str, value: &str) -> Result<(), IndexDeny> {
    if value.trim().is_empty() {
        return Err(IndexDeny::EmptyField(field));
    }
    Ok(())
}

impl ClaimDependencyIndex {
    pub fn new() -> Self {
        Self::default()
    }

    /// Register a claim's snapshot + manifest binding.
    pub fn record_claim(
        &mut self,
        claim_id: impl Into<String>,
        snapshot_id: impl Into<String>,
        manifest_id: impl Into<String>,
    ) -> Result<(), IndexDeny> {
        let claim_id = claim_id.into();
        let snapshot_id = snapshot_id.into();
        let manifest_id = manifest_id.into();
        require_non_empty("claim_id", &claim_id)?;
        require_non_empty("snapshot_id", &snapshot_id)?;
        require_non_empty("manifest_id", &manifest_id)?;
        if self.claim_snapshot.contains_key(&claim_id) {
            return Err(IndexDeny::AlreadyRegistered);
        }
        self.claim_snapshot.insert(claim_id.clone(), snapshot_id);
        self.claim_manifest.insert(claim_id, manifest_id);
        Ok(())
    }

    /// Register a `derived_from` causal edge. The source must already exist in
    /// the same tree index — a dangling source would make revocation
    /// propagation undefined (INV-25).
    pub fn record_derived_from(
        &mut self,
        claim_id: impl Into<String>,
        source_claim_id: impl Into<String>,
    ) -> Result<(), IndexDeny> {
        let claim_id = claim_id.into();
        let source_claim_id = source_claim_id.into();
        require_non_empty("claim_id", &claim_id)?;
        require_non_empty("source_claim_id", &source_claim_id)?;
        if !self.claim_snapshot.contains_key(&source_claim_id) {
            return Err(IndexDeny::DanglingDerivedFrom);
        }
        self.derived_from_edges.push((claim_id, source_claim_id));
        Ok(())
    }

    /// Register a live consumer of a claim.
    pub fn register_consumer(
        &mut self,
        claim_id: impl Into<String>,
        consumer: ConsumerNode,
    ) -> Result<(), IndexDeny> {
        let claim_id = claim_id.into();
        require_non_empty("claim_id", &claim_id)?;
        require_non_empty("node_id", &consumer.node_id)?;
        if !self.claim_snapshot.contains_key(&claim_id) {
            return Err(IndexDeny::ForeignClaim);
        }
        self.claim_consumers.entry(claim_id).or_default().push(consumer);
        Ok(())
    }

    /// All claims that transitively depend on `claim_id` through
    /// `derived_from` (the claim itself first).
    pub fn dependent_claims(&self, claim_id: &str) -> Vec<String> {
        let mut result = vec![claim_id.to_string()];
        let mut changed = true;
        while changed {
            changed = false;
            for (claim, source) in &self.derived_from_edges {
                if result.contains(source) && !result.contains(claim) {
                    result.push(claim.clone());
                    changed = true;
                }
            }
        }
        result
    }

    /// Every consumer affected by revoking `claim_id` — direct consumers of
    /// the claim plus consumers of any transitively derived claim (indirect
    /// consumers are never missed).
    pub fn affected_consumers(&self, claim_id: &str) -> Vec<ConsumerNode> {
        let mut seen = Vec::<String>::new();
        let mut result = Vec::new();
        for dependent in self.dependent_claims(claim_id) {
            if let Some(consumers) = self.claim_consumers.get(&dependent) {
                for consumer in consumers {
                    if !seen.contains(&consumer.node_id) {
                        seen.push(consumer.node_id.clone());
                        result.push(consumer.clone());
                    }
                }
            }
        }
        result
    }

    /// Disposition for one consumer when `claim_id` is revoked. Nodes that
    /// depend on the claim only through unrelated paths are Unaffected.
    pub fn disposition_for(&self, claim_id: &str, consumer: &ConsumerNode) -> RevocationDisposition {
        let affected = self.affected_consumers(claim_id);
        if !affected.iter().any(|c| c.node_id == consumer.node_id) {
            return RevocationDisposition::Unaffected;
        }
        match consumer.kind {
            ConsumerKind::Undispatched => RevocationDisposition::ReAdmission,
            ConsumerKind::ReadOnly => RevocationDisposition::CancelAndRebase,
            ConsumerKind::Write => RevocationDisposition::BlockDispatch {
                state: BlockedState::Frozen,
            },
            ConsumerKind::ExternalEffect => RevocationDisposition::BlockDispatch {
                state: BlockedState::Frozen,
            },
        }
    }

    /// Full revocation analysis for `claim_id`: every affected consumer with
    /// its disposition.
    pub fn analyze_revocation(
        &self,
        claim_id: &str,
    ) -> Vec<(ConsumerNode, RevocationDisposition)> {
        self.affected_consumers(claim_id)
            .into_iter()
            .map(|consumer| {
                let disposition = self.disposition_for(claim_id, &consumer);
                (consumer, disposition)
            })
            .collect()
    }

    /// Kernel cascade (DEBT-028 W2a-1): run the revocation to its fixed
    /// point and report the quarantine scope.
    ///
    /// Termination is structural: each round only ever ADDS claims to the
    /// affected set (monotone over the finite claim universe), so the loop
    /// reaches a fixed point in at most `|claims| + 1` iterations. The
    /// quarantine scope is exactly the affected set's partition — a bad
    /// record freezes its partition, never the whole product.
    pub fn run_cascade(&self, revocation: &str) -> CascadeOutcome {
        // Round-based closure until fixed point (monotone growth ⇒ bounded).
        let mut current: Vec<String> = vec![revocation.to_string()];
        let mut iterations: u32 = 0;
        loop {
            iterations += 1;
            let mut next = current.clone();
            for (claim, source) in &self.derived_from_edges {
                if current.contains(source) && !next.contains(claim) {
                    next.push(claim.clone());
                }
            }
            next.sort();
            next.dedup();
            if next == current {
                break;
            }
            current = next;
        }
        // Cycle members (no stable JTMS labeling) are Frozen candidates.
        let mut frozen_members: Vec<String> = Vec::new();
        for member in &current {
            if self.depends_on_self(member) {
                frozen_members.push(member.clone());
            }
        }
        frozen_members.sort();
        frozen_members.dedup();
        // Quarantine partition: snapshots touched by the affected claims.
        let mut quarantined_trees: Vec<String> = current
            .iter()
            .filter_map(|claim| self.claim_snapshot.get(claim).cloned())
            .collect();
        quarantined_trees.sort();
        quarantined_trees.dedup();
        CascadeOutcome {
            iterations,
            affected_claims: current,
            quarantined_trees,
            frozen_members,
        }
    }

    /// Whether `claim_id` can reach itself through forward `derived_from`
    /// edges — the JTMS odd-loop criterion for `Frozen(NoStableLabeling)`.
    fn depends_on_self(&self, claim_id: &str) -> bool {
        let mut horizon: Vec<String> = vec![claim_id.to_string()];
        let seen: Vec<String> = Vec::new();
        let mut changed = true;
        while changed {
            changed = false;
            for (claim, source) in &self.derived_from_edges {
                if horizon.contains(source) && !seen.contains(claim) && !horizon.contains(claim) {
                    horizon.push(claim.clone());
                    changed = true;
                }
                if horizon.contains(source) && claim == claim_id {
                    return true;
                }
            }
        }
        false
    }
}

/// Fixed-point outcome of a revocation cascade (DEBT-028 W2a-1).
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct CascadeOutcome {
    /// Rounds to the fixed point (bounded by |claims| + 1).
    pub iterations: u32,
    /// The affected claims: the revocation's reachable set.
    pub affected_claims: Vec<String>,
    /// Quarantine partitions (snapshot ids touched by affected claims).
    pub quarantined_trees: Vec<String>,
    /// Cycle members with no stable labeling — Frozen candidates.
    pub frozen_members: Vec<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn index_with_claims() -> ClaimDependencyIndex {
        let mut index = ClaimDependencyIndex::new();
        index
            .record_claim("claim-1", "snapshot-1", "manifest-1")
            .expect("claim-1");
        index
            .record_claim("claim-2", "snapshot-1", "manifest-1")
            .expect("claim-2");
        index
            .record_claim("claim-3", "snapshot-2", "manifest-2")
            .expect("claim-3");
        index
            .record_derived_from("claim-2", "claim-1")
            .expect("derived");
        index
            .record_derived_from("claim-3", "claim-2")
            .expect("derived2");
        index
    }

    fn consumer(node: &str, kind: ConsumerKind) -> ConsumerNode {
        ConsumerNode {
            node_id: node.to_string(),
            kind,
            operation_id: None,
        }
    }

    #[test]
    fn record_and_derive_positive() {
        let index = index_with_claims();
        assert_eq!(index.dependent_claims("claim-1"), vec!["claim-1", "claim-2", "claim-3"]);
        assert_eq!(index.dependent_claims("claim-2"), vec!["claim-2", "claim-3"]);
        assert_eq!(index.dependent_claims("claim-3"), vec!["claim-3"]);
    }

    #[test]
    fn dangling_derived_from_and_duplicate_denied() {
        let mut index = ClaimDependencyIndex::new();
        assert_eq!(
            index.record_derived_from("a", "missing").unwrap_err(),
            IndexDeny::DanglingDerivedFrom
        );
        index.record_claim("a", "s", "m").expect("a");
        assert_eq!(
            index.record_claim("a", "s2", "m2").unwrap_err(),
            IndexDeny::AlreadyRegistered
        );
        assert_eq!(
            index
                .register_consumer("foreign", consumer("n", ConsumerKind::ReadOnly))
                .unwrap_err(),
            IndexDeny::ForeignClaim
        );
    }

    #[test]
    fn revocation_dispositions_follow_consumer_kind() {
        let mut index = index_with_claims();
        index
            .register_consumer("claim-1", consumer("undispatched", ConsumerKind::Undispatched))
            .expect("reg");
        index
            .register_consumer("claim-1", consumer("reader", ConsumerKind::ReadOnly))
            .expect("reg");
        index
            .register_consumer("claim-2", consumer("writer", ConsumerKind::Write))
            .expect("reg"); // indirect via derived_from
        index
            .register_consumer("claim-1", consumer("effector", ConsumerKind::ExternalEffect))
            .expect("reg");

        let analysis = index.analyze_revocation("claim-1");
        assert_eq!(analysis.len(), 4, "indirect consumers must not be missed");
        let by_node = |node: &str| {
            analysis
                .iter()
                .find(|(c, _)| c.node_id == node)
                .map(|(_, d)| d.clone())
                .expect("consumer present")
        };
        assert_eq!(by_node("undispatched"), RevocationDisposition::ReAdmission);
        assert_eq!(by_node("reader"), RevocationDisposition::CancelAndRebase);
        assert_eq!(
            by_node("writer"),
            RevocationDisposition::BlockDispatch {
                state: BlockedState::Frozen
            }
        );
        assert_eq!(
            by_node("effector"),
            RevocationDisposition::BlockDispatch {
                state: BlockedState::Frozen
            }
        );
    }

    #[test]
    fn unrelated_sibling_stays_unaffected() {
        let mut index = index_with_claims();
        // claim-4 is outside claim-1's transitive dependency cone (the
        // fixture's claim-3 IS downstream of claim-1 via derived_from).
        index
            .record_claim("claim-4", "snapshot-4", "manifest-4")
            .expect("claim-4");
        index
            .register_consumer("claim-1", consumer("reader", ConsumerKind::ReadOnly))
            .expect("reg");
        index
            .register_consumer("claim-4", consumer("sibling", ConsumerKind::Write))
            .expect("reg");

        let analysis = index.analyze_revocation("claim-1");
        assert_eq!(analysis.len(), 1, "only the dependent consumer is affected");
        assert_eq!(analysis[0].0.node_id, "reader");
        // The sibling (write, but depends only on claim-4) is Unaffected.
        let sibling = consumer("sibling", ConsumerKind::Write);
        assert_eq!(
            index.disposition_for("claim-1", &sibling),
            RevocationDisposition::Unaffected
        );
    }

    #[test]
    fn revoking_a_leaf_claim_touches_only_its_consumers() {
        let mut index = index_with_claims();
        index
            .register_consumer("claim-2", consumer("reader", ConsumerKind::ReadOnly))
            .expect("reg");
        index
            .register_consumer("claim-3", consumer("other", ConsumerKind::ReadOnly))
            .expect("reg");
        // Revoking claim-3: claim-3's consumers only (claim-2's reader is
        // upstream, not downstream).
        let analysis = index.analyze_revocation("claim-3");
        assert_eq!(analysis.len(), 1);
        assert_eq!(analysis[0].0.node_id, "other");
    }

    #[test]
    fn cascade_reaches_fixed_point_with_bounded_iterations() {
        // base ← d1 ← d2 ← d3 ← d4 : the closure needs 5 rounds (one per
        // level) and must report the exact reachable set.
        let mut index = ClaimDependencyIndex::new();
        for (claim, snapshot) in [
            ("base", "snap-A"),
            ("d1", "snap-A"),
            ("d2", "snap-A"),
            ("d3", "snap-A"),
            ("d4", "snap-A"),
        ] {
            index
                .record_claim(claim, snapshot, "manifest-A")
                .expect("claim");
        }
        index.record_derived_from("d1", "base").expect("d1");
        index.record_derived_from("d2", "d1").expect("d2");
        index.record_derived_from("d3", "d2").expect("d3");
        index.record_derived_from("d4", "d3").expect("d4");
        let outcome = index.run_cascade("base");
        assert_eq!(
            outcome.affected_claims,
            vec!["base", "d1", "d2", "d3", "d4"]
        );
        assert!(
            outcome.iterations <= 6,
            "iterations bounded by |claims| + 1, got {}",
            outcome.iterations
        );
        assert_eq!(outcome.quarantined_trees, vec!["snap-A"]);
        assert!(outcome.frozen_members.is_empty());
    }

    #[test]
    fn cascade_frozen_cycle_members_and_isolates_partitions() {
        // a → b → a is an odd loop (no stable labeling → Frozen members);
        // claim-x lives in another snapshot partition and must stay out of
        // the quarantine.
        let mut index = ClaimDependencyIndex::new();
        index.record_claim("a", "snap-1", "m1").expect("a");
        index.record_claim("b", "snap-1", "m1").expect("b");
        index.record_claim("x", "snap-2", "m2").expect("x");
        index.record_derived_from("a", "b").expect("a<-b");
        index.record_derived_from("b", "a").expect("b<-a");
        let outcome = index.run_cascade("a");
        assert_eq!(outcome.affected_claims, vec!["a", "b"]);
        assert_eq!(
            outcome.frozen_members,
            vec!["a", "b"],
            "odd-loop members are Frozen(NoStableLabeling) candidates"
        );
        assert_eq!(
            outcome.quarantined_trees,
            vec!["snap-1"],
            "a bad record quarantines its partition, never the product"
        );
        assert!(!outcome.quarantined_trees.contains(&"snap-2".to_owned()));
    }
}
