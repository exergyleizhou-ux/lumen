//! NG-03C — append-only lifecycle event journal (K2: state derived from the
//! log, never invented).
//!
//! `GovernedOperationStore` persists a *state snapshot*; this journal is the
//! *authority event log*: every authority mutation is appended as a typed
//! event, and the read model is derived from the event set. Same event set →
//! same derived state (deterministic), a gap/duplicate/foreign-owner event
//! fails closed, and a terminal event cannot be followed by a revival.
//!
//! Physically this is a single JSONL file per tree; the logical envelope is
//! what is uniform, not a giant shared log (see INV-21).

use std::fs::OpenOptions;
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue, ENCODING_REVISION};

/// Logical authority event kinds (execution book §3.3.2).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum GovernedLifecycleEventKind {
    Booting,
    Ready,
    PromptAccepted,
    Running,
    Blocked,
    Checkpointed,
    TerminalSucceeded,
    TerminalFailed,
    Cancelled,
    Reconciled,
    Frozen,
}

impl GovernedLifecycleEventKind {
    fn as_str(self) -> &'static str {
        match self {
            GovernedLifecycleEventKind::Booting => "booting",
            GovernedLifecycleEventKind::Ready => "ready",
            GovernedLifecycleEventKind::PromptAccepted => "prompt_accepted",
            GovernedLifecycleEventKind::Running => "running",
            GovernedLifecycleEventKind::Blocked => "blocked",
            GovernedLifecycleEventKind::Checkpointed => "checkpointed",
            GovernedLifecycleEventKind::TerminalSucceeded => "terminal_succeeded",
            GovernedLifecycleEventKind::TerminalFailed => "terminal_failed",
            GovernedLifecycleEventKind::Cancelled => "cancelled",
            GovernedLifecycleEventKind::Reconciled => "reconciled",
            GovernedLifecycleEventKind::Frozen => "frozen",
        }
    }

    pub fn is_terminal(self) -> bool {
        matches!(
            self,
            GovernedLifecycleEventKind::TerminalSucceeded
                | GovernedLifecycleEventKind::TerminalFailed
                | GovernedLifecycleEventKind::Cancelled
                | GovernedLifecycleEventKind::Frozen
        )
    }
}

/// Who emitted the event. UI/log are projections, never sources.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum GovernedLifecycleEventSource {
    Actor,
    Scheduler,
    TerminalAdapter,
    WorkflowAdapter,
}

/// One authority event (execution book §3.3.2).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GovernedLifecycleEventV1 {
    pub event_id: String,
    pub task_tree_id: String,
    pub node_id: String,
    pub owner_session_id: String,
    pub sequence: u64,
    pub causal_parent: Option<u64>,
    pub kind: GovernedLifecycleEventKind,
    pub source: GovernedLifecycleEventSource,
    pub lease_id: Option<String>,
    pub contract_hash: Option<String>,
    pub policy_revision: u64,
    pub evidence_refs: Vec<String>,
    pub occurred_at: u64,
    /// Canonical payload hash (NG-00): commits the event body without a
    /// second serialization convention.
    pub payload_hash: String,
}

impl GovernedLifecycleEventV1 {
    /// Canonical preimage of the event identity + payload reference.
    pub fn canonical_bytes(&self) -> Result<Vec<u8>, CanonicalError> {
        let record = CanonicalRecord::new("lifecycle-event")
            .field("event_id", CanonicalValue::str(&self.event_id))
            .field("task_tree_id", CanonicalValue::str(&self.task_tree_id))
            .field("node_id", CanonicalValue::str(&self.node_id))
            .field("owner_session_id", CanonicalValue::str(&self.owner_session_id))
            .field("sequence", CanonicalValue::U64(self.sequence))
            .field(
                "causal_parent",
                self.causal_parent
                    .map(CanonicalValue::U64)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field("kind", CanonicalValue::str(self.kind.as_str()))
            .field(
                "source",
                CanonicalValue::str(match self.source {
                    GovernedLifecycleEventSource::Actor => "actor",
                    GovernedLifecycleEventSource::Scheduler => "scheduler",
                    GovernedLifecycleEventSource::TerminalAdapter => "terminal_adapter",
                    GovernedLifecycleEventSource::WorkflowAdapter => "workflow_adapter",
                }),
            )
            .field(
                "lease_id",
                self.lease_id
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field(
                "contract_hash",
                self.contract_hash
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            )
            .field("policy_revision", CanonicalValue::U64(self.policy_revision))
            .field(
                "evidence_refs",
                CanonicalValue::Seq(
                    self.evidence_refs
                        .iter()
                        .map(|reference| CanonicalValue::str(reference))
                        .collect(),
                ),
            )
            .field("occurred_at", CanonicalValue::U64(self.occurred_at));
        record.canonical_bytes()
    }

    pub fn compute_payload_hash(&self) -> Result<String, CanonicalError> {
        let digest = Sha256::digest(self.canonical_bytes()?);
        Ok(format!("sha256:{digest:x}"))
    }
}

/// Append-only journal for one task tree. `append` enforces monotonic
/// sequence, causal-parent linkage and the no-revival rule; `derived_state`
/// is the K2 read model.
#[derive(Debug, Clone)]
pub struct LifecycleJournal {
    root_tree_id: String,
    path: Option<PathBuf>,
    events: Vec<GovernedLifecycleEventV1>,
    next_sequence: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum JournalError {
    ForeignTree,
    SequenceGap { expected: u64, got: u64 },
    DuplicateSequence { sequence: u64 },
    UnknownCausalParent { parent: u64 },
    LateEventAfterTerminal { kind: GovernedLifecycleEventKind },
    PayloadHashMismatch,
    Io(String),
}

impl std::fmt::Display for JournalError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            JournalError::ForeignTree => write!(f, "event belongs to another task tree"),
            JournalError::SequenceGap { expected, got } => {
                write!(f, "sequence gap: expected {expected}, got {got}")
            }
            JournalError::DuplicateSequence { sequence } => {
                write!(f, "duplicate sequence {sequence}")
            }
            JournalError::UnknownCausalParent { parent } => {
                write!(f, "unknown causal parent {parent}")
            }
            JournalError::LateEventAfterTerminal { kind } => {
                write!(f, "late event {kind:?} after terminal state")
            }
            JournalError::PayloadHashMismatch => {
                write!(f, "payload hash does not match the canonical preimage")
            }
            JournalError::Io(message) => write!(f, "journal I/O failed: {message}"),
        }
    }
}

/// Read model derived from the event set (K2: same events → same state).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DerivedOperationState {
    pub current: Option<GovernedLifecycleEventKind>,
    pub last_sequence: u64,
    pub terminal: bool,
}

impl LifecycleJournal {
    pub fn in_memory(root_tree_id: impl Into<String>) -> Self {
        Self {
            root_tree_id: root_tree_id.into(),
            path: None,
            events: Vec::new(),
            next_sequence: 0,
        }
    }

    pub fn at_path(root_tree_id: impl Into<String>, path: impl Into<PathBuf>) -> Self {
        let root_tree_id = root_tree_id.into();
        let path = path.into();
        let mut journal = Self::in_memory(root_tree_id.clone());
        journal.path = Some(path.clone());
        if let Ok(file) = std::fs::File::open(&path) {
            let reader = BufReader::new(file);
            for line in reader.lines() {
                let Ok(line) = line else { break };
                let Ok(event) = serde_json::from_str::<GovernedLifecycleEventV1>(&line) else {
                    break;
                };
                if event.task_tree_id != journal.root_tree_id {
                    continue;
                }
                journal.next_sequence = journal.next_sequence.max(event.sequence + 1);
                journal.events.push(event);
            }
        }
        journal
    }

    pub fn events(&self) -> &[GovernedLifecycleEventV1] {
        &self.events
    }

    /// Append one authority event. Rules (fail closed, never best-effort):
    /// same tree, exact next sequence, known causal parent, no revival after
    /// a terminal event, payload hash recomputable.
    pub fn append(&mut self, mut event: GovernedLifecycleEventV1) -> Result<(), JournalError> {
        if event.task_tree_id != self.root_tree_id {
            return Err(JournalError::ForeignTree);
        }
        if event.sequence != self.next_sequence {
            return Err(JournalError::SequenceGap {
                expected: self.next_sequence,
                got: event.sequence,
            });
        }
        if let Some(parent) = event.causal_parent {
            let known = self
                .events
                .iter()
                .any(|existing| existing.sequence == parent);
            if !known {
                return Err(JournalError::UnknownCausalParent { parent });
            }
        }
        if let Some(last) = self.events.last()
            && last.kind.is_terminal()
        {
            return Err(JournalError::LateEventAfterTerminal {
                kind: event.kind,
            });
        }
        let computed = event.compute_payload_hash().map_err(|_| {
            JournalError::PayloadHashMismatch
        })?;
        if computed != event.payload_hash {
            return Err(JournalError::PayloadHashMismatch);
        }
        if let Some(path) = &self.path {
            let mut file = OpenOptions::new()
                .create(true)
                .append(true)
                .open(path)
                .map_err(|error| JournalError::Io(error.to_string()))?;
            let line = serde_json::to_string(&event)
                .map_err(|error| JournalError::Io(error.to_string()))?;
            file.write_all(line.as_bytes())
                .and_then(|_| file.write_all(b"\n"))
                .and_then(|_| file.flush())
                .map_err(|error| JournalError::Io(error.to_string()))?;
        }
        self.next_sequence += 1;
        self.events.push(event);
        Ok(())
    }

    /// K2 read model: the current operation state is a pure function of the
    /// event set.
    pub fn derived_state(&self) -> DerivedOperationState {
        let Some(last) = self.events.last() else {
            return DerivedOperationState {
                current: None,
                last_sequence: 0,
                terminal: false,
            };
        };
        DerivedOperationState {
            current: Some(last.kind),
            last_sequence: last.sequence,
            terminal: last.kind.is_terminal(),
        }
    }
}

#[cfg(test)]
mod lifecycle_journal_tests {
    use super::*;

    fn event(
        journal: &LifecycleJournal,
        sequence: u64,
        kind: GovernedLifecycleEventKind,
        parent: Option<u64>,
        tree: &str,
    ) -> GovernedLifecycleEventV1 {
        let mut event = GovernedLifecycleEventV1 {
            event_id: format!("evt-{sequence}"),
            task_tree_id: tree.to_owned(),
            node_id: "node-a".to_owned(),
            owner_session_id: "root-session".to_owned(),
            sequence,
            causal_parent: parent,
            kind,
            source: GovernedLifecycleEventSource::Actor,
            lease_id: None,
            contract_hash: None,
            policy_revision: 1,
            evidence_refs: Vec::new(),
            occurred_at: 1_700_000_000 + sequence,
            payload_hash: String::new(),
        };
        event.payload_hash = event.compute_payload_hash().unwrap();
        let _ = journal;
        event
    }

    #[test]
    fn same_event_set_derives_same_state() {
        let mut journal = LifecycleJournal::in_memory("tree-1");
        journal
            .append(event(&journal, 0, GovernedLifecycleEventKind::PromptAccepted, None, "tree-1"))
            .unwrap();
        journal
            .append(event(&journal, 1, GovernedLifecycleEventKind::Running, Some(0), "tree-1"))
            .unwrap();
        journal
            .append(
                event(
                    &journal,
                    2,
                    GovernedLifecycleEventKind::TerminalSucceeded,
                    Some(1),
                    "tree-1",
                ),
            )
            .unwrap();
        let state = journal.derived_state();
        assert_eq!(
            state.current,
            Some(GovernedLifecycleEventKind::TerminalSucceeded)
        );
        assert!(state.terminal);
        assert_eq!(state.last_sequence, 2);
        // Deterministic: replaying the same events yields the same state.
        let mut replay = LifecycleJournal::in_memory("tree-1");
        for event in journal.events().iter().cloned() {
            replay.append(event).unwrap();
        }
        assert_eq!(replay.derived_state(), state);
    }

    #[test]
    fn sequence_gap_and_duplicate_fail_closed() {
        let mut journal = LifecycleJournal::in_memory("tree-1");
        journal
            .append(event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-1"))
            .unwrap();
        assert_eq!(
            journal.append(event(&journal, 2, GovernedLifecycleEventKind::Running, None, "tree-1")),
            Err(JournalError::SequenceGap { expected: 1, got: 2 })
        );
        assert_eq!(
            journal.append(event(&journal, 0, GovernedLifecycleEventKind::Running, None, "tree-1")),
            Err(JournalError::SequenceGap { expected: 1, got: 0 })
        );
        assert_eq!(
            journal.append(event(&journal, 1, GovernedLifecycleEventKind::Running, Some(7), "tree-1")),
            Err(JournalError::UnknownCausalParent { parent: 7 })
        );
    }

    #[test]
    fn foreign_tree_event_is_rejected() {
        let mut journal = LifecycleJournal::in_memory("tree-1");
        assert_eq!(
            journal.append(event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-2")),
            Err(JournalError::ForeignTree)
        );
    }

    #[test]
    fn terminal_event_cannot_be_revived() {
        let mut journal = LifecycleJournal::in_memory("tree-1");
        journal
            .append(event(&journal, 0, GovernedLifecycleEventKind::Frozen, None, "tree-1"))
            .unwrap();
        assert_eq!(
            journal.append(event(&journal, 1, GovernedLifecycleEventKind::Running, Some(0), "tree-1")),
            Err(JournalError::LateEventAfterTerminal {
                kind: GovernedLifecycleEventKind::Running
            })
        );
    }

    #[test]
    fn tampered_payload_hash_is_rejected() {
        let mut journal = LifecycleJournal::in_memory("tree-1");
        let mut tampered = event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-1");
        tampered.payload_hash = "sha256:forged".to_owned();
        assert_eq!(
            journal.append(tampered),
            Err(JournalError::PayloadHashMismatch)
        );
    }

    #[test]
    fn journal_round_trips_through_disk() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("tree-1-events.jsonl");
        let mut journal = LifecycleJournal::at_path("tree-1", &path);
        journal
            .append(event(&journal, 0, GovernedLifecycleEventKind::PromptAccepted, None, "tree-1"))
            .unwrap();
        journal
            .append(event(&journal, 1, GovernedLifecycleEventKind::Blocked, Some(0), "tree-1"))
            .unwrap();
        drop(journal);

        let reloaded = LifecycleJournal::at_path("tree-1", &path);
        assert_eq!(reloaded.events().len(), 2);
        assert_eq!(
            reloaded.derived_state().current,
            Some(GovernedLifecycleEventKind::Blocked)
        );
        // The read model must be derivable from the log alone.
        assert_eq!(reloaded.derived_state().last_sequence, 1);
    }
}
