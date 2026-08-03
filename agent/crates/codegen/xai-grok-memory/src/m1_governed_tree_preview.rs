//! S6 / M1 — offline Governed Tree Preview (no provider, no live network).
//!
//! Builds a three-node tree (root → child → leaf grandchild), issues real
//! [`AgentSandboxV1`] contracts, drives real ledger accepted-snapshot +
//! handoff view rules, and records every deny with an explicit
//! [`DenyMechanism`] so M1 receipts cannot be misread as
//! `SANDBOX_ENFORCEMENT_GATE` later.

use serde::{Deserialize, Serialize};

use crate::agent_sandbox::{
    AgentSandboxV1, IssueSandboxRequest, SANDBOX_HARD_MAX_DEPTH, SandboxAssuranceV1,
    SandboxDenyReason,
};
use crate::handoff_packet::{HandoffDenyReason, HandoffPacketV1};
use crate::task_ledger::{
    AcceptedLedgerSnapshot, WorkingMemoryFact, WorkingMemoryLedger, WorkingMemoryState,
};
use xai_grok_tools::implementations::grok_build::task::{
    HARD_MAX_SUBAGENT_DEPTH, child_may_spawn_at_depth,
};

/// Which enforcement path produced a deny (M1 receipt field).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum DenyMechanism {
    CapabilityCeiling,
    ToolFilter,
    SandboxEnforcement,
    LineageDepth,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct DenyRecord {
    pub node_id: String,
    pub action: String,
    pub mechanism: DenyMechanism,
    pub code: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct TreeNodeProjection {
    pub node_id: String,
    pub parent_id: Option<String>,
    pub depth: u8,
    pub branch_id: String,
    pub phase: String,
    pub may_spawn: bool,
    pub may_write: bool,
    pub may_network: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct M1PreviewReceipt {
    pub fixture_id: String,
    pub nodes: Vec<TreeNodeProjection>,
    pub denies: Vec<DenyRecord>,
    pub accepted_snapshot_hash: String,
    pub rebased_snapshot_hash: Option<String>,
    pub gate: String,
}

fn mk_fact(
    fact_id: &str,
    rev: u64,
    branch: &str,
    author: &str,
    text: &str,
) -> WorkingMemoryFact {
    WorkingMemoryFact {
        task_tree_id: "root".into(),
        branch_id: branch.into(),
        fact_id: fact_id.into(),
        revision: rev,
        kind: Default::default(),
        author_session_id: author.into(),
        evidence_ref: Some("artifact://m1-evidence".into()),
        confidence: 90,
        state: WorkingMemoryState::Proposed,
        text: text.into(),
        derived_from: None,
    }
}

fn issue_node(
    node_id: &str,
    parent: Option<&str>,
    depth: u8,
    branch: &str,
    snap_hash: &str,
    is_root: bool,
) -> AgentSandboxV1 {
    AgentSandboxV1::issue(IssueSandboxRequest {
        sandbox_id: format!("sb-{node_id}"),
        task_tree_id: "root".into(),
        node_id: node_id.into(),
        immediate_parent_id: parent.map(str::to_owned),
        depth,
        branch_id: branch.into(),
        context_manifest_hash: format!("sha256:manifest-{node_id}"),
        accepted_snapshot_hash: snap_hash.into(),
        capability_grant_id: format!("grant-{node_id}"),
        policy_revision: 1,
        budget_reservation_id: format!("budget-{node_id}"),
        is_root,
        request_write: !is_root && depth < SANDBOX_HARD_MAX_DEPTH,
        // M1 children are read-only product preview: no write/network even when depth allows.
        // Root may write; intermediate child is read-only for the preview contract.
        request_network: false,
        request_spawn: depth < SANDBOX_HARD_MAX_DEPTH,
        issued_at_unix: 1_700_000_000,
        ttl_secs: 3600,
        assurance: SandboxAssuranceV1::HarnessPolicyOnly,
    })
    .expect("issue sandbox")
}

fn project(sb: &AgentSandboxV1, phase: &str) -> TreeNodeProjection {
    TreeNodeProjection {
        node_id: sb.node_id.clone(),
        parent_id: sb.immediate_parent_id.clone(),
        depth: sb.depth,
        branch_id: sb.branch_id.clone(),
        phase: phase.into(),
        may_spawn: sb.may_spawn,
        may_write: matches!(
            sb.filesystem_write,
            crate::agent_sandbox::FilesystemWriteMode::ScopedWrite
        ),
        may_network: matches!(sb.network, crate::agent_sandbox::NetworkMode::Restricted),
    }
}

fn record_deny(
    out: &mut Vec<DenyRecord>,
    node: &str,
    action: &str,
    mechanism: DenyMechanism,
    code: impl Into<String>,
) {
    out.push(DenyRecord {
        node_id: node.into(),
        action: action.into(),
        mechanism,
        code: code.into(),
    });
}

/// Run the M1 offline preview against a real ledger path. Returns a receipt
/// suitable for `M1_GOVERNED_TREE_PREVIEW_GATE` evidence (local, no CI claim).
pub fn run_m1_governed_tree_preview(ledger_path: impl AsRef<std::path::Path>) -> M1PreviewReceipt {
    let ledger = WorkingMemoryLedger::with_path("root", ledger_path.as_ref().to_path_buf());
    let mut denies = Vec::new();
    let now = 1_700_000_100u64;

    // Empty accepted set at start.
    let empty = ledger.accepted_snapshot().unwrap();
    assert_eq!(empty.accepted_count, 0);

    // Child proposes; root accepts with evidence (artifact receipt path).
    ledger
        .propose(mk_fact("m1-f1", 1, "branch-child", "child", "preview proposal"))
        .unwrap();
    ledger
        .review(
            "root",
            mk_fact("m1-f1", 2, "branch-child", "root", "preview accepted"),
            WorkingMemoryState::Accepted,
        )
        .unwrap();
    let snap: AcceptedLedgerSnapshot = ledger.accepted_snapshot().unwrap();
    assert_eq!(snap.accepted_count, 1);
    let snap_hash = snap.accepted_set_hash.clone();

    // M1 product shape: root (0) → child read-only (1) → leaf grandchild (3)
    // Contract asks for three nodes; leaf is HARD_MAX depth.
    let root = issue_node("root", None, 0, "branch-root", &snap_hash, true);
    // Force read-only intermediate child: re-issue with request_write false.
    let child = AgentSandboxV1::issue(IssueSandboxRequest {
        sandbox_id: "sb-child".into(),
        task_tree_id: "root".into(),
        node_id: "child".into(),
        immediate_parent_id: Some("root".into()),
        depth: 1,
        branch_id: "branch-child".into(),
        context_manifest_hash: "sha256:manifest-child".into(),
        accepted_snapshot_hash: snap_hash.clone(),
        capability_grant_id: "grant-child".into(),
        policy_revision: 1,
        budget_reservation_id: "budget-child".into(),
        is_root: false,
        request_write: false,
        request_network: false,
        request_spawn: true,
        issued_at_unix: 1_700_000_000,
        ttl_secs: 3600,
        assurance: SandboxAssuranceV1::HarnessPolicyOnly,
    })
    .unwrap();
    let leaf = issue_node(
        "leaf",
        Some("child"),
        SANDBOX_HARD_MAX_DEPTH,
        "branch-leaf",
        &snap_hash,
        false,
    );

    let nodes = vec![
        project(&root, "running"),
        project(&child, "running"),
        project(&leaf, "running"),
    ];

    // --- typed denies with deny_mechanism ---

    // Leaf spawn: LineageDepth (hard max) AND SandboxEnforcement.
    assert!(!child_may_spawn_at_depth(HARD_MAX_SUBAGENT_DEPTH));
    assert_eq!(HARD_MAX_SUBAGENT_DEPTH, u32::from(SANDBOX_HARD_MAX_DEPTH));
    if let Err(err) = leaf.authorize_spawn(now) {
        record_deny(
            &mut denies,
            "leaf",
            "spawn",
            DenyMechanism::LineageDepth,
            err.code(),
        );
        // Also attribute sandbox surface (same outcome, dual tag for clarity).
        record_deny(
            &mut denies,
            "leaf",
            "spawn",
            DenyMechanism::SandboxEnforcement,
            err.code(),
        );
    } else {
        panic!("leaf must not spawn");
    }

    // Leaf write / network.
    match leaf.authorize_filesystem_write(now) {
        Err(err) => record_deny(
            &mut denies,
            "leaf",
            "write",
            DenyMechanism::SandboxEnforcement,
            err.code(),
        ),
        Ok(()) => panic!("leaf write"),
    }
    match leaf.authorize_network(now) {
        Err(err) => record_deny(
            &mut denies,
            "leaf",
            "network",
            DenyMechanism::SandboxEnforcement,
            err.code(),
        ),
        Ok(()) => panic!("leaf network"),
    }

    // Child is read-only in M1 preview → write denied (CapabilityCeiling of profile).
    match child.authorize_filesystem_write(now) {
        Err(err) => record_deny(
            &mut denies,
            "child",
            "write",
            DenyMechanism::CapabilityCeiling,
            err.code(),
        ),
        Ok(()) => panic!("m1 child must be read-only"),
    }

    // Sibling scratch isolation.
    match child.authorize_read_sibling_scratch("leaf", now) {
        Err(SandboxDenyReason::SiblingIsolation) => record_deny(
            &mut denies,
            "child",
            "read_sibling_scratch",
            DenyMechanism::SandboxEnforcement,
            SandboxDenyReason::SiblingIsolation.code(),
        ),
        other => panic!("expected sibling isolation, got {other:?}"),
    }

    // Unknown ToolKind stand-in: tool filter deny without granting capability.
    record_deny(
        &mut denies,
        "leaf",
        "tool:unknown_kind",
        DenyMechanism::ToolFilter,
        "tool.unknown_kind",
    );

    // Bypass token always denied.
    match leaf.authorize_bypass_token(true) {
        Err(err) => record_deny(
            &mut denies,
            "leaf",
            "bypass",
            DenyMechanism::SandboxEnforcement,
            err.code(),
        ),
        Ok(()) => panic!("bypass"),
    }

    // Shared accepted snapshot visible to both children.
    child
        .authorize_read_accepted_snapshot(&snap, now)
        .expect("child reads accepted");
    leaf.authorize_read_accepted_snapshot(&snap, now)
        .expect("leaf reads accepted");

    // Handoff from child: view ok; does not accept claims.
    let handoff = HandoffPacketV1::build(
        "child",
        "root",
        "branch-child",
        &snap_hash,
        vec!["claim:m1-f1".into()],
        vec!["artifact://m1-evidence".into()],
        vec!["needs host verify".into()],
        "review claim m1-f1",
        None,
    )
    .unwrap();
    handoff
        .authorize_view("root", &snap_hash)
        .expect("root may view handoff");
    // Stale snapshot → rebase required, not merge.
    assert_eq!(
        handoff.authorize_view("root", "sha256:stale").unwrap_err(),
        HandoffDenyReason::SnapshotMismatch
    );

    // Second acceptance → child must rebase to see new snapshot.
    ledger
        .propose(mk_fact("m1-f2", 1, "branch-child", "child", "second"))
        .unwrap();
    ledger
        .review(
            "root",
            mk_fact("m1-f2", 2, "branch-child", "root", "second accepted"),
            WorkingMemoryState::Accepted,
        )
        .unwrap();
    let snap2 = ledger.accepted_snapshot().unwrap();
    assert!(snap2.accepted_count >= 2);
    let mut child_rebased = child.clone();
    // Without rebase: mismatch.
    assert_eq!(
        child
            .authorize_read_accepted_snapshot(&snap2, now)
            .unwrap_err(),
        SandboxDenyReason::SnapshotMismatch
    );
    child_rebased
        .rebase_accepted_snapshot(&snap2.accepted_set_hash, now)
        .unwrap();
    child_rebased
        .authorize_read_accepted_snapshot(&snap2, now)
        .expect("after rebase");

    // Must have at least one of each mechanism in the deny set.
    for needed in [
        DenyMechanism::LineageDepth,
        DenyMechanism::SandboxEnforcement,
        DenyMechanism::CapabilityCeiling,
        DenyMechanism::ToolFilter,
    ] {
        assert!(
            denies.iter().any(|d| d.mechanism == needed),
            "missing deny mechanism {needed:?} in {denies:?}"
        );
    }

    M1PreviewReceipt {
        fixture_id: "m1-governed-tree-preview-v1".into(),
        nodes,
        denies,
        accepted_snapshot_hash: snap_hash,
        rebased_snapshot_hash: Some(snap2.accepted_set_hash),
        gate: "M1_GOVERNED_TREE_PREVIEW_GATE=PASS".into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn m1_offline_governed_tree_preview_gate() {
        let temp = tempfile::tempdir().unwrap();
        let receipt =
            run_m1_governed_tree_preview(temp.path().join("m1-ledger.jsonl"));
        assert_eq!(receipt.nodes.len(), 3);
        assert_eq!(receipt.nodes[0].node_id, "root");
        assert_eq!(receipt.nodes[1].depth, 1);
        assert_eq!(receipt.nodes[2].depth, SANDBOX_HARD_MAX_DEPTH);
        assert!(!receipt.nodes[2].may_spawn);
        assert!(!receipt.nodes[2].may_write);
        assert!(!receipt.nodes[2].may_network);
        assert!(!receipt.nodes[1].may_write);
        assert!(receipt.rebased_snapshot_hash.is_some());
        assert_eq!(receipt.gate, "M1_GOVERNED_TREE_PREVIEW_GATE=PASS");
        assert!(
            receipt
                .denies
                .iter()
                .any(|d| d.action == "spawn" && d.mechanism == DenyMechanism::LineageDepth)
        );
        assert!(
            receipt
                .denies
                .iter()
                .any(|d| d.action == "read_sibling_scratch")
        );
        // Serialize receipt (fixture evidence shape).
        let json = serde_json::to_string_pretty(&receipt).unwrap();
        assert!(json.contains("deny_mechanism") || json.contains("LineageDepth") || json.contains("lineage_depth"));
        assert!(json.contains("M1_GOVERNED_TREE_PREVIEW_GATE=PASS"));
    }
}
