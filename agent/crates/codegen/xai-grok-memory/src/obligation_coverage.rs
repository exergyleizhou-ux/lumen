//! ObligationCoverageV1 — F5 / DEBT-028 W1-1: negative knowledge and the
//! completion gate.
//!
//! The system can represent "this claim is unproven", but until this module
//! it could not represent "this question was never asked". A child that never
//! ran its tests looks perfectly healthy: zero failed obligations, zero
//! falsified claims. That is more dangerous than a lying child, because no
//! defence trips.
//!
//! This module closes that gap with three rules:
//!
//! 1. `CompletionCandidate` requires `never_attempted` to be empty —
//!    structural, checked at the candidate boundary, not a runtime hope.
//! 2. The root must explicitly issue an `AdequacyDeclaration` — "this
//!    obligation set is enough to judge the task complete". The declaration
//!    is itself an auditable ledger claim (kind=Decision) that later
//!    evidence can overturn.
//! 3. Overturning the declaration cascades: `Accepted` facts that relied on
//!    it lose support through the existing revocation propagation.
//!
//! Two "no"s are distinguished structurally: `LookedAndDidNotFind` (a probe
//! receipt — that IS evidence) vs `NeverLooked` (a gap). There is no
//! conversion between them.

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue};

pub const OBLIGATION_COVERAGE_SCHEMA_VERSION: u16 = 1;

/// Per-tree obligation bookkeeping. `never_attempted` is DERIVED at build
/// time (`declared − discharged − refuted`) — there is no constructor path
/// that lets a caller hand-write it.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct ObligationCoverageV1 {
    pub schema_version: u16,
    pub tree_id: String,
    /// The declared obligation set (deduplicated, ordered).
    pub declared: Vec<String>,
    /// Obligations whose host-checkable predicate was adjudicated true.
    pub discharged: Vec<String>,
    /// Obligations whose predicate was adjudicated false.
    pub refuted: Vec<String>,
    /// Declared − discharged − refuted. Never hand-written.
    pub never_attempted: Vec<String>,
    /// Ledger claim id of the root's adequacy declaration (kind=Decision).
    pub adequacy_declaration: Option<String>,
    /// Canonical hash over all coverage fields (NG-00 mechanism).
    pub coverage_hash: String,
}

/// The two kinds of "no". Only [`KnowledgeGap::LookedAndDidNotFind`] carries
/// a probe receipt; `NeverLooked` is a gap, not evidence.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum KnowledgeGap {
    LookedAndDidNotFind { probe_ref: String },
    NeverLooked,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CoverageDeny {
    Invalid(String),
    EmptyField(&'static str),
    /// `CompletionCandidate` requires every declared obligation to have been
    /// attempted (discharged or refuted).
    NeverAttemptedNotEmpty,
    /// A discharged/refuted id that was never declared.
    UndeclaredMember,
    /// Duplicate id in the declared set.
    DuplicateDeclared,
    /// Same obligation listed as both discharged and refuted.
    DoubleAdjudicated,
    /// Unattended loops require an adequacy declaration.
    AdequacyMissing,
    /// The adequacy declaration was overturned; reliance on it is Frozen.
    AdequacyRevoked,
    HashMismatch,
    SchemaMismatch,
}

impl CoverageDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::Invalid(_) => "coverage.invalid",
            Self::EmptyField(_) => "coverage.empty_field",
            Self::NeverAttemptedNotEmpty => "coverage.never_attempted_not_empty",
            Self::UndeclaredMember => "coverage.undeclared_member",
            Self::DuplicateDeclared => "coverage.duplicate_declared",
            Self::DoubleAdjudicated => "coverage.double_adjudicated",
            Self::AdequacyMissing => "coverage.adequacy_missing",
            Self::AdequacyRevoked => "coverage.adequacy_revoked",
            Self::HashMismatch => "coverage.hash_mismatch",
            Self::SchemaMismatch => "coverage.schema_mismatch",
        }
    }
}

impl std::fmt::Display for CoverageDeny {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Invalid(msg) => write!(f, "{}: {msg}", self.code()),
            Self::EmptyField(name) => write!(f, "{}: {name}", self.code()),
            other => write!(f, "{}", other.code()),
        }
    }
}

fn coverage_preimage(coverage: &ObligationCoverageV1) -> Result<Vec<u8>, CanonicalError> {
    CanonicalRecord::new("obligation-coverage")
        .field("schema_version", CanonicalValue::U64(coverage.schema_version as u64))
        .field("tree_id", CanonicalValue::str(&coverage.tree_id))
        .field(
            "declared",
            CanonicalValue::Seq(
                coverage
                    .declared
                    .iter()
                    .map(CanonicalValue::str)
                    .collect(),
            ),
        )
        .field(
            "discharged",
            CanonicalValue::Seq(
                coverage
                    .discharged
                    .iter()
                    .map(CanonicalValue::str)
                    .collect(),
            ),
        )
        .field(
            "refuted",
            CanonicalValue::Seq(
                coverage
                    .refuted
                    .iter()
                    .map(CanonicalValue::str)
                    .collect(),
            ),
        )
        .field(
            "never_attempted",
            CanonicalValue::Seq(
                coverage
                    .never_attempted
                    .iter()
                    .map(CanonicalValue::str)
                    .collect(),
            ),
        )
        .field(
            "adequacy_declaration",
            match &coverage.adequacy_declaration {
                Some(id) => CanonicalValue::str(id),
                None => CanonicalValue::Null,
            },
        )
        .canonical_bytes()
}

/// Build coverage for one tree. `never_attempted` is derived, never
/// caller-supplied: this is the type-level guarantee that a completion
/// candidate cannot be minted while unasked questions exist.
pub fn build_coverage(
    tree_id: impl Into<String>,
    declared: &[&str],
    discharged: &[&str],
    refuted: &[&str],
) -> Result<ObligationCoverageV1, CoverageDeny> {
    let tree_id = tree_id.into();
    if tree_id.trim().is_empty() {
        return Err(CoverageDeny::EmptyField("tree_id"));
    }
    let mut seen: Vec<&str> = Vec::new();
    let mut declared_owned: Vec<String> = Vec::new();
    for id in declared {
        if id.trim().is_empty() {
            return Err(CoverageDeny::EmptyField("declared"));
        }
        if seen.contains(id) {
            return Err(CoverageDeny::DuplicateDeclared);
        }
        seen.push(id);
        declared_owned.push((*id).to_string());
    }
    if declared_owned.is_empty() {
        return Err(CoverageDeny::EmptyField("declared"));
    }
    let mut discharged_owned: Vec<String> = Vec::new();
    for id in discharged {
        if !declared_owned.iter().any(|d| d == id) {
            return Err(CoverageDeny::UndeclaredMember);
        }
        if discharged_owned.contains(&(*id).to_string()) {
            return Err(CoverageDeny::DuplicateDeclared);
        }
        discharged_owned.push((*id).to_string());
    }
    let mut refuted_owned: Vec<String> = Vec::new();
    for id in refuted {
        if !declared_owned.iter().any(|d| d == id) {
            return Err(CoverageDeny::UndeclaredMember);
        }
        if discharged_owned.iter().any(|d| d == id) {
            return Err(CoverageDeny::DoubleAdjudicated);
        }
        if refuted_owned.contains(&(*id).to_string()) {
            return Err(CoverageDeny::DuplicateDeclared);
        }
        refuted_owned.push((*id).to_string());
    }
    let mut never_attempted: Vec<String> = declared_owned
        .iter()
        .filter(|id| {
            !discharged_owned.contains(id) && !refuted_owned.contains(id)
        })
        .cloned()
        .collect();
    never_attempted.sort();
    discharged_owned.sort();
    refuted_owned.sort();

    let mut coverage = ObligationCoverageV1 {
        schema_version: OBLIGATION_COVERAGE_SCHEMA_VERSION,
        tree_id,
        declared: declared_owned,
        discharged: discharged_owned,
        refuted: refuted_owned,
        never_attempted,
        adequacy_declaration: None,
        coverage_hash: String::new(),
    };
    let hash = coverage_preimage(&coverage)
        .map_err(|e| CoverageDeny::Invalid(e.to_string()))?;
    coverage.coverage_hash = format!("sha256:{}", blake3::hash(&hash).to_hex());
    coverage.validate()?;
    Ok(coverage)
}

impl ObligationCoverageV1 {
    pub fn validate(&self) -> Result<(), CoverageDeny> {
        if self.schema_version != OBLIGATION_COVERAGE_SCHEMA_VERSION {
            return Err(CoverageDeny::SchemaMismatch);
        }
        if self.tree_id.trim().is_empty() {
            return Err(CoverageDeny::EmptyField("tree_id"));
        }
        if self.declared.is_empty() {
            return Err(CoverageDeny::EmptyField("declared"));
        }
        let recomputed = coverage_preimage(self)
            .map_err(|e| CoverageDeny::Invalid(e.to_string()))?;
        if format!("sha256:{}", blake3::hash(&recomputed).to_hex()) != self.coverage_hash {
            return Err(CoverageDeny::HashMismatch);
        }
        Ok(())
    }

    /// Bind the root's adequacy declaration (a ledger claim id, kind
    /// Decision). Returns the updated coverage with a fresh hash.
    pub fn with_adequacy_declaration(
        mut self,
        declaration_claim_id: impl Into<String>,
    ) -> Result<Self, CoverageDeny> {
        let declaration_claim_id = declaration_claim_id.into();
        if declaration_claim_id.trim().is_empty() {
            return Err(CoverageDeny::EmptyField("adequacy_declaration"));
        }
        self.adequacy_declaration = Some(declaration_claim_id);
        let hash = coverage_preimage(&self)
            .map_err(|e| CoverageDeny::Invalid(e.to_string()))?;
        self.coverage_hash = format!("sha256:{}", blake3::hash(&hash).to_hex());
        self.validate()?;
        Ok(self)
    }
}

/// Rule 1: a completion candidate requires every declared obligation to have
/// been attempted AND a live (unrevoked) adequacy declaration. The candidate
/// may still fail verification later — this only gates candidatehood.
pub fn authorize_completion_candidate(
    coverage: &ObligationCoverageV1,
    adequacy_revoked: bool,
) -> Result<(), CoverageDeny> {
    coverage.validate()?;
    if !coverage.never_attempted.is_empty() {
        return Err(CoverageDeny::NeverAttemptedNotEmpty);
    }
    match &coverage.adequacy_declaration {
        None => Err(CoverageDeny::AdequacyMissing),
        Some(_) if adequacy_revoked => Err(CoverageDeny::AdequacyRevoked),
        Some(_) => Ok(()),
    }
}

/// Rule 2: an unattended (Kairos) loop requires a live adequacy declaration.
/// The declaration itself is a claim — it can be overturned, and then this
/// check fails closed.
pub fn authorize_unattended_loop(
    coverage: &ObligationCoverageV1,
    adequacy_revoked: bool,
) -> Result<(), CoverageDeny> {
    coverage.validate()?;
    match &coverage.adequacy_declaration {
        None => Err(CoverageDeny::AdequacyMissing),
        Some(_) if adequacy_revoked => Err(CoverageDeny::AdequacyRevoked),
        Some(_) => Ok(()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn declared_ids() -> Vec<&'static str> {
        vec!["obl-a", "obl-b", "obl-c"]
    }

    #[test]
    fn build_derives_never_attempted() {
        let coverage = build_coverage(
            "tree-1",
            &declared_ids(),
            &["obl-a"],
            &["obl-b"],
        )
        .expect("coverage");
        assert_eq!(coverage.never_attempted, vec!["obl-c".to_owned()]);
        coverage.validate().expect("valid");
    }

    #[test]
    fn build_rejects_inconsistent_sets() {
        assert_eq!(
            build_coverage("tree-1", &[], &[], &[]).unwrap_err(),
            CoverageDeny::EmptyField("declared")
        );
        assert_eq!(
            build_coverage("tree-1", &["a", "a"], &[], &[]).unwrap_err(),
            CoverageDeny::DuplicateDeclared
        );
        assert_eq!(
            build_coverage("tree-1", &declared_ids(), &["ghost"], &[]).unwrap_err(),
            CoverageDeny::UndeclaredMember
        );
        assert_eq!(
            build_coverage("tree-1", &declared_ids(), &["obl-a"], &["obl-a"]).unwrap_err(),
            CoverageDeny::DoubleAdjudicated
        );
    }

    #[test]
    fn completion_candidate_requires_full_attempt_and_live_adequacy() {
        let mut coverage = build_coverage(
            "tree-1",
            &declared_ids(),
            &["obl-a", "obl-b", "obl-c"],
            &[],
        )
        .expect("fully adjudicated");
        // No declaration → candidate refused.
        assert_eq!(
            authorize_completion_candidate(&coverage, false).unwrap_err(),
            CoverageDeny::AdequacyMissing
        );
        coverage = coverage
            .with_adequacy_declaration("claim-adeq-1")
            .expect("declaration");
        // Full coverage + live declaration → candidate legal.
        authorize_completion_candidate(&coverage, false).expect("candidate ok");
        // Overturned declaration → candidate refused (cascade follows).
        assert_eq!(
            authorize_completion_candidate(&coverage, true).unwrap_err(),
            CoverageDeny::AdequacyRevoked
        );
    }

    #[test]
    fn never_attempted_blocks_completion_candidate() {
        // One obligation was never attempted: the candidate is structurally
        // impossible — the exact "unasked question" gap (F5).
        let coverage = build_coverage(
            "tree-1",
            &declared_ids(),
            &["obl-a", "obl-b"],
            &[],
        )
        .expect("coverage")
        .with_adequacy_declaration("claim-adeq-1")
        .expect("declaration");
        assert_eq!(coverage.never_attempted, vec!["obl-c".to_owned()]);
        assert_eq!(
            authorize_completion_candidate(&coverage, false).unwrap_err(),
            CoverageDeny::NeverAttemptedNotEmpty
        );
        // Unattended loop still legal without full attempt — but never
        // without a live declaration.
        authorize_unattended_loop(&coverage, false).expect("loop ok");
        assert_eq!(
            authorize_unattended_loop(&coverage, true).unwrap_err(),
            CoverageDeny::AdequacyRevoked
        );
    }

    #[test]
    fn knowledge_gap_distinguishes_looked_from_never_looked() {
        let looked: KnowledgeGap = KnowledgeGap::LookedAndDidNotFind {
            probe_ref: "probe://go-test:receipt-1".into(),
        };
        let never: KnowledgeGap = KnowledgeGap::NeverLooked;
        assert_ne!(looked, never, "a probe receipt is evidence; NeverLooked is a gap");
        match looked {
            KnowledgeGap::LookedAndDidNotFind { probe_ref } => {
                assert_eq!(probe_ref, "probe://go-test:receipt-1");
            }
            KnowledgeGap::NeverLooked => panic!("wrong variant"),
        }
        assert_eq!(never, KnowledgeGap::NeverLooked);
    }

    #[test]
    fn tamper_detected() {
        // Build with one never-attempted obligation, then hand-wipe the
        // derived field: the canonical hash must catch the tamper.
        let mut coverage = build_coverage(
            "tree-1",
            &declared_ids(),
            &["obl-a", "obl-b"],
            &[],
        )
        .expect("coverage");
        assert_eq!(coverage.never_attempted, vec!["obl-c".to_owned()]);
        coverage.never_attempted = Vec::new();
        assert_eq!(coverage.validate().unwrap_err(), CoverageDeny::HashMismatch);
    }
}
