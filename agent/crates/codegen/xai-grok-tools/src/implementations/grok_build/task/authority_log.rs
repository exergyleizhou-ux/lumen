//! Coordinator-local authority event log (NG-03C wiring).
//!
//! Full NG-00 canonical encoding + disk JSONL live in `xai-grok-memory::LifecycleJournal`.
//! This log is the **in-process** append trail the coordinator writes on
//! spawn/settle/cancel so the shipped path has a fail-closed event sequence
//! without a tools→memory crate cycle. Same no-revival / monotonic rules.

use std::collections::HashSet;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AuthorityEventKind {
    SpawnReserved,
    SpawnClaimed,
    TerminalSucceeded,
    TerminalFailed,
    Cancelled,
}

impl AuthorityEventKind {
    pub fn is_terminal(self) -> bool {
        matches!(
            self,
            AuthorityEventKind::TerminalSucceeded
                | AuthorityEventKind::TerminalFailed
                | AuthorityEventKind::Cancelled
        )
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AuthorityEvent {
    pub sequence: u64,
    pub node_id: String,
    pub operation_id: String,
    pub kind: AuthorityEventKind,
    pub reservation_id: Option<String>,
}

/// Per-tree authority trail. Sequence is tree-monotonic; terminal is
/// **per operation_id** so siblings can complete/cancel independently.
#[derive(Debug, Clone, Default)]
pub struct TreeAuthorityLog {
    events: Vec<AuthorityEvent>,
    next_sequence: u64,
    terminal_ops: HashSet<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AuthorityLogError {
    LateEventAfterTerminal {
        operation_id: String,
        kind: AuthorityEventKind,
    },
}

impl std::fmt::Display for AuthorityLogError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AuthorityLogError::LateEventAfterTerminal {
                operation_id,
                kind,
            } => write!(
                f,
                "late authority event {kind:?} after terminal for {operation_id}"
            ),
        }
    }
}

impl TreeAuthorityLog {
    pub fn append(
        &mut self,
        node_id: impl Into<String>,
        operation_id: impl Into<String>,
        kind: AuthorityEventKind,
        reservation_id: Option<String>,
    ) -> Result<u64, AuthorityLogError> {
        let operation_id = operation_id.into();
        if self.terminal_ops.contains(&operation_id) {
            return Err(AuthorityLogError::LateEventAfterTerminal {
                operation_id,
                kind,
            });
        }
        let sequence = self.next_sequence;
        self.next_sequence = self.next_sequence.saturating_add(1);
        if kind.is_terminal() {
            self.terminal_ops.insert(operation_id.clone());
        }
        self.events.push(AuthorityEvent {
            sequence,
            node_id: node_id.into(),
            operation_id,
            kind,
            reservation_id,
        });
        Ok(sequence)
    }

    pub fn events(&self) -> &[AuthorityEvent] {
        &self.events
    }

    pub fn is_operation_terminal(&self, operation_id: &str) -> bool {
        self.terminal_ops.contains(operation_id)
    }

    pub fn last_kind_for(&self, operation_id: &str) -> Option<AuthorityEventKind> {
        self.events
            .iter()
            .rev()
            .find(|e| e.operation_id == operation_id)
            .map(|e| e.kind)
    }

    pub fn events_for(&self, operation_id: &str) -> Vec<&AuthorityEvent> {
        self.events
            .iter()
            .filter(|e| e.operation_id == operation_id)
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn append_is_monotonic_and_terminal_blocks_revival_per_op() {
        let mut log = TreeAuthorityLog::default();
        log.append("n1", "op:n1", AuthorityEventKind::SpawnReserved, Some("res:1".into()))
            .unwrap();
        log.append("n1", "op:n1", AuthorityEventKind::SpawnClaimed, Some("res:1".into()))
            .unwrap();
        log.append("n1", "op:n1", AuthorityEventKind::TerminalSucceeded, None)
            .unwrap();
        assert!(log.is_operation_terminal("op:n1"));
        assert_eq!(
            log.append("n1", "op:n1", AuthorityEventKind::SpawnReserved, None),
            Err(AuthorityLogError::LateEventAfterTerminal {
                operation_id: "op:n1".into(),
                kind: AuthorityEventKind::SpawnReserved
            })
        );
        // Sibling operation may still progress.
        log.append("n2", "op:n2", AuthorityEventKind::SpawnReserved, None)
            .unwrap();
        assert_eq!(log.events().len(), 4);
        assert_eq!(log.events()[0].sequence, 0);
        assert_eq!(log.events()[3].sequence, 3);
    }
}
