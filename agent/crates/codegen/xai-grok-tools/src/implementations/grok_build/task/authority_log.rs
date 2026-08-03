//! Coordinator authority event log (NG-03C wiring).
//!
//! In-process append trail the coordinator writes on spawn/settle/cancel.
//! Fail-closed: per-operation no-revival, monotonic sequence. Optional JSONL
//! persistence next to the operation store so a process restart keeps the
//! event trail.
//!
//! Full NG-00 canonical `LifecycleJournal` remains in `xai-grok-memory` for
//! offline compose evidence (tools cannot depend on memory without a crate
//! cycle). Schema unification is the **kind mapping** below plus the memory
//! projection that builds `GovernedLifecycleEventV1` from these records —
//! not a shared crate dependency and not a single physical log (INV-21).

use std::collections::HashSet;
use std::fs::OpenOptions;
use std::io::{BufRead, BufReader, Write};
use std::path::PathBuf;

use serde::{Deserialize, Serialize};

/// Wire schema for coordinator authority JSONL lines. Bump only with a
/// migration story; unknown higher versions fail closed on reload.
pub const AUTHORITY_EVENT_SCHEMA_VERSION: u16 = 1;

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

    /// Snake-case identity of this coordinator kind (stable wire token).
    pub fn as_str(self) -> &'static str {
        match self {
            AuthorityEventKind::SpawnReserved => "spawn_reserved",
            AuthorityEventKind::SpawnClaimed => "spawn_claimed",
            AuthorityEventKind::TerminalSucceeded => "terminal_succeeded",
            AuthorityEventKind::TerminalFailed => "terminal_failed",
            AuthorityEventKind::Cancelled => "cancelled",
        }
    }

    /// Maps onto execution-book §3.3.2 `GovernedLifecycleEventKind` wire
    /// names. Coordinator kinds that have no 1:1 lifecycle twin project to
    /// the nearest authority phase; the original coord kind is preserved in
    /// evidence as `coord_kind:` by the memory projector.
    ///
    /// | AuthorityEventKind   | lifecycle kind          |
    /// |----------------------|-------------------------|
    /// | SpawnReserved        | ready                   |
    /// | SpawnClaimed         | running                 |
    /// | TerminalSucceeded    | terminal_succeeded      |
    /// | TerminalFailed       | terminal_failed         |
    /// | Cancelled            | cancelled               |
    pub fn lifecycle_kind_str(self) -> &'static str {
        match self {
            AuthorityEventKind::SpawnReserved => "ready",
            AuthorityEventKind::SpawnClaimed => "running",
            AuthorityEventKind::TerminalSucceeded => "terminal_succeeded",
            AuthorityEventKind::TerminalFailed => "terminal_failed",
            AuthorityEventKind::Cancelled => "cancelled",
        }
    }

    /// Inverse of [`Self::lifecycle_kind_str`] for **lossless** coordinator
    /// kinds only (terminals + cancelled). Phase projections that collapse
    /// multiple coord kinds are recovered via `coord_kind:` evidence.
    pub fn from_lifecycle_kind_str(kind: &str) -> Option<Self> {
        match kind {
            "terminal_succeeded" => Some(AuthorityEventKind::TerminalSucceeded),
            "terminal_failed" => Some(AuthorityEventKind::TerminalFailed),
            "cancelled" => Some(AuthorityEventKind::Cancelled),
            _ => None,
        }
    }

    pub fn from_str_token(token: &str) -> Option<Self> {
        match token {
            "spawn_reserved" => Some(AuthorityEventKind::SpawnReserved),
            "spawn_claimed" => Some(AuthorityEventKind::SpawnClaimed),
            "terminal_succeeded" => Some(AuthorityEventKind::TerminalSucceeded),
            "terminal_failed" => Some(AuthorityEventKind::TerminalFailed),
            "cancelled" => Some(AuthorityEventKind::Cancelled),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AuthorityEvent {
    /// Defaults to 1 for lines written before the field existed.
    #[serde(default = "default_authority_schema_version")]
    pub schema_version: u16,
    pub sequence: u64,
    pub node_id: String,
    pub operation_id: String,
    pub kind: AuthorityEventKind,
    pub reservation_id: Option<String>,
}

fn default_authority_schema_version() -> u16 {
    AUTHORITY_EVENT_SCHEMA_VERSION
}

/// Per-tree authority trail. Sequence is tree-monotonic; terminal is
/// **per operation_id** so siblings can complete/cancel independently.
#[derive(Debug, Clone, Default)]
pub struct TreeAuthorityLog {
    events: Vec<AuthorityEvent>,
    next_sequence: u64,
    terminal_ops: HashSet<String>,
    path: Option<PathBuf>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AuthorityLogError {
    LateEventAfterTerminal {
        operation_id: String,
        kind: AuthorityEventKind,
    },
    Io(String),
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
            AuthorityLogError::Io(message) => write!(f, "authority log I/O failed: {message}"),
        }
    }
}

impl TreeAuthorityLog {
    pub fn in_memory() -> Self {
        Self::default()
    }

    /// Load or create a durable JSONL log at `path`. Corrupt lines stop the
    /// load (fail-closed to the last good prefix); the next append continues
    /// from the highest recovered sequence.
    pub fn at_path(path: impl Into<PathBuf>) -> Self {
        let path = path.into();
        let mut log = Self {
            events: Vec::new(),
            next_sequence: 0,
            terminal_ops: HashSet::new(),
            path: Some(path.clone()),
        };
        if let Ok(file) = std::fs::File::open(&path) {
            for line in BufReader::new(file).lines() {
                let Ok(line) = line else { break };
                if line.trim().is_empty() {
                    continue;
                }
                let Ok(event) = serde_json::from_str::<AuthorityEvent>(&line) else {
                    break;
                };
                // Unknown future schema: stop at last good prefix (fail-closed).
                if event.schema_version > AUTHORITY_EVENT_SCHEMA_VERSION {
                    break;
                }
                if event.kind.is_terminal() {
                    log.terminal_ops.insert(event.operation_id.clone());
                }
                log.next_sequence = log.next_sequence.max(event.sequence.saturating_add(1));
                log.events.push(event);
            }
        }
        log
    }

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
        let event = AuthorityEvent {
            schema_version: AUTHORITY_EVENT_SCHEMA_VERSION,
            sequence,
            node_id: node_id.into(),
            operation_id,
            kind,
            reservation_id,
        };
        if let Some(path) = &self.path {
            if let Some(parent) = path.parent() {
                std::fs::create_dir_all(parent)
                    .map_err(|error| AuthorityLogError::Io(error.to_string()))?;
            }
            let mut file = OpenOptions::new()
                .create(true)
                .append(true)
                .open(path)
                .map_err(|error| AuthorityLogError::Io(error.to_string()))?;
            let line = serde_json::to_string(&event)
                .map_err(|error| AuthorityLogError::Io(error.to_string()))?;
            file.write_all(line.as_bytes())
                .and_then(|_| file.write_all(b"\n"))
                .and_then(|_| file.flush())
                .map_err(|error| AuthorityLogError::Io(error.to_string()))?;
        }
        self.events.push(event);
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
        let mut log = TreeAuthorityLog::in_memory();
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

    #[test]
    fn jsonl_round_trips_through_disk() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("auth.jsonl");
        {
            let mut log = TreeAuthorityLog::at_path(&path);
            log.append("n1", "op:n1", AuthorityEventKind::SpawnReserved, Some("ledger:1".into()))
                .unwrap();
            log.append("n1", "op:n1", AuthorityEventKind::SpawnClaimed, Some("ledger:1".into()))
                .unwrap();
            log.append("n1", "op:n1", AuthorityEventKind::TerminalSucceeded, None)
                .unwrap();
        }
        let mut reloaded = TreeAuthorityLog::at_path(&path);
        assert_eq!(reloaded.events().len(), 3);
        assert!(reloaded.is_operation_terminal("op:n1"));
        assert_eq!(
            reloaded.last_kind_for("op:n1"),
            Some(AuthorityEventKind::TerminalSucceeded)
        );
        assert_eq!(
            reloaded.events()[0].schema_version,
            AUTHORITY_EVENT_SCHEMA_VERSION
        );
        assert_eq!(
            reloaded.append("n1", "op:n1", AuthorityEventKind::SpawnReserved, None),
            Err(AuthorityLogError::LateEventAfterTerminal {
                operation_id: "op:n1".into(),
                kind: AuthorityEventKind::SpawnReserved
            })
        );
    }

    #[test]
    fn lifecycle_kind_mapping_is_stable_and_invertible_for_terminals() {
        assert_eq!(
            AuthorityEventKind::SpawnReserved.lifecycle_kind_str(),
            "ready"
        );
        assert_eq!(
            AuthorityEventKind::SpawnClaimed.lifecycle_kind_str(),
            "running"
        );
        assert_eq!(
            AuthorityEventKind::TerminalSucceeded.lifecycle_kind_str(),
            "terminal_succeeded"
        );
        assert_eq!(
            AuthorityEventKind::TerminalFailed.lifecycle_kind_str(),
            "terminal_failed"
        );
        assert_eq!(AuthorityEventKind::Cancelled.lifecycle_kind_str(), "cancelled");

        for kind in [
            AuthorityEventKind::TerminalSucceeded,
            AuthorityEventKind::TerminalFailed,
            AuthorityEventKind::Cancelled,
        ] {
            assert_eq!(
                AuthorityEventKind::from_lifecycle_kind_str(kind.lifecycle_kind_str()),
                Some(kind)
            );
        }
        // Collapsed phases are not invertible from lifecycle alone.
        assert_eq!(
            AuthorityEventKind::from_lifecycle_kind_str("ready"),
            None
        );
        assert_eq!(
            AuthorityEventKind::from_lifecycle_kind_str("running"),
            None
        );
        assert_eq!(
            AuthorityEventKind::from_str_token("spawn_reserved"),
            Some(AuthorityEventKind::SpawnReserved)
        );
    }

    #[test]
    fn legacy_jsonl_without_schema_version_still_loads() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("legacy.jsonl");
        std::fs::write(
            &path,
            r#"{"sequence":0,"node_id":"n1","operation_id":"op:n1","kind":"spawn_reserved","reservation_id":"r1"}
"#,
        )
        .unwrap();
        let log = TreeAuthorityLog::at_path(&path);
        assert_eq!(log.events().len(), 1);
        assert_eq!(log.events()[0].schema_version, AUTHORITY_EVENT_SCHEMA_VERSION);
        assert_eq!(log.events()[0].kind, AuthorityEventKind::SpawnReserved);
    }

    #[test]
    fn future_schema_version_stops_reload_at_last_good_prefix() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("future.jsonl");
        // One good v1 line, then a v2 line from a future binary.
        std::fs::write(
            &path,
            format!(
                r#"{{"schema_version":1,"sequence":0,"node_id":"n1","operation_id":"op:good","kind":"spawn_reserved","reservation_id":"r1"}}
{{"schema_version":2,"sequence":1,"node_id":"n1","operation_id":"op:future","kind":"spawn_claimed","reservation_id":"r2"}}
"#
            ),
        )
        .unwrap();
        let log = TreeAuthorityLog::at_path(&path);
        // Fail-closed: unknown future schema must not be imported as truth.
        assert_eq!(log.events().len(), 1, "future-schema line must not load");
        assert_eq!(log.events()[0].operation_id, "op:good");
        // Sequence stays ahead of the loaded prefix so a later v1 append
        // cannot collide with a future line we refused to read.
        let mut log = log;
        let seq = log
            .append("n1", "op:good", AuthorityEventKind::SpawnClaimed, None)
            .unwrap();
        assert_eq!(seq, 1, "next sequence must follow the loaded prefix");
    }
}
