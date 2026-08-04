//! M1 governed-tree projection + governed-profile upgrade surface (ACP seam).
//!
//! `x.ai/governedTree/status` runs the real offline governed-tree preview
//! (`xai_grok_memory::m1_governed_tree_preview::run_m1_governed_tree_preview`
//! — zero provider, zero external side effects) and projects the three-node
//! tree truthfully: node capabilities (spawn/write/network), the typed deny
//! records, and the accepted snapshot hash. The same response carries the
//! runtime profile + one-way upgrade recommendation, so a client that is
//! about to request parallel/subagent/recovery work sees the governed-tree
//! upgrade surface before it happens (master plan §0.1.7 UX acceptance).
//!
//! The handler is a thin projection wrapper: all semantics live in the
//! memory-crate modules and are covered by their own tests; this seam just
//! makes the offline projection reachable from ACP clients.

use agent_client_protocol as acp;
use serde::{Deserialize, Serialize};

use crate::agent::MvpAgent;
use crate::session::ExtMethodResult;

type ExtResult = Result<acp::ExtResponse, acp::Error>;

/// Wire DTO mirroring the memory `TreeNodeProjection` (camelCase for ACP).
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TreeNodeProjectionWire {
    pub node_id: String,
    pub parent_id: Option<String>,
    pub depth: u8,
    pub branch_id: String,
    pub phase: String,
    pub may_spawn: bool,
    pub may_write: bool,
    pub may_network: bool,
}

/// Wire DTO mirroring the memory `DenyRecord`.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DenyRecordWire {
    pub node_id: String,
    pub action: String,
    pub mechanism: String,
    pub code: String,
}

/// Wire response for `x.ai/governedTree/status`.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct GovernedTreeStatusResponse {
    pub fixture_id: String,
    pub nodes: Vec<TreeNodeProjectionWire>,
    pub denies: Vec<DenyRecordWire>,
    pub accepted_snapshot_hash: String,
    /// Default runtime profile for new runs (interactive_single_turn).
    pub default_profile: String,
    /// One-way upgrade surface: the governed profile a client can request.
    pub upgrade_target: String,
    pub upgrade_deny_on_downgrade: String,
}

fn respond<T: Serialize>(result: Result<T, impl std::fmt::Display>) -> ExtResult {
    ExtMethodResult::from_result(result)
        .to_ext_response()
        .map_err(|e| acp::Error::internal_error().data(e.to_string()))
}

/// Run the offline governed-tree preview into a scratch JSONL and project it.
fn governed_tree_status() -> Result<GovernedTreeStatusResponse, String> {
    use xai_grok_memory::m1_governed_tree_preview::run_m1_governed_tree_preview;
    use xai_grok_memory::runtime_profile::RuntimeProfile;

    let scratch = std::env::temp_dir().join(format!(
        "lumen-m1-{}.jsonl",
        std::process::id()
    ));
    let preview = run_m1_governed_tree_preview(&scratch);
    let _ = std::fs::remove_file(&scratch);
    Ok(GovernedTreeStatusResponse {
        fixture_id: preview.fixture_id,
        nodes: preview
            .nodes
            .into_iter()
            .map(|node| TreeNodeProjectionWire {
                node_id: node.node_id,
                parent_id: node.parent_id,
                depth: node.depth,
                branch_id: node.branch_id,
                phase: node.phase,
                may_spawn: node.may_spawn,
                may_write: node.may_write,
                may_network: node.may_network,
            })
            .collect(),
        denies: preview
            .denies
            .into_iter()
            .map(|deny| DenyRecordWire {
                node_id: deny.node_id,
                action: deny.action,
                mechanism: deny.mechanism.as_str().to_string(),
                code: deny.code,
            })
            .collect(),
        accepted_snapshot_hash: preview.accepted_snapshot_hash,
        default_profile: RuntimeProfile::default_profile().as_str().to_string(),
        upgrade_target: RuntimeProfile::GovernedTreeDevelopment.as_str().to_string(),
        upgrade_deny_on_downgrade: "profile.admission_upgrade_failed".to_string(),
    })
}

/// Handle `x.ai/governedTree/*` extension methods.
pub async fn handle(_agent: &MvpAgent, args: &acp::ExtRequest) -> ExtResult {
    match args.method.as_ref() {
        "x.ai/governedTree/status" => {
            respond(governed_tree_status())
        }
        "x.ai/governedTree/assignmentRecommendation" => {
            respond(Ok::<_, String>(assignment_recommendation()))
        }
        _ => Err(acp::Error::method_not_found()),
    }
}

/// NG-07 (A9) recommend surface: the readiness matrix for a governed
/// assignment, derived from the authoritative applied-chain gate. With no
/// live assignment in flight the projection truthfully reports every
/// required condition as unmet — the UI's "what must hold before Applied".
fn assignment_recommendation() -> AssignmentRecommendationWire {
    use xai_grok_memory::bounded_assignment_apply::AssignmentLifecycle;
    use xai_grok_memory::nextgen_exit_gates::AppliedAssignmentChain;
    use xai_grok_memory::recommend_assignment::build_assignment_recommendation;

    let chain = AppliedAssignmentChain {
        lifecycle: AssignmentLifecycle::Draft,
        assignment_hash: "",
        expected_assignment_hash: "",
        accepted_snapshot_hash: "",
        live_snapshot_hash: "",
        budget_reservation_held: false,
        root_approval_id: "",
        sealed_receipt_id: "",
        tree_budget_reservation_id: "",
        context_manifest_hash: "",
        model_receipt_id: "",
        ledger_decision: "",
    };
    let recommendation = build_assignment_recommendation(&chain);
    AssignmentRecommendationWire {
        assignment_hash: recommendation.assignment_hash,
        lifecycle: recommendation.lifecycle,
        checks: recommendation
            .checks
            .into_iter()
            .map(|c| RecommendationCheckWire {
                name: c.name,
                met: c.met,
                required: c.required,
            })
            .collect(),
        gate_code: recommendation.gate_code,
        recommended: recommendation.recommended,
    }
}

/// Wire DTO for `x.ai/governedTree/assignmentRecommendation`.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AssignmentRecommendationWire {
    pub assignment_hash: String,
    pub lifecycle: String,
    pub checks: Vec<RecommendationCheckWire>,
    pub gate_code: String,
    pub recommended: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RecommendationCheckWire {
    pub name: String,
    pub met: bool,
    pub required: bool,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn governed_tree_status_projects_three_node_tree_with_typed_denies() {
        let response = governed_tree_status().expect("offline preview");
        assert_eq!(response.nodes.len(), 3, "root -> child -> leaf");
        let leaf = response
            .nodes
            .iter()
            .find(|node| !node.may_spawn)
            .expect("leaf node cannot spawn");
        assert_eq!(leaf.depth, 3, "leaf sits at HARD_MAX_SUBAGENT_DEPTH");
        assert!(!leaf.may_write, "leaf cannot write");
        assert!(!leaf.may_network, "leaf cannot network");
        // The fixture denies at least the leaf's spawn/write attempts.
        assert!(
            response.denies.iter().any(|deny| deny.node_id == leaf.node_id),
            "leaf must carry typed deny records"
        );
        assert!(
            response
                .denies
                .iter()
                .all(|deny| !deny.code.is_empty()),
            "deny records carry machine-readable codes"
        );
        // The fixture guarantees at least one of each enforcement mechanism.
        for mechanism in [
            "capability_ceiling",
            "tool_filter",
            "sandbox_enforcement",
            "lineage_depth",
        ] {
            assert!(
                response
                    .denies
                    .iter()
                    .any(|deny| deny.mechanism == mechanism),
                "deny set must include {mechanism}"
            );
        }
        assert!(!response.accepted_snapshot_hash.is_empty());
        assert_eq!(response.default_profile, "interactive_single_turn");
        assert_eq!(response.upgrade_target, "governed_tree_development");
    }

    #[test]
    fn assignment_recommendation_projects_required_chain() {
        let recommendation = assignment_recommendation();
        assert!(!recommendation.recommended, "no live assignment in flight");
        assert_eq!(recommendation.lifecycle, "draft");
        // Every required condition is surfaced with its readiness state.
        let names: Vec<&str> = recommendation
            .checks
            .iter()
            .map(|c| c.name.as_str())
            .collect();
        for required in [
            "root_approval",
            "sealed_receipt",
            "tree_budget_reservation",
            "context_manifest",
            "model_receipt",
            "assignment_identity",
            "accepted_snapshot",
            "budget_held",
            "ledger_decision",
        ] {
            assert!(names.contains(&required), "missing check {required}");
        }
        assert!(
            recommendation.checks.iter().all(|c| c.required),
            "every chain condition is required"
        );
        assert_eq!(
            recommendation.gate_code,
            "applied.missing_root_approval",
            "the authoritative chain's first deny (missing root approval) is surfaced"
        );
        // Serializes as the wire contract.
        let json = serde_json::to_string(&recommendation).expect("serde");
        assert!(json.contains("rootApproval") || json.contains("root_approval"));
    }
}
