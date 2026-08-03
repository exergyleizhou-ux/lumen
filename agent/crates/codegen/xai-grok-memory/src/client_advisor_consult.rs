//! S9 / NG-06A — ClientAdvisor consult contract (pure, offline-safe).
//!
//! The primary model may request a bounded, independent review at checkpoints.
//! This module is the pure contract: request kinds are enumerable, the context
//! capsule is redacted and hashed, the report is explicitly advisory, and the
//! usage receipt is independent of the model receipt. Nothing here performs a
//! provider call; the shell wires a mock adapter for fixtures only.
//!
//! Absolute boundaries (INV-1/2, plan §3.4.3): Advisor has no filesystem/shell/
//! MCP/write scope, no claim acceptance, no bypass, no model switch, no
//! terminal success, no direct child spawn. A timeout or unavailable advisor
//! returns `Blocked`, never a downgraded success.

use serde::{Deserialize, Serialize};

use crate::client_advisor_shadow::{AdvisorMode, AdviceReportV1, AdvisorDeny};

/// Enumerable consult kinds — a free-form prompt is never a legal argument.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AdvisorRequestKind {
    PlanReview,
    EvidenceGapReview,
    FailureConvergenceReview,
    ScopeOrBudgetEscalationReview,
    CompletionCandidateReview,
}

/// Bounded consult request: kind + optional short question + artifact refs.
///
/// Free text cannot modify system policy, model pool, tools, budget, or
/// completion gates; the question is stored as a non-executable artifact.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AdvisorRequestV1 {
    pub request_id: String,
    pub kind: AdvisorRequestKind,
    /// Bounded review question (size-capped by the capsule builder).
    pub review_question: Option<String>,
    /// Allowed artifact references (allowlisted by the capsule builder).
    pub artifact_refs: Vec<String>,
}

const MAX_REVIEW_QUESTION: usize = 4_000;

/// Independent usage receipt for one consult. Unknown usage is recorded as
/// `Unknown`, never as zero.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AdvisorUsageReceiptV1 {
    pub receipt_id: String,
    pub request_id: String,
    pub issued_at_epoch_ms: u64,
    pub mode: AdvisorMode,
    /// User pool entry / provider reference; unknown stays unknown.
    pub model_ref: Option<String>,
    /// Hashes of the capsule inputs (manifest/snapshot/input) — see
    /// [`AdvisorContextCapsuleV1::report_hash`] for the report side.
    pub capsule_hash: String,
    pub token_usage: TokenUsage,
    pub deadline_epoch_ms: Option<u64>,
    /// `None` = not cancelled/timed-out/denied (completed normally).
    pub cancel_or_deny_reason: Option<String>,
    /// Whether root adopted the advice into a decision.
    pub adopted_by_root: bool,
}

/// Unknown usage must be recorded as unknown, never zero.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TokenUsage {
    Unknown,
    Known { input_tokens: u64, output_tokens: u64 },
}

/// Redacted, hashed context capsule handed to the advisor (zero network here).
///
/// The capsule is the *only* context an advisor sees: redacted manifest +
/// accepted-snapshot projection + allowlisted artifact refs. Secrets, foreign
/// paths, proposed facts and oversized inputs are denied by the builder.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AdvisorContextCapsuleV1 {
    pub request_id: String,
    pub kind: AdvisorRequestKind,
    /// Redacted manifest summary (no secrets, no paths).
    pub manifest_summary: String,
    /// Redacted accepted-snapshot summary.
    pub accepted_snapshot_summary: String,
    /// Allowlisted artifact refs (bounded count and per-ref length).
    pub artifact_refs: Vec<String>,
    /// Stable hash over all fields; any input drift changes the hash.
    pub capsule_hash: String,
    /// Size cap in bytes; exceeding it fails the build (fail-closed).
    pub size_cap: usize,
}

const CAPSULE_SIZE_CAP: usize = 16 * 1024;
const MAX_ARTIFACT_REFS: usize = 8;
const MAX_ARTIFACT_REF_LEN: usize = 256;

/// Redaction: strip credential-like material from free text.
pub fn redact_text(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    let mut rest = text;
    const PATTERNS: &[&str] = &[
        "api_key=",
        "api-key=",
        "apikey=",
        "token=",
        "secret=",
        "password=",
        "-----begin ",
        "sk-",
    ];
    while let Some(first) = PATTERNS
        .iter()
        .filter_map(|p| rest.find(p).map(|i| (i, p)))
        .min_by_key(|(i, _)| *i)
    {
        let (idx, pat) = first;
        out.push_str(&rest[..idx]);
        let start = idx + pat.len();
        let end = rest[start..]
            .find(|c: char| c.is_whitespace() || c == '"' || c == '\'')
            .map(|i| start + i)
            .unwrap_or(rest.len());
        out.push_str(&format!("{}<redacted>", pat));
        rest = &rest[end..];
    }
    out.push_str(rest);
    out
}

fn is_secret_like(text: &str) -> bool {
    let lower = text.to_ascii_lowercase();
    lower.contains("api_key=")
        || lower.contains("api-key=")
        || lower.contains("-----begin ")
        || lower.contains("sk-")
}

/// A path reference is allowed only when it is inside the session's allowed
/// artifact root; absolute paths outside it are denied (foreign-path rule).
pub fn is_allowed_artifact_ref(ref_: &str, allowed_prefixes: &[&str]) -> bool {
    if ref_.is_empty() || ref_.len() > MAX_ARTIFACT_REF_LEN {
        return false;
    }
    if ref_.contains("..") {
        return false;
    }
    allowed_prefixes.iter().any(|p| ref_.starts_with(p))
}

/// Build the redacted capsule. Pure, zero network, fail-closed:
/// any denied input fails the whole build with a typed denial.
pub fn build_advisor_capsule(
    request_id: impl Into<String>,
    kind: AdvisorRequestKind,
    raw_manifest: &str,
    raw_accepted_snapshot: &str,
    review_question: Option<&str>,
    artifact_refs: &[String],
    allowed_artifact_prefixes: &[&str],
) -> Result<AdvisorContextCapsuleV1, AdvisorCapsuleDeny> {
    if let Some(q) = review_question {
        if q.trim().is_empty() {
            return Err(AdvisorCapsuleDeny::EmptyQuestion);
        }
        if q.len() > MAX_REVIEW_QUESTION {
            return Err(AdvisorCapsuleDeny::QuestionOversize);
        }
    }
    if artifact_refs.len() > MAX_ARTIFACT_REFS {
        return Err(AdvisorCapsuleDeny::TooManyArtifacts);
    }
    if is_secret_like(raw_manifest) || is_secret_like(raw_accepted_snapshot) {
        return Err(AdvisorCapsuleDeny::SecretLike);
    }
    let mut allowed = Vec::with_capacity(artifact_refs.len());
    for r in artifact_refs {
        if !is_allowed_artifact_ref(r, allowed_artifact_prefixes) {
            return Err(AdvisorCapsuleDeny::ForeignPath(r.clone()));
        }
        allowed.push(r.clone());
    }
    let manifest_summary = redact_text(raw_manifest);
    let snapshot_summary = redact_text(raw_accepted_snapshot);
    let mut capsule = AdvisorContextCapsuleV1 {
        request_id: request_id.into(),
        kind,
        manifest_summary,
        accepted_snapshot_summary: snapshot_summary,
        artifact_refs: allowed,
        capsule_hash: String::new(),
        size_cap: CAPSULE_SIZE_CAP,
    };
    capsule.capsule_hash = capsule.compute_hash();
    if capsule.encoded_len() > CAPSULE_SIZE_CAP {
        return Err(AdvisorCapsuleDeny::Oversize);
    }
    Ok(capsule)
}

/// Denial reasons for capsule building (fail-closed).
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AdvisorCapsuleDeny {
    EmptyQuestion,
    QuestionOversize,
    TooManyArtifacts,
    SecretLike,
    ForeignPath(String),
    Oversize,
}

impl AdvisorCapsuleDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::EmptyQuestion => "advisor_capsule.empty_question",
            Self::QuestionOversize => "advisor_capsule.question_oversize",
            Self::TooManyArtifacts => "advisor_capsule.too_many_artifacts",
            Self::SecretLike => "advisor_capsule.secret_like",
            Self::ForeignPath(_) => "advisor_capsule.foreign_path",
            Self::Oversize => "advisor_capsule.oversize",
        }
    }
}

/// A consult either succeeds with a report or is explicitly blocked. A
/// timeout / unavailable advisor is `Blocked` — never a downgraded success.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConsultOutcome {
    Succeeded {
        report_id: String,
    },
    Blocked {
        reason: ConsultBlockReason,
    },
}

/// Block reasons observable by the actor/UI. `Unavailable` and `TimedOut`
/// never reopen the primary task; a mandatory checkpoint stays blocked.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConsultBlockReason {
    AdvisorUnavailable,
    TimedOut,
    Denied(AdvisorDeny),
    Cancelled,
    PolicyRefused,
}

impl ConsultBlockReason {
    pub fn code(&self) -> &'static str {
        match self {
            Self::AdvisorUnavailable => "advisor.unavailable",
            Self::TimedOut => "advisor.timed_out",
            Self::Denied(d) => d.code(),
            Self::Cancelled => "advisor.cancelled",
            Self::PolicyRefused => "advisor.policy_refused",
        }
    }
}

/// Pure timeout check: a consult is timed out when `now` is at or past the
/// deadline. `started_epoch_ms` keeps the API shape for callers that track
/// elapsed time; the deadline is absolute, so the started stamp is not part
/// of the decision (a deadline always bounds the window).
pub fn consult_timed_out(_started_epoch_ms: u64, deadline_epoch_ms: u64, now_epoch_ms: u64) -> bool {
    now_epoch_ms >= deadline_epoch_ms
}

impl AdvisorContextCapsuleV1 {
    /// Stable hash over all capsule fields (report_hash / drift detection).
    pub fn compute_hash(&self) -> String {
        use std::hash::{DefaultHasher, Hash, Hasher};
        let mut h = DefaultHasher::new();
        self.request_id.hash(&mut h);
        format!("{:?}", self.kind).hash(&mut h);
        self.manifest_summary.hash(&mut h);
        self.accepted_snapshot_summary.hash(&mut h);
        self.artifact_refs.hash(&mut h);
        self.size_cap.hash(&mut h);
        format!("{:016x}", h.finish())
    }

    fn encoded_len(&self) -> usize {
        self.request_id.len()
            + self.manifest_summary.len()
            + self.accepted_snapshot_summary.len()
            + self.artifact_refs.iter().map(|s| s.len()).sum::<usize>()
            + self.capsule_hash.len()
            + 64
    }
}

/// Build the report hash from a capsule + report payload so that any
/// manifest/snapshot/redaction drift fails closed at verification time.
pub fn report_hash(capsule: &AdvisorContextCapsuleV1, report: &AdviceReportV1) -> String {
    use std::hash::{DefaultHasher, Hash, Hasher};
    let mut h = DefaultHasher::new();
    capsule.capsule_hash.hash(&mut h);
    report.advice_id.hash(&mut h);
    report.summary.hash(&mut h);
    format!("{:?}", report.mode).hash(&mut h);
    format!("{:016x}", h.finish())
}

/// Build an independent usage receipt for a consult. Unknown usage stays
/// unknown (never zero); cancel/deny/timeout reasons are recorded.
pub fn build_usage_receipt(
    receipt_id: impl Into<String>,
    request_id: impl Into<String>,
    issued_at_epoch_ms: u64,
    mode: AdvisorMode,
    model_ref: Option<String>,
    capsule_hash: impl Into<String>,
    token_usage: TokenUsage,
    deadline_epoch_ms: Option<u64>,
    cancel_or_deny_reason: Option<String>,
    adopted_by_root: bool,
) -> AdvisorUsageReceiptV1 {
    AdvisorUsageReceiptV1 {
        receipt_id: receipt_id.into(),
        request_id: request_id.into(),
        issued_at_epoch_ms,
        mode,
        model_ref,
        capsule_hash: capsule_hash.into(),
        token_usage,
        deadline_epoch_ms,
        cancel_or_deny_reason,
        adopted_by_root,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::client_advisor_shadow::{issue_shadow_advice, AdvisorMode};

    #[test]
    fn capsule_redacts_secrets_and_denies_foreign_paths() {
        // `password=` is redacted in place (not a hard deny);
        // `sk-` style material is a hard fail-closed deny (see next test).
        let manifest = "model=deepseek-v4-flash password=hunter2 endpoint ok";
        let snap = "accepted fact: tests pass";
        let capsule = build_advisor_capsule(
            "r1",
            AdvisorRequestKind::PlanReview,
            manifest,
            snap,
            Some("is the plan sound?"),
            &["artifacts/r0/ok.md".to_string()],
            &["artifacts/"],
        )
        .unwrap();
        assert!(!capsule.manifest_summary.contains("hunter2"));
        assert!(capsule.manifest_summary.contains("<redacted>"));
        assert_eq!(capsule.capsule_hash.len(), 16);

        let err = build_advisor_capsule(
            "r2",
            AdvisorRequestKind::PlanReview,
            "ok",
            "ok",
            None,
            &["/etc/passwd".to_string()],
            &["artifacts/"],
        )
        .unwrap_err();
        assert!(matches!(err, AdvisorCapsuleDeny::ForeignPath(_)));
    }

    #[test]
    fn secret_input_fails_closed() {
        assert!(matches!(
            build_advisor_capsule(
                "r3",
                AdvisorRequestKind::EvidenceGapReview,
                "-----begin private key-----",
                "ok",
                None,
                &[],
                &["artifacts/"]
            )
            .unwrap_err(),
            AdvisorCapsuleDeny::SecretLike
        ));
        // `sk-` credential material is a hard deny, not redacted-through.
        assert!(matches!(
            build_advisor_capsule(
                "r3b",
                AdvisorRequestKind::EvidenceGapReview,
                "creds: sk-abc123xyz",
                "ok",
                None,
                &[],
                &["artifacts/"]
            )
            .unwrap_err(),
            AdvisorCapsuleDeny::SecretLike
        ));
    }

    #[test]
    fn oversized_question_and_too_many_artifacts_fail_closed() {
        let big: String = "q".repeat(MAX_REVIEW_QUESTION + 1);
        assert!(matches!(
            build_advisor_capsule(
                "r4",
                AdvisorRequestKind::PlanReview,
                "ok",
                "ok",
                Some(&big),
                &[],
                &["artifacts/"]
            )
            .unwrap_err(),
            AdvisorCapsuleDeny::QuestionOversize
        ));
        let refs: Vec<String> = (0..MAX_ARTIFACT_REFS + 1).map(|i| format!("artifacts/{i}.md")).collect();
        assert!(matches!(
            build_advisor_capsule("r5", AdvisorRequestKind::PlanReview, "ok", "ok", None, &refs, &["artifacts/"])
                .unwrap_err(),
            AdvisorCapsuleDeny::TooManyArtifacts
        ));
    }

    #[test]
    fn timeout_is_pure_and_blocked_never_success() {
        assert!(consult_timed_out(0, 1000, 1000));
        assert!(consult_timed_out(0, 1000, 2000));
        assert!(!consult_timed_out(0, 1000, 999));
        assert_eq!(
            ConsultOutcome::Blocked { reason: ConsultBlockReason::TimedOut }
                .blocked_code(),
            "advisor.timed_out"
        );
        assert_eq!(
            ConsultOutcome::Blocked { reason: ConsultBlockReason::AdvisorUnavailable }
                .blocked_code(),
            "advisor.unavailable"
        );
    }

    #[test]
    fn usage_receipt_keeps_unknown_usage_unknown_and_records_reason() {
        let r = build_usage_receipt(
            "u1",
            "req1",
            1,
            AdvisorMode::Shadow,
            None,
            "hash",
            TokenUsage::Unknown,
            Some(1000),
            Some("advisor.timed_out".into()),
            false,
        );
        assert_eq!(r.token_usage, TokenUsage::Unknown);
        assert_eq!(r.cancel_or_deny_reason.as_deref(), Some("advisor.timed_out"));
        assert!(!r.adopted_by_root);
        assert!(r.model_ref.is_none());
    }

    #[test]
    fn report_hash_drifts_on_capsule_change() {
        let report = issue_shadow_advice(
            AdvisorMode::Shadow,
            "a1",
            "consider running tests",
            None,
            Some("usage://u1".into()),
        )
        .unwrap();
        let c1 = build_advisor_capsule(
            "r",
            AdvisorRequestKind::PlanReview,
            "manifest v1",
            "snap",
            None,
            &[],
            &["artifacts/"],
        )
        .unwrap();
        let h1 = report_hash(&c1, &report);
        let mut c2 = c1.clone();
        c2.manifest_summary = "manifest v2".to_string();
        c2.capsule_hash = c2.compute_hash();
        let h2 = report_hash(&c2, &report);
        assert_ne!(h1, h2, "capsule drift must change the report hash");
    }

    trait BlockedCode {
        fn blocked_code(&self) -> &'static str;
    }
    impl BlockedCode for ConsultOutcome {
        fn blocked_code(&self) -> &'static str {
            match self {
                ConsultOutcome::Blocked { reason } => reason.code(),
                ConsultOutcome::Succeeded { .. } => "succeeded",
            }
        }
    }
}
