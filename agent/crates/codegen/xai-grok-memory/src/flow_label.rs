//! Flow labels — DEBT-028 W2c-3 (EXPERIMENT, single path: advisor capsule
//! export).
//!
//! The threat table's hardest items (prompt-injection relay, exfiltration,
//! memory poisoning, cache cross-contamination) are information-flow
//! problems, not access-control problems: each individual step is
//! authorised, yet data flows from a low-integrity position to a
//! high-integrity decision point, or from a high-confidentiality position to
//! a low-privilege exit.
//!
//! This module is the minimal, honest experiment: two-component labels
//! (Denning-style) with the two lattice directions the review derived:
//!
//! - Confidentiality joins to the STRICTEST label when data mixes;
//! - Integrity meets to the LEAST trusted label when data mixes.
//!
//! Scope decision (written down, not aspirational): if wiring this single
//! path (capsule export) proves cheap, promote to the other sinks
//! (dispatch, grant mutation, claim transition, journal append). If the
//! propagation hurts, the existing `ContextTrustClass` manual tags remain
//! the fallback — the cost is then documented instead of hidden.

use serde::{Deserialize, Serialize};

/// Confidentiality lattice: `Public ⊑ WorkspacePrivate ⊑ Sensitive ⊑
/// Credential`. Mixing takes the strictest (join).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Confidentiality {
    Public,
    WorkspacePrivate,
    Sensitive,
    Credential,
}

impl Confidentiality {
    /// Strictest of the two (join).
    pub fn join(self, other: Self) -> Self {
        self.max(other)
    }
}

/// Integrity lattice: `Untrusted ⊑ Advisory ⊑ HostVerified ⊑
/// RootAssignment`. Mixing takes the least trusted (meet).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Integrity {
    Untrusted,
    Advisory,
    HostVerified,
    RootAssignment,
}

impl Integrity {
    /// Least trusted of the two (meet).
    pub fn meet(self, other: Self) -> Self {
        self.min(other)
    }
}

/// A value crossing a boundary, carrying its two-component label.
/// The inner value is crate-private: labels cannot be forged by consumers.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Labeled<T> {
    value: T,
    pub confidentiality: Confidentiality,
    pub integrity: Integrity,
}

impl<T> Labeled<T> {
    pub(crate) fn new(
        value: T,
        confidentiality: Confidentiality,
        integrity: Integrity,
    ) -> Self {
        Self {
            value,
            confidentiality,
            integrity,
        }
    }

    pub fn into_inner(self) -> T {
        self.value
    }

    /// Combine two labeled values: confidentiality joins (strictest),
    /// integrity meets (least trusted). The produced label is what any
    /// downstream consumer must honour.
    pub fn combine(&self, other: &Self) -> (Confidentiality, Integrity) {
        (
            self.confidentiality.join(other.confidentiality),
            self.integrity.meet(other.integrity),
        )
    }
}

/// T2: a value may only reach an export sink whose privilege is at least the
/// value's confidentiality (exfiltration is a type error).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FlowDeny {
    ConfidentialityExceedsExport {
        value: Confidentiality,
        export: Confidentiality,
    },
}

/// T2 enforcement at the capsule-export sink (the experiment path).
pub fn authorize_capsule_export(
    value_confidentiality: Confidentiality,
    export_privilege: Confidentiality,
) -> Result<(), FlowDeny> {
    if value_confidentiality > export_privilege {
        return Err(FlowDeny::ConfidentialityExceedsExport {
            value: value_confidentiality,
            export: export_privilege,
        });
    }
    Ok(())
}

/// Explicit, receipted declassification. The ONLY way a label weakens; every
/// call site is greppable and counted (the declassify count must be
/// monotone-decreasing across releases — a rising count means the label
/// split is wrong).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct DeclassifyReceipt {
    pub authority: String,
    pub from: Confidentiality,
    pub to: Confidentiality,
    pub reason: String,
}

pub fn declassify(
    value: Labeled<String>,
    authority: &str,
    to: Confidentiality,
    reason: &str,
) -> Result<(Labeled<String>, DeclassifyReceipt), FlowDeny> {
    if to > value.confidentiality {
        return Err(FlowDeny::ConfidentialityExceedsExport {
            value: value.confidentiality,
            export: to,
        });
    }
    let receipt = DeclassifyReceipt {
        authority: authority.to_string(),
        from: value.confidentiality,
        to,
        reason: reason.to_string(),
    };
    let relabeled = Labeled::new(value.value, to, value.integrity);
    Ok((relabeled, receipt))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn confidentiality_joins_to_strictest() {
        assert_eq!(
            Confidentiality::WorkspacePrivate.join(Confidentiality::Sensitive),
            Confidentiality::Sensitive
        );
        assert_eq!(
            Confidentiality::Credential.join(Confidentiality::Public),
            Confidentiality::Credential
        );
        assert_eq!(
            Confidentiality::Public.join(Confidentiality::Public),
            Confidentiality::Public
        );
    }

    #[test]
    fn integrity_meets_to_least_trusted() {
        assert_eq!(
            Integrity::Untrusted.meet(Integrity::RootAssignment),
            Integrity::Untrusted
        );
        assert_eq!(
            Integrity::HostVerified.meet(Integrity::Advisory),
            Integrity::Advisory
        );
    }

    #[test]
    fn combine_mixes_both_components() {
        let a = Labeled::new("web text", Confidentiality::Public, Integrity::Untrusted);
        let b = Labeled::new(
            "root assignment",
            Confidentiality::WorkspacePrivate,
            Integrity::RootAssignment,
        );
        let (c, i) = a.combine(&b);
        assert_eq!(c, Confidentiality::WorkspacePrivate, "join = strictest");
        assert_eq!(i, Integrity::Untrusted, "meet = least trusted");
    }

    #[test]
    fn capsule_export_rejects_above_export_privilege() {
        // Credential content must never reach a Public/Sensitive export —
        // exfiltration becomes a type error at the sink.
        authorize_capsule_export(Confidentiality::Public, Confidentiality::Public)
            .expect("public ok");
        authorize_capsule_export(Confidentiality::WorkspacePrivate, Confidentiality::Sensitive)
            .expect("within privilege ok");
        assert_eq!(
            authorize_capsule_export(Confidentiality::Credential, Confidentiality::Sensitive)
                .unwrap_err(),
            FlowDeny::ConfidentialityExceedsExport {
                value: Confidentiality::Credential,
                export: Confidentiality::Sensitive
            }
        );
    }

    #[test]
    fn declassify_is_explicit_receipted_and_only_weakens() {
        let secret = Labeled::new(
            "value".to_string(),
            Confidentiality::Credential,
            Integrity::HostVerified,
        );
        // Declassify only weakens confidentiality — attempting to STRENGTHEN
        // (e.g. Public → Credential) is refused.
        let public = Labeled::new(
            "value".to_string(),
            Confidentiality::Public,
            Integrity::Untrusted,
        );
        assert_eq!(
            declassify(public, "root", Confidentiality::Credential, "escalate").unwrap_err(),
            FlowDeny::ConfidentialityExceedsExport {
                value: Confidentiality::Public,
                export: Confidentiality::Credential
            }
        );
        let (relabeled, receipt) = declassify(
            secret,
            "root",
            Confidentiality::Public,
            "UI display only",
        )
        .expect("declassify");
        assert_eq!(relabeled.confidentiality, Confidentiality::Public);
        assert_eq!(relabeled.integrity, Integrity::HostVerified);
        assert_eq!(receipt.authority, "root");
        assert_eq!(receipt.from, Confidentiality::Credential);
        assert_eq!(receipt.to, Confidentiality::Public);
        assert_eq!(receipt.reason, "UI display only");
    }
}
