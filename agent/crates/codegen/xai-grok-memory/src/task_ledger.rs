//! Append-only shared working-memory ledger for one task tree.
//!
//! This deliberately does **not** reuse session JSONL or curated `MEMORY.md`.
//! A branch agent may propose a fact with evidence, but only the root session
//! can promote/reject it.  Readers therefore share verified task state rather
//! than recursively amplifying an unreviewed child instruction.

use std::collections::BTreeMap;
use std::fs::OpenOptions;
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::sync::{LazyLock, Mutex};

use fs2::FileExt;
use serde::{Deserialize, Serialize};

use crate::MemoryStorage;

/// `flock` serializes cooperating processes, but is not enough on its own for
/// two independently-opened descriptors in this process. Keep the check and
/// append together here as well: concurrent child completions must never both
/// observe the same next revision and silently append it.
static APPEND_LOCK: LazyLock<Mutex<()>> = LazyLock::new(|| Mutex::new(()));

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WorkingMemoryState {
    Proposed,
    Accepted,
    Rejected,
    Superseded,
}

/// One revision of a fact in a task tree. Revisions are append-only so every
/// change remains attributable to its session and evidence.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct WorkingMemoryFact {
    pub task_tree_id: String,
    pub branch_id: String,
    pub fact_id: String,
    pub revision: u64,
    pub author_session_id: String,
    pub evidence_ref: Option<String>,
    pub confidence: u8,
    pub state: WorkingMemoryState,
    pub text: String,
}

impl WorkingMemoryFact {
    fn validate(&self) -> Result<(), WorkingMemoryLedgerError> {
        for (field, value) in [
            ("task_tree_id", self.task_tree_id.as_str()),
            ("branch_id", self.branch_id.as_str()),
            ("fact_id", self.fact_id.as_str()),
            ("author_session_id", self.author_session_id.as_str()),
            ("text", self.text.as_str()),
        ] {
            if value.trim().is_empty() {
                return Err(WorkingMemoryLedgerError::Invalid(format!(
                    "{field} must not be empty"
                )));
            }
        }
        if self.confidence > 100 {
            return Err(WorkingMemoryLedgerError::Invalid(
                "confidence must be in 0..=100".to_owned(),
            ));
        }
        Ok(())
    }
}

#[derive(Debug)]
pub enum WorkingMemoryLedgerError {
    Io(std::io::Error),
    Json(serde_json::Error),
    Invalid(String),
    UnauthorizedReview {
        reviewer: String,
        root: String,
    },
    RevisionConflict {
        fact_id: String,
        expected: u64,
        actual: u64,
    },
    CorruptRecord {
        line: usize,
        message: String,
    },
}

impl std::fmt::Display for WorkingMemoryLedgerError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Io(error) => write!(f, "working-memory ledger I/O failed: {error}"),
            Self::Json(error) => write!(f, "working-memory ledger JSON failed: {error}"),
            Self::Invalid(message) => write!(f, "invalid working-memory fact: {message}"),
            Self::UnauthorizedReview { reviewer, root } => write!(
                f,
                "only root session {root:?} may review working-memory facts (got {reviewer:?})"
            ),
            Self::RevisionConflict {
                fact_id,
                expected,
                actual,
            } => write!(
                f,
                "working-memory fact {fact_id:?} revision conflict: expected {expected}, got {actual}"
            ),
            Self::CorruptRecord { line, message } => {
                write!(
                    f,
                    "corrupt working-memory ledger record at line {line}: {message}"
                )
            }
        }
    }
}

impl std::error::Error for WorkingMemoryLedgerError {}

impl From<std::io::Error> for WorkingMemoryLedgerError {
    fn from(error: std::io::Error) -> Self {
        Self::Io(error)
    }
}

impl From<serde_json::Error> for WorkingMemoryLedgerError {
    fn from(error: serde_json::Error) -> Self {
        Self::Json(error)
    }
}

/// Per-task-tree append-only ledger. The identifier is hashed for the filename
/// so session IDs never become path components.
#[derive(Debug, Clone)]
pub struct WorkingMemoryLedger {
    root_session_id: String,
    path: PathBuf,
}

impl WorkingMemoryLedger {
    pub fn for_task_tree(storage: &MemoryStorage, root_session_id: impl Into<String>) -> Self {
        Self::for_workspace_dir(storage.workspace_dir(), root_session_id)
    }

    /// Resolve a ledger from the workspace memory directory captured by the
    /// root session. Nested children may use isolated worktrees, so deriving
    /// this location again from a child's current directory would split one
    /// task tree into multiple, incompatible ledgers.
    pub fn for_workspace_dir(
        workspace_dir: impl AsRef<Path>,
        root_session_id: impl Into<String>,
    ) -> Self {
        let root_session_id = root_session_id.into();
        let hash = blake3::hash(root_session_id.as_bytes()).to_hex();
        Self {
            root_session_id,
            path: workspace_dir
                .as_ref()
                .join("task-ledgers")
                .join(format!("{}.jsonl", &hash[..16])),
        }
    }

    pub fn with_path(root_session_id: impl Into<String>, path: impl Into<PathBuf>) -> Self {
        Self {
            root_session_id: root_session_id.into(),
            path: path.into(),
        }
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    pub fn propose(&self, mut fact: WorkingMemoryFact) -> Result<(), WorkingMemoryLedgerError> {
        fact.state = WorkingMemoryState::Proposed;
        self.append_checked(fact, false)
    }

    /// Append a reviewed revision. Only the root session may change a fact out
    /// of `Proposed`, and the revision must directly follow the last one for
    /// that fact.
    pub fn review(
        &self,
        reviewer_session_id: &str,
        mut fact: WorkingMemoryFact,
        state: WorkingMemoryState,
    ) -> Result<(), WorkingMemoryLedgerError> {
        if reviewer_session_id != self.root_session_id {
            return Err(WorkingMemoryLedgerError::UnauthorizedReview {
                reviewer: reviewer_session_id.to_owned(),
                root: self.root_session_id.clone(),
            });
        }
        if state == WorkingMemoryState::Proposed {
            return Err(WorkingMemoryLedgerError::Invalid(
                "review state must not be proposed".to_owned(),
            ));
        }
        fact.author_session_id = reviewer_session_id.to_owned();
        fact.state = state;
        self.append_checked(fact, true)
    }

    /// Latest accepted fact for each id, ordered by fact id. Proposed, rejected
    /// and superseded revisions never enter another agent's shared fact view.
    pub fn accepted_facts(&self) -> Result<Vec<WorkingMemoryFact>, WorkingMemoryLedgerError> {
        let mut latest = BTreeMap::<String, WorkingMemoryFact>::new();
        for fact in self.load_all()? {
            latest
                .entry(fact.fact_id.clone())
                .and_modify(|current| {
                    if fact.revision > current.revision {
                        *current = fact.clone();
                    }
                })
                .or_insert(fact);
        }
        Ok(latest
            .into_values()
            .filter(|fact| fact.state == WorkingMemoryState::Accepted)
            .collect())
    }

    pub fn load_all(&self) -> Result<Vec<WorkingMemoryFact>, WorkingMemoryLedgerError> {
        let file = match std::fs::File::open(&self.path) {
            Ok(file) => file,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
            Err(error) => return Err(error.into()),
        };
        let lines: Vec<_> = BufReader::new(file).lines().collect::<Result<_, _>>()?;
        let last_nonempty = lines.iter().rposition(|line| !line.trim().is_empty());
        let mut facts = Vec::new();
        for (index, line) in lines.iter().enumerate() {
            if line.trim().is_empty() {
                continue;
            }
            match serde_json::from_str::<WorkingMemoryFact>(line) {
                Ok(fact) => facts.push(fact),
                // A power loss can tear only the final append. Do not invent a
                // fact from it; corruption in the middle remains a hard error.
                Err(_) if Some(index) == last_nonempty => break,
                Err(error) => {
                    return Err(WorkingMemoryLedgerError::CorruptRecord {
                        line: index + 1,
                        message: error.to_string(),
                    });
                }
            }
        }
        Ok(facts)
    }

    fn append_checked(
        &self,
        fact: WorkingMemoryFact,
        require_next_revision: bool,
    ) -> Result<(), WorkingMemoryLedgerError> {
        let _process_lock = APPEND_LOCK.lock().map_err(|_| {
            WorkingMemoryLedgerError::Invalid("working-memory append lock poisoned".to_owned())
        })?;
        fact.validate()?;
        if fact.task_tree_id != self.root_session_id {
            return Err(WorkingMemoryLedgerError::Invalid(
                "task_tree_id must equal this ledger's root session id".to_owned(),
            ));
        }
        let parent = self.path.parent().ok_or_else(|| {
            WorkingMemoryLedgerError::Invalid("ledger path has no parent".to_owned())
        })?;
        std::fs::create_dir_all(parent)?;
        let line = serde_json::to_string(&fact)?;
        let mut file = OpenOptions::new()
            .create(true)
            .read(true)
            .append(true)
            .open(&self.path)?;
        file.lock_exclusive()?;
        let result = (|| {
            let current_revision = self
                .load_all()?
                .into_iter()
                .filter(|current| current.fact_id == fact.fact_id)
                .map(|current| current.revision)
                .max();
            let expected = current_revision.map_or(1, |revision| revision.saturating_add(1));
            if (require_next_revision || current_revision.is_some()) && fact.revision != expected {
                return Err(WorkingMemoryLedgerError::RevisionConflict {
                    fact_id: fact.fact_id,
                    expected,
                    actual: fact.revision,
                });
            }
            if !require_next_revision && current_revision.is_none() && fact.revision != 1 {
                return Err(WorkingMemoryLedgerError::RevisionConflict {
                    fact_id: fact.fact_id,
                    expected: 1,
                    actual: fact.revision,
                });
            }
            file.write_all(line.as_bytes())?;
            file.write_all(b"\n")?;
            file.sync_data()?;
            Ok(())
        })();
        file.unlock()?;
        result
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fact(fact_id: &str, revision: u64, author: &str, text: &str) -> WorkingMemoryFact {
        WorkingMemoryFact {
            task_tree_id: "root".to_owned(),
            branch_id: "branch-a".to_owned(),
            fact_id: fact_id.to_owned(),
            revision,
            author_session_id: author.to_owned(),
            evidence_ref: Some("test://evidence".to_owned()),
            confidence: 80,
            state: WorkingMemoryState::Proposed,
            text: text.to_owned(),
        }
    }

    #[test]
    fn child_proposal_is_invisible_until_root_accepts_it() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        ledger
            .propose(fact("fact-a", 1, "child", "unreviewed"))
            .unwrap();
        assert!(ledger.accepted_facts().unwrap().is_empty());

        ledger
            .review(
                "root",
                fact("fact-a", 2, "ignored", "reviewed"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        let accepted = ledger.accepted_facts().unwrap();
        assert_eq!(accepted.len(), 1);
        assert_eq!(accepted[0].text, "reviewed");
        assert_eq!(accepted[0].author_session_id, "root");
    }

    #[test]
    fn child_cannot_promote_its_own_fact() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        let error = ledger
            .review(
                "child",
                fact("fact-a", 1, "child", "not allowed"),
                WorkingMemoryState::Accepted,
            )
            .unwrap_err();
        assert!(matches!(
            error,
            WorkingMemoryLedgerError::UnauthorizedReview { .. }
        ));
    }

    #[test]
    fn root_workspace_directory_keeps_isolated_descendants_on_one_ledger() {
        let temp = tempfile::tempdir().unwrap();
        let root_workspace_memory = temp.path().join("root-workspace-memory");
        let root_ledger = WorkingMemoryLedger::for_workspace_dir(&root_workspace_memory, "root");

        // A child worktree would normally resolve a different workspace memory
        // directory. Passing the root-selected directory is what keeps the
        // whole nested task tree on the same reviewed-fact ledger.
        let isolated_child_workspace = temp.path().join("child-worktree-memory");
        let child_ledger = WorkingMemoryLedger::for_workspace_dir(&root_workspace_memory, "root");
        let incorrectly_recomputed =
            WorkingMemoryLedger::for_workspace_dir(&isolated_child_workspace, "root");

        assert_eq!(root_ledger.path(), child_ledger.path());
        assert_ne!(root_ledger.path(), incorrectly_recomputed.path());
    }

    #[test]
    fn concurrent_same_revision_has_one_winner_not_two_records() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("ledger.jsonl");
        let first = WorkingMemoryLedger::with_path("root", &path);
        let second = WorkingMemoryLedger::with_path("root", &path);
        let barrier = std::sync::Arc::new(std::sync::Barrier::new(2));

        let first_barrier = barrier.clone();
        let first_thread = std::thread::spawn(move || {
            first_barrier.wait();
            first.propose(fact("fact-a", 1, "child-a", "first"))
        });
        let second_thread = std::thread::spawn(move || {
            barrier.wait();
            second.propose(fact("fact-a", 1, "child-b", "second"))
        });

        let outcomes = [first_thread.join().unwrap(), second_thread.join().unwrap()];
        assert_eq!(outcomes.iter().filter(|outcome| outcome.is_ok()).count(), 1);
        assert!(outcomes.iter().any(|outcome| matches!(
            outcome,
            Err(WorkingMemoryLedgerError::RevisionConflict { .. })
        )));

        let facts = WorkingMemoryLedger::with_path("root", path)
            .load_all()
            .unwrap();
        assert_eq!(facts.len(), 1);
        assert_eq!(facts[0].revision, 1);
    }

    #[test]
    fn torn_final_record_is_ignored_but_middle_corruption_is_rejected() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("ledger.jsonl");
        let ledger = WorkingMemoryLedger::with_path("root", &path);
        ledger.propose(fact("fact-a", 1, "child", "valid")).unwrap();
        std::fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap()
            .write_all(b"{torn")
            .unwrap();
        assert_eq!(ledger.load_all().unwrap().len(), 1);

        std::fs::write(&path, b"{bad}\n{also-bad}\n").unwrap();
        assert!(matches!(
            ledger.load_all(),
            Err(WorkingMemoryLedgerError::CorruptRecord { line: 1, .. })
        ));
    }
}
