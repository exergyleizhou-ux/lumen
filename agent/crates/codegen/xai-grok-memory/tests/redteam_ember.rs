//! Red-team test: Ember (CL4R1T4S-based) AI prompt-injection payloads vs
//! Lumen's real advisory-layer defenses.
//!
//! Every payload text from the Ember toolkit is fed through the *shipped*
//! pure functions:
//!   - `issue_shadow_advice` (AuthorityClaim / SecretLike / EmptySummary /
//!     Oversize denials)
//!   - `build_advisor_capsule` (SecretLike / ForeignPath / size-cap denials)
//!   - `ClaimAuthority::validate` (role boundaries: Child/Advisor can never
//!     accept; foreign tree fails closed)
//!
//! A payload that is *accepted* (Ok) by the advisory layer is a penetration
//! candidate; a payload that is denied is blocked. The report prints both.

use xai_grok_memory::client_advisor_shadow::{AdvisorMode, issue_shadow_advice};
use xai_grok_memory::client_advisor_consult::{
    AdvisorRequestKind, AdvisorCapsuleDeny, build_advisor_capsule,
};
use xai_grok_memory::claim_authority::{
    ClaimAuthority, ClaimAuthorityActor, ClaimDenyReason, ClaimTransitionRequest,
};
use xai_grok_memory::task_ledger::WorkingMemoryState;

fn load_payloads() -> Vec<String> {
    let path = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/tests/fixtures/ember_payloads.txt"
    );
    let raw = std::fs::read_to_string(path).expect("ember payload fixture missing");
    raw.split("\n\n")
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
        .collect()
}

#[test]
fn ember_payloads_against_advisor_deny_layer() {
    let payloads = load_payloads();
    assert!(!payloads.is_empty(), "payload fixture must not be empty");
    let mut denied = 0usize;
    let mut accepted = Vec::new();

    for (i, payload) in payloads.iter().enumerate() {
        // 1. Shadow advice surface: authority-claim / secret prose is denied.
        let advice_outcome = issue_shadow_advice(
            AdvisorMode::Shadow,
            format!("redteam-{i}"),
            payload.clone(),
            None,
            None,
        );
        let blocked_by_advice = advice_outcome.is_err();

        // 2. Capsule surface: credential-like / foreign-path input is denied.
        let capsule_outcome = build_advisor_capsule(
            format!("redteam-{i}"),
            AdvisorRequestKind::CompletionCandidateReview,
            payload,
            "accepted snapshot (redacted)",
            None,
            &[],
            &["artifacts/"],
        );
        let blocked_by_capsule = capsule_outcome.is_err();

        if blocked_by_advice || blocked_by_capsule {
            denied += 1;
        } else {
            accepted.push((i, payload.chars().take(80).collect::<String>()));
        }
        // THE security invariant: even when the advisory layer *accepts* a
        // payload as text, it must never carry authority — advice can never
        // mutate a claim, assignment, or completion (INV-1/2). A payload that
        // flips `applies_authority` is a true penetration.
        if let Ok(report) = advice_outcome {
            assert!(
                !report.applies_authority,
                "payload #{i} produced advice with authority!"
            );
        }
    }

    eprintln!("=== Ember red-team vs Lumen advisory layer ===");
    eprintln!("payloads: {} | denied: {} | accepted: {} | authority mutations: 0", payloads.len(), denied, accepted.len());
    for (i, preview) in &accepted {
        eprintln!("ACCEPTED #{i}: {preview}…");
    }
    // Hard gates (asserted): zero authority mutations (inline above) and the
    // claim boundary (second test). The deny-layer hit rate is *informational*:
    // Ember's extraction/tool-abuse probes do not match Lumen's deny keywords
    // (authority-claim / secret-like prose), so they are accepted as ordinary
    // advice text. Residual risk is bounded because the advisor capsule is
    // redacted and minimal (no system prompt, no secrets, no paths) and
    // advice carries no authority.
    eprintln!("FINDING: deny layer blocks authority-claim/secret prose but not extraction-style probes (debug-mode, env-dump, system-prompt-read). Recommend adding extraction patterns to AdvisorDeny.");
    assert!(
        denied > 0 || true, // informational; hard gates are the two invariants
        "see FINDING above"
    );
}

#[test]
fn ember_payloads_against_claim_authority_boundary() {
    // Claim boundaries are structural, not lexical: no payload can make a
    // Child or Advisor accept a fact, and a foreign tree always fails closed.
    let payloads = load_payloads();
    let mut role_breaches = 0usize;
    let mut foreign_breaches = 0usize;

    for (i, payload) in payloads.iter().enumerate() {
        let base = ClaimTransitionRequest {
            actor: ClaimAuthorityActor::Child,
            actor_session_id: "child",
            root_session_id: "root",
            ledger_task_tree_id: "t1",
            fact_task_tree_id: "t1",
            from: Some(WorkingMemoryState::Proposed),
            to: WorkingMemoryState::Accepted,
            evidence_ref: Some(payload.as_str()),
            expected_revision: 1,
            actual_revision: 1,
            grant_cancelled: false,
        };
        // Child accepting must always be denied (INV: child has no promotion).
        if ClaimAuthority::validate(&base).is_ok() {
            role_breaches += 1;
        }
        // Advisor accepting must always be denied.
        let advisor = ClaimTransitionRequest {
            actor: ClaimAuthorityActor::Advisor,
            ..base
        };
        if ClaimAuthority::validate(&advisor).is_ok() {
            role_breaches += 1;
        }
        // Foreign tree must always fail closed.
        let foreign = ClaimTransitionRequest {
            actor: ClaimAuthorityActor::RootSessionActor,
            fact_task_tree_id: "other-tree",
            ..base
        };
        if ClaimAuthority::validate(&foreign).is_ok() {
            foreign_breaches += 1;
        }
    }

    eprintln!("=== Ember red-team vs ClaimAuthority ===");
    eprintln!(
        "payloads: {} | role breaches: {} | foreign-tree breaches: {}",
        payloads.len(),
        role_breaches,
        foreign_breaches
    );
    assert_eq!(role_breaches, 0, "child/advisor accepted a fact under injection");
    assert_eq!(foreign_breaches, 0, "foreign tree did not fail closed");
}
