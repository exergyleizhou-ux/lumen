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
use std::path::PathBuf;

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::canonical::{CanonicalError, CanonicalRecord, CanonicalValue};

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
    /// Session context was structurally reset (compaction or explicit
    /// cache-reset point, DEBT-033 A2-c). `detail` carries
    /// `{reason, old_epoch, new_epoch}` when available.
    ContextReset,
    /// Stale tool output was archived and shortened. `detail` carries
    /// `{original_sequence, content_hash, causal_parent}` per snipped record.
    ToolResultSnip,
    /// One cache-health observation. `detail` carries
    /// `{prompt_tokens, hit_tokens, miss_tokens, output_tokens, hit_ratio, truth}`.
    CacheHealthSample,
}

impl GovernedLifecycleEventKind {
    pub fn as_str(self) -> &'static str {
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
            GovernedLifecycleEventKind::ContextReset => "context_reset",
            GovernedLifecycleEventKind::ToolResultSnip => "tool_result_snip",
            GovernedLifecycleEventKind::CacheHealthSample => "cache_health_sample",
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
    /// Tamper-evident chain link (DEBT-031, absorbed from block/buzz
    /// buzz-audit): the payload hash of the PREVIOUS event in this tree's
    /// journal. The journal (the single writer) fills this on append and
    /// recomputes the payload hash over `(body, prev)`; a forger who rewrites
    /// a middle event cannot recompute the downstream chain. Legacy v1
    /// records decode as `None` — the chain is re-established from the first
    /// v2 append. The tree id is already folded into the payload preimage,
    /// so a chain can never be spliced across trees.
    #[serde(default)]
    pub prev_payload_hash: Option<String>,
    /// Kind-specific structured payload (DEBT-033 A2-c): context reset
    /// reason/epochs, snipped-record hash chain, cache-health numbers.
    /// `None` for events without a payload; legacy records decode as `None`.
    #[serde(default)]
    pub detail: Option<serde_json::Value>,
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
                        .map(CanonicalValue::str)
                        .collect(),
                ),
            )
            .field("occurred_at", CanonicalValue::U64(self.occurred_at))
            .field(
                "prev_payload_hash",
                self.prev_payload_hash
                    .as_deref()
                    .map(CanonicalValue::str)
                    .unwrap_or(CanonicalValue::Null),
            );
        // DEBT-033 A2-c: kind-specific detail. Encoded as a deterministic JSON
        // string (serde_json::Map sorts keys) so floats stay representable.
        // Only included when present: legacy events keep byte-identical
        // canonical preimages and their stored payload hashes still verify.
        let record = match &self.detail {
            Some(detail) => record.field(
                "detail",
                CanonicalValue::str(
                    serde_json::to_string(detail).expect("serde_json::Value is serializable"),
                ),
            ),
            None => record,
        };
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
    /// Tamper-evident chain broken (DEBT-031): the appended event's
    /// `prev_payload_hash` does not match the previous event, or a genesis
    /// event claims a predecessor. Fail-closed — the journal refuses rather
    /// than guessing.
    ChainBroken { sequence: u64 },
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
            JournalError::ChainBroken { sequence } => {
                write!(f, "tamper-evident chain broken at sequence {sequence}")
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

    /// Verify the tamper-evident chain over the whole journal (DEBT-031):
    /// every event's `prev_payload_hash` must equal the previous event's
    /// `payload_hash`, and the genesis event must have no predecessor. This
    /// is the read-side check a third-party verifier runs over the exported
    /// evidence bundle — a rewrite of any event (even the genesis, whose
    /// change invalidates the next event's link) is detected structurally.
    pub fn verify_chain(&self) -> Result<(), JournalError> {
        let mut prev: Option<&str> = None;
        for event in &self.events {
            match (prev, event.prev_payload_hash.as_deref()) {
                (Some(expected), Some(got)) if got == expected => {}
                (Some(_), None) => {
                    return Err(JournalError::ChainBroken {
                        sequence: event.sequence,
                    })
                }
                (Some(_), Some(_)) => {
                    return Err(JournalError::ChainBroken {
                        sequence: event.sequence,
                    })
                }
                (None, None) => {}
                (None, Some(_)) => {
                    return Err(JournalError::ChainBroken {
                        sequence: event.sequence,
                    })
                }
            }
            prev = Some(&event.payload_hash);
        }
        Ok(())
    }

    /// Append one authority event. Rules (fail closed, never best-effort):
    /// same tree, exact next sequence, known causal parent, no revival after
    /// a terminal event, payload hash recomputable, tamper-evident chain
    /// intact. The journal is the single writer: it fills `prev_payload_hash`
    /// from the previous event and recomputes the payload hash over
    /// `(body, prev)`, so the chain is structural, not conventional.
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
        // Tamper-evident chain (DEBT-031): an explicit wrong predecessor is
        // refused outright; a missing predecessor is filled by the journal
        // (the caller cannot know the previous hash without holding the
        // journal). Genesis (no previous event) must not claim a predecessor.
        match self.events.last() {
            Some(last) => {
                if let Some(got) = &event.prev_payload_hash
                    && got != &last.payload_hash
                {
                    return Err(JournalError::ChainBroken {
                        sequence: event.sequence,
                    });
                }
                event.prev_payload_hash = Some(last.payload_hash.clone());
            }
            None => {
                if event.prev_payload_hash.is_some() {
                    return Err(JournalError::ChainBroken {
                        sequence: event.sequence,
                    });
                }
            }
        }
        // The body hash is recomputed AFTER chaining: the committed hash now
        // covers (body, prev), so rewriting any middle event invalidates
        // every downstream hash.
        let computed = event
            .compute_payload_hash()
            .map_err(|_| JournalError::PayloadHashMismatch)?;
        event.payload_hash = computed;
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
            prev_payload_hash: None,
            detail: None,
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
    fn caller_forged_hash_is_overridden_by_journal_authority() {
        // The journal is the single writer: a caller-supplied payload hash
        // is never trusted — the journal recomputes it over (body, prev).
        // The old reject semantics are superseded by authority recomputation.
        let mut journal = LifecycleJournal::in_memory("tree-1");
        let mut forged = event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-1");
        forged.payload_hash = "sha256:forged".to_owned();
        journal.append(forged).expect("authority overrides the forged hash");
        let committed = &journal.events()[0];
        assert_ne!(committed.payload_hash, "sha256:forged");
        assert_eq!(committed.prev_payload_hash, None, "genesis has no predecessor");
        journal.verify_chain().expect("chain intact");
    }

    #[test]
    fn chain_links_events_and_verifies() {
        let mut journal = LifecycleJournal::in_memory("tree-1");
        journal
            .append(event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-1"))
            .unwrap();
        journal
            .append(event(&journal, 1, GovernedLifecycleEventKind::PromptAccepted, Some(0), "tree-1"))
            .unwrap();
        journal
            .append(event(&journal, 2, GovernedLifecycleEventKind::Running, Some(1), "tree-1"))
            .unwrap();
        assert_eq!(
            journal.events()[1].prev_payload_hash.as_deref(),
            Some(journal.events()[0].payload_hash.as_str()),
            "each event chains to the previous payload hash"
        );
        assert_eq!(
            journal.events()[2].prev_payload_hash.as_deref(),
            Some(journal.events()[1].payload_hash.as_str())
        );
        journal.verify_chain().expect("chain verifies");
    }

    #[test]
    fn rewriting_a_middle_event_breaks_the_chain() {
        // Self-consistent forgery: the attacker rewrites a middle event AND
        // recomputes its own payload hash — but cannot recompute the
        // downstream link, so the chain still breaks.
        let mut journal = LifecycleJournal::in_memory("tree-1");
        journal
            .append(event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-1"))
            .unwrap();
        journal
            .append(event(&journal, 1, GovernedLifecycleEventKind::PromptAccepted, Some(0), "tree-1"))
            .unwrap();
        journal
            .append(event(&journal, 2, GovernedLifecycleEventKind::Running, Some(1), "tree-1"))
            .unwrap();
        // Forge event 1: change its body and recompute its own hash.
        let mut forged = journal.events()[1].clone();
        forged.node_id = "node-compromised".to_owned();
        forged.payload_hash = forged.compute_payload_hash().unwrap();
        let mut tampered = LifecycleJournal::in_memory("tree-1");
        tampered.events.push(journal.events()[0].clone());
        tampered.events.push(forged);
        tampered.events.push(journal.events()[2].clone());
        tampered.next_sequence = 3;
        assert_eq!(
            tampered.verify_chain().unwrap_err(),
            JournalError::ChainBroken { sequence: 2 },
            "event 2's link points at the ORIGINAL hash — the forgery is detected"
        );
    }

    #[test]
    fn rewriting_the_genesis_event_breaks_the_next_link() {
        // Even the first event is locked: its change invalidates event 1's
        // prev link. No event is outside the chain.
        let mut journal = LifecycleJournal::in_memory("tree-1");
        journal
            .append(event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-1"))
            .unwrap();
        journal
            .append(event(&journal, 1, GovernedLifecycleEventKind::PromptAccepted, Some(0), "tree-1"))
            .unwrap();
        let mut forged_genesis = journal.events()[0].clone();
        forged_genesis.owner_session_id = "compromised".to_owned();
        forged_genesis.payload_hash = forged_genesis.compute_payload_hash().unwrap();
        let mut tampered = LifecycleJournal::in_memory("tree-1");
        tampered.events.push(forged_genesis);
        tampered.events.push(journal.events()[1].clone());
        tampered.next_sequence = 2;
        assert_eq!(
            tampered.verify_chain().unwrap_err(),
            JournalError::ChainBroken { sequence: 1 }
        );
    }

    #[test]
    fn genesis_with_claimed_predecessor_and_wrong_prev_are_rejected() {
        let mut journal = LifecycleJournal::in_memory("tree-1");
        let mut genesis = event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-1");
        genesis.prev_payload_hash = Some("sha256:somewhere-else".to_owned());
        assert_eq!(
            journal.append(genesis).unwrap_err(),
            JournalError::ChainBroken { sequence: 0 },
            "a genesis event must not claim a predecessor"
        );
        journal
            .append(event(&journal, 0, GovernedLifecycleEventKind::Ready, None, "tree-1"))
            .unwrap();
        let mut second = event(&journal, 1, GovernedLifecycleEventKind::PromptAccepted, Some(0), "tree-1");
        second.prev_payload_hash = Some("sha256:wrong-prev".to_owned());
        assert_eq!(
            journal.append(second).unwrap_err(),
            JournalError::ChainBroken { sequence: 1 },
            "an explicit wrong predecessor is refused, never auto-healed"
        );
    }

    #[test]
    fn legacy_v1_events_chain_is_reestablished_on_first_v2_append() {
        // A v1 journal has no prev_payload_hash field; records decode as
        // None (read-only legacy projection). The chain re-establishes from
        // the first v2 append and verifies end-to-end.
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("legacy-events.jsonl");
        let legacy_line = r#"{"event_id":"evt-old","task_tree_id":"tree-1","node_id":"n","owner_session_id":"s","sequence":0,"causal_parent":null,"kind":"ready","source":"actor","lease_id":null,"contract_hash":null,"policy_revision":1,"evidence_refs":[],"occurred_at":1700000000,"payload_hash":"sha256:legacy-hash"}"#;
        std::fs::write(&path, format!("{legacy_line}\n")).unwrap();
        let mut journal = LifecycleJournal::at_path("tree-1", &path);
        assert_eq!(journal.events().len(), 1);
        assert_eq!(journal.events()[0].prev_payload_hash, None);
        journal
            .append(event(&journal, 1, GovernedLifecycleEventKind::PromptAccepted, Some(0), "tree-1"))
            .unwrap();
        assert_eq!(
            journal.events()[1].prev_payload_hash.as_deref(),
            Some(journal.events()[0].payload_hash.as_str()),
            "the chain links onto the legacy head"
        );
        journal.verify_chain().expect("mixed v1/v2 chain verifies");
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

    #[test]
    fn debt033_kinds_with_detail_round_trip_and_chain() {
        let mut journal = LifecycleJournal::in_memory("tree-debt033");
        let mut e1 = event(&journal, 0, GovernedLifecycleEventKind::ContextReset, None, "tree-debt033");
        e1.detail = Some(serde_json::json!({
            "reason": "explicit",
            "old_epoch": "e1",
            "new_epoch": "e2",
        }));
        let mut e2 = event(&journal, 1, GovernedLifecycleEventKind::ToolResultSnip, Some(0), "tree-debt033");
        e2.detail = Some(serde_json::json!({
            "original_sequence": 7,
            "content_hash": "sha256:abc",
            "causal_parent": 5,
        }));
        let mut e3 = event(&journal, 2, GovernedLifecycleEventKind::CacheHealthSample, Some(1), "tree-debt033");
        e3.detail = Some(serde_json::json!({
            "prompt_tokens": 1000,
            "hit_tokens": 900,
            "miss_tokens": 100,
            "output_tokens": 42,
            "hit_ratio": 0.9,
            "truth": "reported",
        }));
        journal.append(e1).unwrap();
        journal.append(e2).unwrap();
        journal.append(e3).unwrap();

        assert!(journal.verify_chain().is_ok(), "chain must verify with detail folded in");
        assert_eq!(journal.events()[0].kind.as_str(), "context_reset");
        assert_eq!(journal.events()[1].kind.as_str(), "tool_result_snip");
        assert_eq!(journal.events()[2].kind.as_str(), "cache_health_sample");
        assert_eq!(
            journal.events()[2].detail.as_ref().unwrap()["hit_ratio"],
            serde_json::json!(0.9)
        );
        // None of the new kinds are terminal.
        assert!(!journal.events()[0].kind.is_terminal());
        assert!(!journal.events()[2].kind.is_terminal());
    }

    #[test]
    fn detail_participates_in_payload_hash() {
        let journal = LifecycleJournal::in_memory("tree-dh");
        let plain = event(&journal, 1, GovernedLifecycleEventKind::ContextReset, None, "tree-dh");
        let mut with_detail = event(&journal, 1, GovernedLifecycleEventKind::ContextReset, None, "tree-dh");
        with_detail.detail = Some(serde_json::json!({"reason": "x"}));
        with_detail.payload_hash = with_detail.compute_payload_hash().unwrap();
        assert_ne!(plain.payload_hash, with_detail.payload_hash);
        // Canonical bytes for the detail-less event must not include a detail
        // field: legacy stored hashes keep verifying. Field marker in the
        // canonical encoding is `detail<TAB>`.
        let legacy_bytes = plain.canonical_bytes().unwrap();
        let as_text = String::from_utf8_lossy(&legacy_bytes);
        assert!(as_text.contains("prev_payload_hash\t"));
        assert!(!as_text.contains("detail\t"));
        let with_bytes = with_detail.canonical_bytes().unwrap();
        assert!(String::from_utf8_lossy(&with_bytes).contains("detail\t"));
    }

    #[test]
    fn legacy_json_without_detail_field_decodes_and_verifies() {
        // A v1-shape record (no detail key) must decode with detail = None and
        // keep its stored payload hash verifiable.
        let journal = LifecycleJournal::in_memory("tree-legacy-detail");
        let event = event(&journal, 1, GovernedLifecycleEventKind::Ready, None, "tree-legacy-detail");
        let mut json = serde_json::to_value(&event).unwrap();
        json.as_object_mut().unwrap().remove("detail");
        let decoded: GovernedLifecycleEventV1 = serde_json::from_value(json).unwrap();
        assert_eq!(decoded.detail, None);
        assert_eq!(decoded.payload_hash, event.payload_hash);
        assert_eq!(
            decoded.compute_payload_hash().unwrap(),
            event.payload_hash,
            "payload hash must survive legacy decode"
        );
    }
}
