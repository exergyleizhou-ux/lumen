//! NG-07 recommendation projection (DEBT-024(b), A9 recommend surface).
//!
//! The authoritative gate is [`authorize_applied_assignment_chain`]; this
//! module derives a per-condition readiness matrix from the SAME inputs so a
//! UI can recommend/explain what must hold before an assignment may become
//! `Applied`. Every check is required; `recommended` is true only when all
//! checks pass and the authoritative gate agrees. This is a projection, never
//! an authority: it cannot change claim/assignment state.

use serde::{Deserialize, Serialize};

use crate::bounded_assignment_apply::AssignmentLifecycle;
use crate::nextgen_exit_gates::{
    AppliedAssignmentChain, authorize_applied_assignment_chain,
};

/// One condition of the applied-assignment chain, with its readiness state.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct RecommendationCheck {
    pub name: String,
    pub met: bool,
    /// Every chain condition is required; `false` would mean advisory-only.
    pub required: bool,
}

/// UI-ready recommendation for one assignment.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AssignmentRecommendationV1 {
    pub assignment_hash: String,
    pub lifecycle: String,
    pub checks: Vec<RecommendationCheck>,
    /// Overall gate code from the authoritative chain (`applied.ready` when
    /// every condition is met).
    pub gate_code: String,
    /// True only when every required check is met AND the authoritative gate
    /// returned Applied.
    pub recommended: bool,
}

fn check(name: &'static str, met: bool) -> RecommendationCheck {
    RecommendationCheck {
        name: name.to_string(),
        met,
        required: true,
    }
}

/// Derive the readiness matrix from the same inputs the authoritative chain
/// consumes. Conditions are evaluated independently so a UI can show ALL
/// unmet conditions (not just the first deny).
pub fn build_assignment_recommendation(chain: &AppliedAssignmentChain<'_>) -> AssignmentRecommendationV1 {
    let checks = vec![
        check("root_approval", !chain.root_approval_id.trim().is_empty()),
        check("sealed_receipt", !chain.sealed_receipt_id.trim().is_empty()),
        check(
            "tree_budget_reservation",
            !chain.tree_budget_reservation_id.trim().is_empty(),
        ),
        check(
            "context_manifest",
            !chain.context_manifest_hash.trim().is_empty(),
        ),
        check("model_receipt", !chain.model_receipt_id.trim().is_empty()),
        check(
            "assignment_identity",
            !chain.assignment_hash.trim().is_empty()
                && chain.assignment_hash == chain.expected_assignment_hash,
        ),
        check(
            "accepted_snapshot",
            !chain.accepted_snapshot_hash.trim().is_empty()
                && chain.accepted_snapshot_hash == chain.live_snapshot_hash,
        ),
        check("budget_held", chain.budget_reservation_held),
        check("ledger_decision", chain.ledger_decision == "applied"),
    ];
    let gate = authorize_applied_assignment_chain(chain);
    let recommended = matches!(gate, Ok(AssignmentLifecycle::Applied));
    let gate_code = match gate {
        Ok(_) => "applied.ready".to_string(),
        Err(deny) => deny.code(),
    };
    AssignmentRecommendationV1 {
        assignment_hash: chain.assignment_hash.to_string(),
        lifecycle: match chain.lifecycle {
            AssignmentLifecycle::Draft => "draft",
            AssignmentLifecycle::RootApproved => "root_approved",
            AssignmentLifecycle::Applied => "applied",
            AssignmentLifecycle::Rejected => "rejected",
            AssignmentLifecycle::Superseded => "superseded",
        }
        .to_string(),
        checks,
        gate_code,
        recommended,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::nextgen_exit_gates::AppliedChainDeny;

    fn ok_chain<'a>() -> AppliedAssignmentChain<'a> {
        AppliedAssignmentChain {
            lifecycle: AssignmentLifecycle::RootApproved,
            assignment_hash: "sha256:a",
            expected_assignment_hash: "sha256:a",
            accepted_snapshot_hash: "sha256:s",
            live_snapshot_hash: "sha256:s",
            budget_reservation_held: true,
            root_approval_id: "appr-1",
            sealed_receipt_id: "seal-1",
            tree_budget_reservation_id: "tb-1",
            context_manifest_hash: "sha256:m",
            model_receipt_id: "model-1",
            ledger_decision: "applied",
        }
    }

    #[test]
    fn recommendation_reports_all_conditions_met() {
        let recommendation = build_assignment_recommendation(&ok_chain());
        assert!(recommendation.recommended);
        assert_eq!(recommendation.gate_code, "applied.ready");
        assert!(
            recommendation.checks.iter().all(|c| c.met && c.required),
            "every chain condition is required and met"
        );
        assert_eq!(recommendation.lifecycle, "root_approved");
    }

    #[test]
    fn recommendation_lists_every_unmet_condition() {
        let mut chain = ok_chain();
        chain.root_approval_id = "";
        chain.sealed_receipt_id = "";
        chain.live_snapshot_hash = "sha256:other";
        chain.ledger_decision = "pending";
        let recommendation = build_assignment_recommendation(&chain);
        assert!(!recommendation.recommended);
        let by_name = |name: &str| {
            recommendation
                .checks
                .iter()
                .find(|c| c.name == name)
                .expect("check present")
        };
        assert!(!by_name("root_approval").met);
        assert!(!by_name("sealed_receipt").met);
        assert!(!by_name("accepted_snapshot").met);
        assert!(!by_name("ledger_decision").met);
        // Conditions that still hold stay met (independent evaluation).
        assert!(by_name("tree_budget_reservation").met);
        assert!(by_name("model_receipt").met);
        assert_eq!(recommendation.gate_code, "applied.missing_root_approval");
        // A stale snapshot alone surfaces its own code through the apply gate.
        let mut chain = ok_chain();
        chain.live_snapshot_hash = "sha256:other";
        let recommendation = build_assignment_recommendation(&chain);
        assert!(!recommendation.recommended);
        assert_eq!(recommendation.gate_code, "assignment.snapshot_stale");
        assert!(
            !recommendation
                .checks
                .iter()
                .find(|c| c.name == "accepted_snapshot")
                .expect("check")
                .met
        );
    }

    #[test]
    fn recommendation_deny_codes_match_authoritative_gate() {
        // Root approval missing -> the chain's own deny code is surfaced.
        let mut chain = ok_chain();
        chain.root_approval_id = "";
        let recommendation = build_assignment_recommendation(&chain);
        assert_eq!(
            recommendation.gate_code,
            AppliedChainDeny::MissingRootApproval.code()
        );
        // Ledger mismatch -> ledger code.
        let mut chain = ok_chain();
        chain.ledger_decision = "rejected";
        let recommendation = build_assignment_recommendation(&chain);
        assert_eq!(recommendation.gate_code, AppliedChainDeny::LedgerMismatch.code());
    }
}
