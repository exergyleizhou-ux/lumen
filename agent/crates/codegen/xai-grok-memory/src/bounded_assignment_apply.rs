//! S11 / NG-07 — when a root-approved bounded assignment may be Applied.
//!
//! Applied is never automatic from model prose. All gates must hold; any
//! Unknown/stale condition fails closed.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AssignmentLifecycle {
    Draft,
    RootApproved,
    Applied,
    Rejected,
    Superseded,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AssignmentApplyDeny {
    NotRootApproved,
    HashMismatch,
    SnapshotStale,
    BudgetNotHeld,
    AlreadyApplied,
    Superseded,
    Rejected,
    EmptyIdentity,
}

impl AssignmentApplyDeny {
    pub fn code(&self) -> &'static str {
        match self {
            Self::NotRootApproved => "assignment.not_root_approved",
            Self::HashMismatch => "assignment.hash_mismatch",
            Self::SnapshotStale => "assignment.snapshot_stale",
            Self::BudgetNotHeld => "assignment.budget_not_held",
            Self::AlreadyApplied => "assignment.already_applied",
            Self::Superseded => "assignment.superseded",
            Self::Rejected => "assignment.rejected",
            Self::EmptyIdentity => "assignment.empty_identity",
        }
    }
}

/// Inputs the actor must have already observed (not invented by child).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AssignmentApplyRequest<'a> {
    pub lifecycle: AssignmentLifecycle,
    pub assignment_hash: &'a str,
    pub expected_assignment_hash: &'a str,
    pub accepted_snapshot_hash: &'a str,
    pub live_snapshot_hash: &'a str,
    pub budget_reservation_held: bool,
}

/// Pure gate: transition RootApproved → Applied only.
pub fn authorize_assignment_apply(
    req: &AssignmentApplyRequest<'_>,
) -> Result<AssignmentLifecycle, AssignmentApplyDeny> {
    if req.assignment_hash.trim().is_empty() || req.expected_assignment_hash.trim().is_empty() {
        return Err(AssignmentApplyDeny::EmptyIdentity);
    }
    match req.lifecycle {
        AssignmentLifecycle::Applied => return Err(AssignmentApplyDeny::AlreadyApplied),
        AssignmentLifecycle::Superseded => return Err(AssignmentApplyDeny::Superseded),
        AssignmentLifecycle::Rejected => return Err(AssignmentApplyDeny::Rejected),
        AssignmentLifecycle::Draft => return Err(AssignmentApplyDeny::NotRootApproved),
        AssignmentLifecycle::RootApproved => {}
    }
    if req.assignment_hash != req.expected_assignment_hash {
        return Err(AssignmentApplyDeny::HashMismatch);
    }
    if req.accepted_snapshot_hash != req.live_snapshot_hash {
        return Err(AssignmentApplyDeny::SnapshotStale);
    }
    if !req.budget_reservation_held {
        return Err(AssignmentApplyDeny::BudgetNotHeld);
    }
    Ok(AssignmentLifecycle::Applied)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ok_req() -> AssignmentApplyRequest<'static> {
        AssignmentApplyRequest {
            lifecycle: AssignmentLifecycle::RootApproved,
            assignment_hash: "sha256:a",
            expected_assignment_hash: "sha256:a",
            accepted_snapshot_hash: "sha256:s",
            live_snapshot_hash: "sha256:s",
            budget_reservation_held: true,
        }
    }

    #[test]
    fn apply_requires_all_gates() {
        assert_eq!(
            authorize_assignment_apply(&ok_req()).unwrap(),
            AssignmentLifecycle::Applied
        );
        let mut r = ok_req();
        r.lifecycle = AssignmentLifecycle::Draft;
        assert_eq!(
            authorize_assignment_apply(&r).unwrap_err(),
            AssignmentApplyDeny::NotRootApproved
        );
        let mut r = ok_req();
        r.expected_assignment_hash = "sha256:other";
        assert_eq!(
            authorize_assignment_apply(&r).unwrap_err(),
            AssignmentApplyDeny::HashMismatch
        );
        let mut r = ok_req();
        r.live_snapshot_hash = "sha256:stale";
        assert_eq!(
            authorize_assignment_apply(&r).unwrap_err(),
            AssignmentApplyDeny::SnapshotStale
        );
        let mut r = ok_req();
        r.budget_reservation_held = false;
        assert_eq!(
            authorize_assignment_apply(&r).unwrap_err(),
            AssignmentApplyDeny::BudgetNotHeld
        );
        let mut r = ok_req();
        r.lifecycle = AssignmentLifecycle::Applied;
        assert_eq!(
            authorize_assignment_apply(&r).unwrap_err(),
            AssignmentApplyDeny::AlreadyApplied
        );
    }
}
