//! NG-03C schema bridge: coordinator `TreeAuthorityLog` → NG-00
//! `LifecycleJournal`.
//!
//! tools cannot depend on memory (cycle). The coordinator therefore keeps a
//! slim `AuthorityEvent` trail; this module is the single place that lifts
//! those events into full `GovernedLifecycleEventV1` records with payload
//! hash, evidence refs, and the execution-book kind vocabulary.
//!
//! Physical journals stay separate (INV-21). What is unified is the kind
//! mapping + evidence encoding so offline compose and future Kairos
//! consumers read one logical contract.

use crate::lifecycle_journal::{
    GovernedLifecycleEventKind, GovernedLifecycleEventSource, GovernedLifecycleEventV1,
    JournalError, LifecycleJournal,
};
use xai_grok_tools::implementations::grok_build::task::authority_log::{
    AuthorityEvent, AuthorityEventKind,
};

/// Context required to place a coordinator authority event into a tree-scoped
/// lifecycle journal.
#[derive(Debug, Clone)]
pub struct AuthorityProjectionContext {
    pub task_tree_id: String,
    pub owner_session_id: String,
    pub policy_revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AuthorityProjectionError {
    UnknownLifecycleKind(String),
    Journal(JournalError),
    PayloadHash,
}

impl std::fmt::Display for AuthorityProjectionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AuthorityProjectionError::UnknownLifecycleKind(kind) => {
                write!(f, "unknown lifecycle kind from authority map: {kind}")
            }
            AuthorityProjectionError::Journal(err) => write!(f, "lifecycle journal: {err}"),
            AuthorityProjectionError::PayloadHash => {
                write!(f, "failed to compute lifecycle payload hash")
            }
        }
    }
}

/// Map coordinator kind → §3.3.2 lifecycle kind (see `AuthorityEventKind::lifecycle_kind_str`).
pub fn map_authority_kind(kind: AuthorityEventKind) -> Result<GovernedLifecycleEventKind, AuthorityProjectionError> {
    parse_lifecycle_kind(kind.lifecycle_kind_str())
}

fn parse_lifecycle_kind(token: &str) -> Result<GovernedLifecycleEventKind, AuthorityProjectionError> {
    match token {
        "booting" => Ok(GovernedLifecycleEventKind::Booting),
        "ready" => Ok(GovernedLifecycleEventKind::Ready),
        "prompt_accepted" => Ok(GovernedLifecycleEventKind::PromptAccepted),
        "running" => Ok(GovernedLifecycleEventKind::Running),
        "blocked" => Ok(GovernedLifecycleEventKind::Blocked),
        "checkpointed" => Ok(GovernedLifecycleEventKind::Checkpointed),
        "terminal_succeeded" => Ok(GovernedLifecycleEventKind::TerminalSucceeded),
        "terminal_failed" => Ok(GovernedLifecycleEventKind::TerminalFailed),
        "cancelled" => Ok(GovernedLifecycleEventKind::Cancelled),
        "reconciled" => Ok(GovernedLifecycleEventKind::Reconciled),
        "frozen" => Ok(GovernedLifecycleEventKind::Frozen),
        other => Err(AuthorityProjectionError::UnknownLifecycleKind(other.to_owned())),
    }
}

/// Evidence tokens that keep the projection lossless for reverse inspection.
pub fn evidence_refs_for(event: &AuthorityEvent) -> Vec<String> {
    let mut refs = vec![
        format!("op:{}", event.operation_id),
        format!("coord_kind:{}", event.kind.as_str()),
    ];
    if let Some(reservation) = &event.reservation_id {
        refs.push(format!("reservation:{reservation}"));
    }
    refs
}

/// Recover the original coordinator kind from projected evidence (preferred)
/// or from a lossless lifecycle terminal kind.
pub fn recover_authority_kind(
    lifecycle_kind: GovernedLifecycleEventKind,
    evidence_refs: &[String],
) -> Option<AuthorityEventKind> {
    for reference in evidence_refs {
        if let Some(token) = reference.strip_prefix("coord_kind:") {
            if let Some(kind) = AuthorityEventKind::from_str_token(token) {
                return Some(kind);
            }
        }
    }
    // Terminals map 1:1 without coord_kind.
    let wire = match lifecycle_kind {
        GovernedLifecycleEventKind::TerminalSucceeded => "terminal_succeeded",
        GovernedLifecycleEventKind::TerminalFailed => "terminal_failed",
        GovernedLifecycleEventKind::Cancelled => "cancelled",
        _ => return None,
    };
    AuthorityEventKind::from_lifecycle_kind_str(wire)
}

/// Build one NG-00 lifecycle event from a coordinator authority event.
///
/// `sequence` / `causal_parent` are taken from the authority trail when
/// projecting a single-op chain into a dedicated journal (caller may also
/// re-sequence). `occurred_at` is a typed observation (INV-22), not an
/// ordering key.
pub fn project_authority_event(
    ctx: &AuthorityProjectionContext,
    event: &AuthorityEvent,
    causal_parent: Option<u64>,
    occurred_at: u64,
) -> Result<GovernedLifecycleEventV1, AuthorityProjectionError> {
    let kind = map_authority_kind(event.kind)?;
    let mut projected = GovernedLifecycleEventV1 {
        event_id: format!(
            "auth:{}:{}:{}",
            ctx.task_tree_id, event.operation_id, event.sequence
        ),
        task_tree_id: ctx.task_tree_id.clone(),
        node_id: event.node_id.clone(),
        owner_session_id: ctx.owner_session_id.clone(),
        sequence: event.sequence,
        causal_parent,
        kind,
        source: GovernedLifecycleEventSource::Actor,
        lease_id: None,
        contract_hash: None,
        policy_revision: ctx.policy_revision,
        evidence_refs: evidence_refs_for(event),
        occurred_at,
        payload_hash: String::new(),
    };
    projected.payload_hash = projected
        .compute_payload_hash()
        .map_err(|_| AuthorityProjectionError::PayloadHash)?;
    Ok(projected)
}

/// Project an ordered authority trail into a lifecycle journal.
///
/// Sequences are re-numbered 0..n-1 for the journal (monotonic local) while
/// `event_id` and evidence preserve the original authority sequence. Causal
/// parents form a simple chain. Fail-closed: any append error aborts.
pub fn project_authority_trail(
    journal: &mut LifecycleJournal,
    ctx: &AuthorityProjectionContext,
    events: &[AuthorityEvent],
    base_occurred_at: u64,
) -> Result<usize, AuthorityProjectionError> {
    let mut parent: Option<u64> = None;
    let mut written = 0usize;
    for (index, event) in events.iter().enumerate() {
        let mut projected =
            project_authority_event(ctx, event, parent, base_occurred_at.saturating_add(index as u64))?;
        // Journal requires exact next sequence; re-sequence for the journal
        // stream while keeping authority sequence in event_id / evidence.
        projected.sequence = journal.events().len() as u64;
        projected.causal_parent = parent;
        projected.payload_hash = projected
            .compute_payload_hash()
            .map_err(|_| AuthorityProjectionError::PayloadHash)?;
        journal
            .append(projected)
            .map_err(AuthorityProjectionError::Journal)?;
        parent = Some(journal.events().len() as u64 - 1);
        written += 1;
    }
    Ok(written)
}

#[cfg(test)]
mod projection_tests {
    use super::*;
    use xai_grok_tools::implementations::grok_build::task::authority_log::TreeAuthorityLog;

    fn ctx() -> AuthorityProjectionContext {
        AuthorityProjectionContext {
            task_tree_id: "tree-proj".into(),
            owner_session_id: "root-session".into(),
            policy_revision: 1,
        }
    }

    #[test]
    fn kind_map_covers_all_coordinator_kinds() {
        for kind in [
            AuthorityEventKind::SpawnReserved,
            AuthorityEventKind::SpawnClaimed,
            AuthorityEventKind::TerminalSucceeded,
            AuthorityEventKind::TerminalFailed,
            AuthorityEventKind::Cancelled,
        ] {
            let mapped = map_authority_kind(kind).unwrap();
            assert_eq!(
                mapped.is_terminal(),
                kind.is_terminal(),
                "{kind:?} terminal parity"
            );
        }
        assert_eq!(
            map_authority_kind(AuthorityEventKind::SpawnReserved).unwrap(),
            GovernedLifecycleEventKind::Ready
        );
        assert_eq!(
            map_authority_kind(AuthorityEventKind::SpawnClaimed).unwrap(),
            GovernedLifecycleEventKind::Running
        );
    }

    #[test]
    fn evidence_keeps_coord_kind_lossless() {
        let event = AuthorityEvent {
            schema_version: 1,
            sequence: 0,
            node_id: "n1".into(),
            operation_id: "op:n1".into(),
            kind: AuthorityEventKind::SpawnReserved,
            reservation_id: Some("ledger:9".into()),
        };
        let refs = evidence_refs_for(&event);
        assert!(refs.iter().any(|r| r == "op:op:n1"));
        assert!(refs.iter().any(|r| r == "coord_kind:spawn_reserved"));
        assert!(refs.iter().any(|r| r == "reservation:ledger:9"));
        assert_eq!(
            recover_authority_kind(GovernedLifecycleEventKind::Ready, &refs),
            Some(AuthorityEventKind::SpawnReserved)
        );
    }

    #[test]
    fn authority_trail_projects_into_lifecycle_journal() {
        let mut log = TreeAuthorityLog::in_memory();
        log.append(
            "child",
            "op-child",
            AuthorityEventKind::SpawnReserved,
            Some("ledger:1".into()),
        )
        .unwrap();
        log.append(
            "child",
            "op-child",
            AuthorityEventKind::SpawnClaimed,
            Some("ledger:1".into()),
        )
        .unwrap();
        log.append(
            "child",
            "op-child",
            AuthorityEventKind::TerminalSucceeded,
            None,
        )
        .unwrap();

        let mut journal = LifecycleJournal::in_memory("tree-proj");
        let written =
            project_authority_trail(&mut journal, &ctx(), log.events(), 1_700_000_000).unwrap();
        assert_eq!(written, 3);
        assert_eq!(journal.events().len(), 3);
        assert_eq!(
            journal.events()[0].kind,
            GovernedLifecycleEventKind::Ready
        );
        assert_eq!(
            journal.events()[1].kind,
            GovernedLifecycleEventKind::Running
        );
        assert_eq!(
            journal.events()[2].kind,
            GovernedLifecycleEventKind::TerminalSucceeded
        );
        assert!(journal.derived_state().terminal);
        // No-revival holds on the projected journal.
        assert!(matches!(
            project_authority_trail(
                &mut journal,
                &ctx(),
                &[AuthorityEvent {
                    schema_version: 1,
                    sequence: 99,
                    node_id: "child".into(),
                    operation_id: "op-child".into(),
                    kind: AuthorityEventKind::SpawnReserved,
                    reservation_id: None,
                }],
                1_700_000_100,
            ),
            Err(AuthorityProjectionError::Journal(
                JournalError::LateEventAfterTerminal { .. }
            ))
        ));
        // Reverse: coord_kind recovers SpawnReserved even though lifecycle says Ready.
        assert_eq!(
            recover_authority_kind(
                journal.events()[0].kind,
                &journal.events()[0].evidence_refs
            ),
            Some(AuthorityEventKind::SpawnReserved)
        );
    }

    #[test]
    fn projected_payload_hash_is_canonical() {
        let event = AuthorityEvent {
            schema_version: 1,
            sequence: 0,
            node_id: "n".into(),
            operation_id: "op".into(),
            kind: AuthorityEventKind::Cancelled,
            reservation_id: None,
        };
        let a = project_authority_event(&ctx(), &event, None, 42).unwrap();
        let b = project_authority_event(&ctx(), &event, None, 42).unwrap();
        assert_eq!(a.payload_hash, b.payload_hash);
        assert!(a.payload_hash.starts_with("sha256:"));
        // Journal accepts the projected event.
        let mut journal = LifecycleJournal::in_memory("tree-proj");
        journal.append(a).unwrap();
        assert!(journal.derived_state().terminal);
    }
}
