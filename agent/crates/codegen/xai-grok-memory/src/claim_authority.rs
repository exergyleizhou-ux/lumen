//! Actor-owned claim transition validator for the shared working-memory ledger.
//!
//! This module does not write storage and does not accept facts itself. The
//! SessionActor remains the only authority that may call the ledger review
//! path; ClaimAuthority only answers whether a proposed transition is legal
//! for a named actor role.

use crate::task_ledger::WorkingMemoryState;

/// Who is attempting a claim transition. Roles other than
/// [`ClaimAuthorityActor::RootSessionActor`] never accept facts.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ClaimAuthorityActor {
    /// Nested child agent (tool/session under a root task tree).
    Child,
    /// The root SessionActor for the task tree.
    RootSessionActor,
    /// Advisor / expert shadow reviewer — may advise, never accept.
    Advisor,
    /// Long-running Kairos / scheduler governance — never accept claims.
    Kairos,
    /// TUI / pager operator surface — never accept claims.
    Tui,
    /// MCP client or server adapter — never accept claims.
    Mcp,
    /// Raw tool output path — never accept claims.
    ToolOutput,
    /// Background daemon / workflow host without root session authority.
    Daemon,
    /// Unknown or unauthenticated caller.
    Unknown,
}

impl ClaimAuthorityActor {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Child => "child",
            Self::RootSessionActor => "root_session_actor",
            Self::Advisor => "advisor",
            Self::Kairos => "kairos",
            Self::Tui => "tui",
            Self::Mcp => "mcp",
            Self::ToolOutput => "tool_output",
            Self::Daemon => "daemon",
            Self::Unknown => "unknown",
        }
    }

    pub const fn is_root_session_actor(self) -> bool {
        matches!(self, Self::RootSessionActor)
    }
}

/// Machine-readable denial codes for claim transitions.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ClaimDenyReason {
    ChildCannotAccept,
    ChildCannotReview,
    NonRootCannotReview,
    AdvisorCannotAccept,
    KairosCannotAccept,
    TuiCannotAccept,
    McpCannotAccept,
    ToolOutputCannotAccept,
    DaemonCannotAccept,
    UnknownActorCannotAccept,
    MissingEvidence,
    ForeignTaskTree,
    StaleRevision,
    GrantCancelled,
    InvalidTransition,
    TerminalStateImmutable,
    HostVerificationRequired,
    DraftNotPersistable,
}

impl ClaimDenyReason {
    pub const fn code(self) -> &'static str {
        match self {
            Self::ChildCannotAccept => "claim.child_cannot_accept",
            Self::ChildCannotReview => "claim.child_cannot_review",
            Self::NonRootCannotReview => "claim.non_root_cannot_review",
            Self::AdvisorCannotAccept => "claim.advisor_cannot_accept",
            Self::KairosCannotAccept => "claim.kairos_cannot_accept",
            Self::TuiCannotAccept => "claim.tui_cannot_accept",
            Self::McpCannotAccept => "claim.mcp_cannot_accept",
            Self::ToolOutputCannotAccept => "claim.tool_output_cannot_accept",
            Self::DaemonCannotAccept => "claim.daemon_cannot_accept",
            Self::UnknownActorCannotAccept => "claim.unknown_actor_cannot_accept",
            Self::MissingEvidence => "claim.missing_evidence",
            Self::ForeignTaskTree => "claim.foreign_task_tree",
            Self::StaleRevision => "claim.stale_revision",
            Self::GrantCancelled => "claim.grant_cancelled",
            Self::InvalidTransition => "claim.invalid_transition",
            Self::TerminalStateImmutable => "claim.terminal_state_immutable",
            Self::HostVerificationRequired => "claim.host_verification_required",
            Self::DraftNotPersistable => "claim.draft_not_persistable",
        }
    }
}

impl std::fmt::Display for ClaimDenyReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.code(), self.message())
    }
}

impl ClaimDenyReason {
    pub const fn message(self) -> &'static str {
        match self {
            Self::ChildCannotAccept => "child agents may only propose claims",
            Self::ChildCannotReview => "child agents may not review claims",
            Self::NonRootCannotReview => "only the root SessionActor may review claims",
            Self::AdvisorCannotAccept => "Advisor cannot accept or complete claims",
            Self::KairosCannotAccept => "Kairos cannot accept or complete claims",
            Self::TuiCannotAccept => "TUI cannot accept or complete claims",
            Self::McpCannotAccept => "MCP cannot accept or complete claims",
            Self::ToolOutputCannotAccept => "tool output cannot accept or complete claims",
            Self::DaemonCannotAccept => "daemon cannot accept or complete claims",
            Self::UnknownActorCannotAccept => "unknown actor cannot accept or complete claims",
            Self::MissingEvidence => "accepted claims require a non-empty evidence_ref",
            Self::ForeignTaskTree => "claim belongs to a foreign task tree",
            Self::StaleRevision => "claim revision is stale or conflicts",
            Self::GrantCancelled => "capability grant was cancelled; claim transition denied",
            Self::InvalidTransition => "claim state transition is not permitted",
            Self::TerminalStateImmutable => "terminal claim state cannot be rewritten this way",
            Self::HostVerificationRequired => "host verification is required before acceptance",
            Self::DraftNotPersistable => "draft claims are not durable ledger records",
        }
    }
}

/// Inputs for one claim transition validation.
#[derive(Debug, Clone)]
pub struct ClaimTransitionRequest<'a> {
    pub actor: ClaimAuthorityActor,
    pub actor_session_id: &'a str,
    pub root_session_id: &'a str,
    pub ledger_task_tree_id: &'a str,
    pub fact_task_tree_id: &'a str,
    pub from: Option<WorkingMemoryState>,
    pub to: WorkingMemoryState,
    pub evidence_ref: Option<&'a str>,
    pub expected_revision: u64,
    pub actual_revision: u64,
    pub grant_cancelled: bool,
}

/// Pure transition validator. Callers must re-check storage races after this
/// returns Ok; revision conflicts remain fail-closed at append time.
pub struct ClaimAuthority;

impl ClaimAuthority {
    pub fn validate(request: &ClaimTransitionRequest<'_>) -> Result<(), ClaimDenyReason> {
        if request.grant_cancelled {
            return Err(ClaimDenyReason::GrantCancelled);
        }
        if request.fact_task_tree_id != request.ledger_task_tree_id {
            return Err(ClaimDenyReason::ForeignTaskTree);
        }
        if request.actual_revision != request.expected_revision {
            return Err(ClaimDenyReason::StaleRevision);
        }
        if request.to == WorkingMemoryState::Draft {
            return Err(ClaimDenyReason::DraftNotPersistable);
        }

        match request.to {
            WorkingMemoryState::Proposed => Self::validate_propose(request),
            WorkingMemoryState::EvidenceAttached => Self::validate_attach(request),
            WorkingMemoryState::HostVerified
            | WorkingMemoryState::Accepted
            | WorkingMemoryState::Rejected
            | WorkingMemoryState::Conflicted
            | WorkingMemoryState::Inconclusive
            | WorkingMemoryState::Superseded
            | WorkingMemoryState::Revoked
            | WorkingMemoryState::Frozen => Self::validate_review(request),
            WorkingMemoryState::Draft => Err(ClaimDenyReason::DraftNotPersistable),
        }
    }

    fn deny_non_root_review(actor: ClaimAuthorityActor) -> ClaimDenyReason {
        match actor {
            ClaimAuthorityActor::Child => ClaimDenyReason::ChildCannotReview,
            ClaimAuthorityActor::Advisor => ClaimDenyReason::AdvisorCannotAccept,
            ClaimAuthorityActor::Kairos => ClaimDenyReason::KairosCannotAccept,
            ClaimAuthorityActor::Tui => ClaimDenyReason::TuiCannotAccept,
            ClaimAuthorityActor::Mcp => ClaimDenyReason::McpCannotAccept,
            ClaimAuthorityActor::ToolOutput => ClaimDenyReason::ToolOutputCannotAccept,
            ClaimAuthorityActor::Daemon => ClaimDenyReason::DaemonCannotAccept,
            ClaimAuthorityActor::Unknown => ClaimDenyReason::UnknownActorCannotAccept,
            ClaimAuthorityActor::RootSessionActor => ClaimDenyReason::NonRootCannotReview,
        }
    }

    fn validate_propose(request: &ClaimTransitionRequest<'_>) -> Result<(), ClaimDenyReason> {
        // Only Child and Root may create Proposed records. Advisor/daemon/UI
        // must not inject "facts" into the shared ledger as proposals either
        // under a fake review path; proposal from non-agent roles is denied.
        match request.actor {
            ClaimAuthorityActor::Child | ClaimAuthorityActor::RootSessionActor => {}
            other => return Err(Self::deny_non_root_review(other)),
        }
        // A new Proposed revision is always allowed for child/root agents after
        // any prior durable state. It never silently overwrites Accepted shared
        // truth: accepted_facts() only updates on a later Accepted/Superseded/
        // Revoked review.
        match request.from {
            None
            | Some(WorkingMemoryState::Draft)
            | Some(WorkingMemoryState::Proposed)
            | Some(WorkingMemoryState::EvidenceAttached)
            | Some(WorkingMemoryState::HostVerified)
            | Some(WorkingMemoryState::Accepted)
            | Some(WorkingMemoryState::Rejected)
            | Some(WorkingMemoryState::Conflicted)
            | Some(WorkingMemoryState::Inconclusive)
            | Some(WorkingMemoryState::Superseded)
            | Some(WorkingMemoryState::Revoked)
            | Some(WorkingMemoryState::Frozen) => Ok(()),
        }
    }

    /// `Proposed → EvidenceAttached`: the original author node (or root)
    /// binds artifact/command/receipt hashes to the claim (master plan
    /// §3.1.1). Evidence is mandatory; a proposal without evidence cannot
    /// advance. Non-author roles (Advisor/TUI/daemon/MCP) never attach.
    fn validate_attach(request: &ClaimTransitionRequest<'_>) -> Result<(), ClaimDenyReason> {
        match request.actor {
            ClaimAuthorityActor::Child | ClaimAuthorityActor::RootSessionActor => {}
            other => return Err(Self::deny_non_root_review(other)),
        }
        if request.from != Some(WorkingMemoryState::Proposed) {
            return Err(ClaimDenyReason::InvalidTransition);
        }
        Self::require_evidence(request.evidence_ref)
    }

    fn validate_review(request: &ClaimTransitionRequest<'_>) -> Result<(), ClaimDenyReason> {
        if !request.actor.is_root_session_actor() {
            // Child direct Accepted is the highest-risk path.
            if matches!(
                request.to,
                WorkingMemoryState::Accepted | WorkingMemoryState::HostVerified
            ) && matches!(request.actor, ClaimAuthorityActor::Child)
            {
                return Err(ClaimDenyReason::ChildCannotAccept);
            }
            return Err(Self::deny_non_root_review(request.actor));
        }
        if request.actor_session_id != request.root_session_id {
            return Err(ClaimDenyReason::NonRootCannotReview);
        }

        let from = request.from;
        match (from, request.to) {
            // Root may mark host verification on a proposal or on an
            // evidence-attached claim (host receipt re-derivable).
            (Some(WorkingMemoryState::Proposed), WorkingMemoryState::HostVerified)
            | (Some(WorkingMemoryState::EvidenceAttached), WorkingMemoryState::HostVerified) => {
                Ok(())
            }
            // Atomic root acceptance: Proposed/EvidenceAttached → Accepted
            // implies host verification was performed by the root SessionActor
            // in the same authoritative call.
            (Some(WorkingMemoryState::Proposed), WorkingMemoryState::Accepted)
            | (Some(WorkingMemoryState::EvidenceAttached), WorkingMemoryState::Accepted)
            | (Some(WorkingMemoryState::HostVerified), WorkingMemoryState::Accepted) => {
                Self::require_evidence(request.evidence_ref)
            }
            (Some(WorkingMemoryState::Proposed), WorkingMemoryState::Rejected)
            | (Some(WorkingMemoryState::EvidenceAttached), WorkingMemoryState::Rejected)
            | (Some(WorkingMemoryState::HostVerified), WorkingMemoryState::Rejected)
            | (Some(WorkingMemoryState::Proposed), WorkingMemoryState::Superseded)
            | (Some(WorkingMemoryState::EvidenceAttached), WorkingMemoryState::Superseded)
            | (Some(WorkingMemoryState::HostVerified), WorkingMemoryState::Superseded)
            | (Some(WorkingMemoryState::Accepted), WorkingMemoryState::Superseded)
            | (Some(WorkingMemoryState::Accepted), WorkingMemoryState::Revoked)
            | (Some(WorkingMemoryState::HostVerified), WorkingMemoryState::Revoked)
            | (Some(WorkingMemoryState::EvidenceAttached), WorkingMemoryState::Revoked) => Ok(()),
            // Root review outcomes: conflicting/inconclusive on any in-flight
            // state (Proposed/EvidenceAttached/HostVerified).
            (Some(WorkingMemoryState::Proposed), WorkingMemoryState::Conflicted)
            | (Some(WorkingMemoryState::EvidenceAttached), WorkingMemoryState::Conflicted)
            | (Some(WorkingMemoryState::HostVerified), WorkingMemoryState::Conflicted)
            | (Some(WorkingMemoryState::Proposed), WorkingMemoryState::Inconclusive)
            | (Some(WorkingMemoryState::EvidenceAttached), WorkingMemoryState::Inconclusive)
            | (Some(WorkingMemoryState::HostVerified), WorkingMemoryState::Inconclusive) => Ok(()),
            // A Conflicted claim is resolved by a NEW resolution record
            // (master plan §3.1.1); resolution to Accepted still requires
            // evidence.
            (Some(WorkingMemoryState::Conflicted), WorkingMemoryState::Accepted) => {
                Self::require_evidence(request.evidence_ref)
            }
            (Some(WorkingMemoryState::Conflicted), WorkingMemoryState::Rejected)
            | (Some(WorkingMemoryState::Conflicted), WorkingMemoryState::Inconclusive) => Ok(()),
            // * → Frozen: recovery/actor only (root session actor). Frozen is
            // never auto-recovered and never shared truth.
            (Some(_), WorkingMemoryState::Frozen) => Ok(()),
            // First revision cannot be accepted without a prior proposal in the
            // normal path. Allow root to accept only when from is Proposed or
            // HostVerified. from=None → Accept is invalid (no claim to review).
            (None, WorkingMemoryState::Accepted) => Err(ClaimDenyReason::HostVerificationRequired),
            (None, WorkingMemoryState::HostVerified) => Err(ClaimDenyReason::InvalidTransition),
            (None, WorkingMemoryState::Frozen) => Err(ClaimDenyReason::InvalidTransition),
            (Some(WorkingMemoryState::Accepted), WorkingMemoryState::Accepted) => {
                Err(ClaimDenyReason::TerminalStateImmutable)
            }
            (Some(WorkingMemoryState::Rejected), WorkingMemoryState::Accepted)
            | (Some(WorkingMemoryState::Superseded), WorkingMemoryState::Accepted)
            | (Some(WorkingMemoryState::Revoked), WorkingMemoryState::Accepted) => {
                Err(ClaimDenyReason::InvalidTransition)
            }
            (Some(WorkingMemoryState::Draft), _) => Err(ClaimDenyReason::DraftNotPersistable),
            _ => Err(ClaimDenyReason::InvalidTransition),
        }
    }

    fn require_evidence(evidence_ref: Option<&str>) -> Result<(), ClaimDenyReason> {
        match evidence_ref.map(str::trim) {
            Some(value) if !value.is_empty() => Ok(()),
            _ => Err(ClaimDenyReason::MissingEvidence),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn base<'a>(
        actor: ClaimAuthorityActor,
        from: Option<WorkingMemoryState>,
        to: WorkingMemoryState,
    ) -> ClaimTransitionRequest<'a> {
        ClaimTransitionRequest {
            actor,
            actor_session_id: if actor.is_root_session_actor() {
                "root"
            } else {
                "child"
            },
            root_session_id: "root",
            ledger_task_tree_id: "root",
            fact_task_tree_id: "root",
            from,
            to,
            evidence_ref: Some("test://evidence"),
            expected_revision: 2,
            actual_revision: 2,
            grant_cancelled: false,
        }
    }

    #[test]
    fn child_direct_accepted_is_rejected() {
        let err = ClaimAuthority::validate(&base(
            ClaimAuthorityActor::Child,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::Accepted,
        ))
        .unwrap_err();
        assert_eq!(err, ClaimDenyReason::ChildCannotAccept);
        assert_eq!(err.code(), "claim.child_cannot_accept");
    }

    #[test]
    fn root_accepted_without_evidence_is_rejected() {
        let mut req = base(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::Accepted,
        );
        req.evidence_ref = None;
        assert_eq!(
            ClaimAuthority::validate(&req).unwrap_err(),
            ClaimDenyReason::MissingEvidence
        );
    }

    #[test]
    fn advisor_daemon_tui_acceptance_rejected() {
        for actor in [
            ClaimAuthorityActor::Advisor,
            ClaimAuthorityActor::Daemon,
            ClaimAuthorityActor::Tui,
            ClaimAuthorityActor::Kairos,
            ClaimAuthorityActor::Mcp,
            ClaimAuthorityActor::ToolOutput,
        ] {
            let err = ClaimAuthority::validate(&base(
                actor,
                Some(WorkingMemoryState::Proposed),
                WorkingMemoryState::Accepted,
            ))
            .unwrap_err();
            assert_ne!(err, ClaimDenyReason::ChildCannotAccept);
            let code = err.code();
            assert!(
                code.contains("cannot_accept") || code.contains("non_root"),
                "actor {actor:?} reason {code}"
            );
        }
    }

    #[test]
    fn foreign_tree_and_stale_revision_fail_closed() {
        let mut foreign = base(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::Accepted,
        );
        foreign.fact_task_tree_id = "other-tree";
        assert_eq!(
            ClaimAuthority::validate(&foreign).unwrap_err(),
            ClaimDenyReason::ForeignTaskTree
        );
        let mut stale = base(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::Accepted,
        );
        stale.actual_revision = 1;
        stale.expected_revision = 2;
        assert_eq!(
            ClaimAuthority::validate(&stale).unwrap_err(),
            ClaimDenyReason::StaleRevision
        );
    }

    #[test]
    fn cancelled_grant_fails_closed() {
        let mut req = base(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::Accepted,
        );
        req.grant_cancelled = true;
        assert_eq!(
            ClaimAuthority::validate(&req).unwrap_err(),
            ClaimDenyReason::GrantCancelled
        );
    }

    #[test]
    fn happy_path_proposed_host_verified_accepted() {
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::Child,
                None,
                WorkingMemoryState::Proposed,
            ))
            .is_ok()
        );
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::Proposed),
                WorkingMemoryState::HostVerified,
            ))
            .is_ok()
        );
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::HostVerified),
                WorkingMemoryState::Accepted,
            ))
            .is_ok()
        );
        // Atomic Proposed → Accepted for root with evidence.
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::Proposed),
                WorkingMemoryState::Accepted,
            ))
            .is_ok()
        );
    }

    #[test]
    fn child_attaches_evidence_to_own_proposal() {
        let ok = ClaimAuthority::validate(&base(
            ClaimAuthorityActor::Child,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::EvidenceAttached,
        ));
        assert!(ok.is_ok(), "author node may bind evidence: {ok:?}");
        let ok = ClaimAuthority::validate(&base(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::EvidenceAttached,
        ));
        assert!(ok.is_ok(), "root may bind evidence: {ok:?}");
    }

    #[test]
    fn attach_requires_proposed_from_and_evidence() {
        // Attach without prior Proposed is an invalid transition.
        let err = ClaimAuthority::validate(&base(
            ClaimAuthorityActor::Child,
            Some(WorkingMemoryState::HostVerified),
            WorkingMemoryState::EvidenceAttached,
        ))
        .unwrap_err();
        assert_eq!(err, ClaimDenyReason::InvalidTransition);
        // Attach with no evidence fails closed.
        let mut req = base(
            ClaimAuthorityActor::Child,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::EvidenceAttached,
        );
        req.evidence_ref = None;
        assert_eq!(
            ClaimAuthority::validate(&req).unwrap_err(),
            ClaimDenyReason::MissingEvidence
        );
        // Non-author roles never attach.
        let err = ClaimAuthority::validate(&base(
            ClaimAuthorityActor::Advisor,
            Some(WorkingMemoryState::Proposed),
            WorkingMemoryState::EvidenceAttached,
        ))
        .unwrap_err();
        assert_eq!(err, ClaimDenyReason::AdvisorCannotAccept);
    }

    #[test]
    fn root_reviews_to_inconclusive_conflicted_and_frozen() {
        for from in [
            WorkingMemoryState::Proposed,
            WorkingMemoryState::EvidenceAttached,
            WorkingMemoryState::HostVerified,
        ] {
            assert!(
                ClaimAuthority::validate(&base(
                    ClaimAuthorityActor::RootSessionActor,
                    Some(from),
                    WorkingMemoryState::Inconclusive,
                ))
                .is_ok(),
                "root may mark {from:?} inconclusive"
            );
            assert!(
                ClaimAuthority::validate(&base(
                    ClaimAuthorityActor::RootSessionActor,
                    Some(from),
                    WorkingMemoryState::Conflicted,
                ))
                .is_ok(),
                "root may mark {from:?} conflicted"
            );
        }
        // Freeze from any durable state, root only.
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::Accepted),
                WorkingMemoryState::Frozen,
            ))
            .is_ok()
        );
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::Proposed),
                WorkingMemoryState::Frozen,
            ))
            .is_ok()
        );
        // Frozen from nothing is invalid — nothing to freeze.
        assert_eq!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                None,
                WorkingMemoryState::Frozen,
            ))
            .unwrap_err(),
            ClaimDenyReason::InvalidTransition
        );
    }

    #[test]
    fn frozen_transition_is_root_only() {
        let err = ClaimAuthority::validate(&base(
            ClaimAuthorityActor::Child,
            Some(WorkingMemoryState::Accepted),
            WorkingMemoryState::Frozen,
        ))
        .unwrap_err();
        assert_eq!(err, ClaimDenyReason::ChildCannotReview);
        let err = ClaimAuthority::validate(&base(
            ClaimAuthorityActor::Daemon,
            Some(WorkingMemoryState::Accepted),
            WorkingMemoryState::Frozen,
        ))
        .unwrap_err();
        assert_eq!(err, ClaimDenyReason::DaemonCannotAccept);
    }

    #[test]
    fn conflicted_resolution_requires_new_record() {
        // Resolution to Accepted needs evidence; to Rejected/Inconclusive is a
        // plain root review.
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::Conflicted),
                WorkingMemoryState::Accepted,
            ))
            .is_ok()
        );
        let mut req = base(
            ClaimAuthorityActor::RootSessionActor,
            Some(WorkingMemoryState::Conflicted),
            WorkingMemoryState::Accepted,
        );
        req.evidence_ref = None;
        assert_eq!(
            ClaimAuthority::validate(&req).unwrap_err(),
            ClaimDenyReason::MissingEvidence
        );
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::Conflicted),
                WorkingMemoryState::Rejected,
            ))
            .is_ok()
        );
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::Conflicted),
                WorkingMemoryState::Inconclusive,
            ))
            .is_ok()
        );
    }

    #[test]
    fn evidence_attached_to_host_verified_and_accepted() {
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::EvidenceAttached),
                WorkingMemoryState::HostVerified,
            ))
            .is_ok()
        );
        assert!(
            ClaimAuthority::validate(&base(
                ClaimAuthorityActor::RootSessionActor,
                Some(WorkingMemoryState::EvidenceAttached),
                WorkingMemoryState::Accepted,
            ))
            .is_ok()
        );
        // Child can never promote to HostVerified/Accepted — even with
        // evidence attached.
        let err = ClaimAuthority::validate(&base(
            ClaimAuthorityActor::Child,
            Some(WorkingMemoryState::EvidenceAttached),
            WorkingMemoryState::Accepted,
        ))
        .unwrap_err();
        assert_eq!(err, ClaimDenyReason::ChildCannotAccept);
    }
}
