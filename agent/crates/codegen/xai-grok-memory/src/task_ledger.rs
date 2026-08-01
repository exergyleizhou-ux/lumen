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
use xai_grok_tools::types::task_tree_memory::{
    TaskTreeMemoryBackend, TaskTreeMemoryFact as BackendFact, TaskTreeMemoryFactKind,
    TaskTreeMemoryReviewState, TaskTreeMemoryWriteReceipt,
};

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
    #[serde(default)]
    pub kind: TaskTreeMemoryFactKind,
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
    /// The final append may have been interrupted.  This is recoverable only
    /// by an explicit root-owned repair, never by silently continuing to use
    /// or extend the journal.
    TornFinalRecord {
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
            Self::TornFinalRecord { line, message } => write!(
                f,
                "working-memory ledger has a torn final record at line {line}; recovery review required: {message}"
            ),
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

/// Shell-owned implementation of the tools crate's narrow task-tree memory
/// port. It retains the root identity and ledger location chosen by the host;
/// a model call never supplies either value.
#[derive(Debug, Clone)]
pub struct WorkingMemoryLedgerBackend {
    ledger: WorkingMemoryLedger,
}

impl WorkingMemoryLedgerBackend {
    pub fn new(ledger: WorkingMemoryLedger) -> Self {
        Self { ledger }
    }

    fn into_fact(
        &self,
        author_session_id: &str,
        fact: BackendFact,
        state: WorkingMemoryState,
    ) -> WorkingMemoryFact {
        WorkingMemoryFact {
            task_tree_id: self.ledger.root_session_id.clone(),
            // Branch attribution is host-owned just like the author session.
            // A model may describe an observation but must not be able to
            // forge another branch's provenance in the shared ledger.
            branch_id: author_session_id.to_owned(),
            fact_id: fact.fact_id,
            revision: fact.revision,
            kind: fact.kind,
            author_session_id: author_session_id.to_owned(),
            evidence_ref: fact.evidence_ref,
            confidence: fact.confidence,
            state,
            text: fact.text,
        }
    }
}

#[async_trait::async_trait]
impl TaskTreeMemoryBackend for WorkingMemoryLedgerBackend {
    async fn propose(
        &self,
        author_session_id: &str,
        fact: BackendFact,
    ) -> Result<TaskTreeMemoryWriteReceipt, String> {
        let fact = self.into_fact(author_session_id, fact, WorkingMemoryState::Proposed);
        let receipt = TaskTreeMemoryWriteReceipt {
            fact_id: fact.fact_id.clone(),
            revision: fact.revision,
            state: "proposed",
        };
        self.ledger
            .propose(fact)
            .map_err(|error| error.to_string())?;
        Ok(receipt)
    }

    async fn review(
        &self,
        reviewer_session_id: &str,
        fact: BackendFact,
        state: TaskTreeMemoryReviewState,
    ) -> Result<TaskTreeMemoryWriteReceipt, String> {
        let state = match state {
            TaskTreeMemoryReviewState::Accepted => WorkingMemoryState::Accepted,
            TaskTreeMemoryReviewState::Rejected => WorkingMemoryState::Rejected,
            TaskTreeMemoryReviewState::Superseded => WorkingMemoryState::Superseded,
        };
        let fact = self.into_fact(reviewer_session_id, fact, state);
        let receipt = TaskTreeMemoryWriteReceipt {
            fact_id: fact.fact_id.clone(),
            revision: fact.revision,
            state: match state {
                WorkingMemoryState::Accepted => "accepted",
                WorkingMemoryState::Rejected => "rejected",
                WorkingMemoryState::Superseded => "superseded",
                WorkingMemoryState::Proposed => unreachable!("review cannot be proposed"),
            },
        };
        self.ledger
            .review(reviewer_session_id, fact, state)
            .map_err(|error| error.to_string())?;
        Ok(receipt)
    }
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
        // Accepted facts are injected into descendant prompts as shared task
        // truth.  A root review is necessary but not sufficient: without a
        // durable evidence reference a plausible-looking assertion can still
        // become a cross-agent hallucination amplifier.  Rejections and
        // supersessions remain evidence-optional because they never enter the
        // shared fact view.
        if state == WorkingMemoryState::Accepted
            && fact
                .evidence_ref
                .as_deref()
                .is_none_or(|reference| reference.trim().is_empty())
        {
            return Err(WorkingMemoryLedgerError::Invalid(
                "accepted working-memory facts require a non-empty evidence_ref".to_owned(),
            ));
        }
        fact.author_session_id = reviewer_session_id.to_owned();
        fact.state = state;
        self.append_checked(fact, true)
    }

    /// Latest root-accepted fact for each id, ordered by fact id.
    ///
    /// A child may append a newer proposal while the root is reviewing it. That
    /// proposal must not erase the last accepted fact from sibling prompts: an
    /// unreviewed branch would otherwise be able to retract shared truth just
    /// by claiming a revision number. Rejected reviews likewise preserve the
    /// last accepted fact. Only a root-owned `Superseded` revision explicitly
    /// withdraws it from the shared view.
    pub fn accepted_facts(&self) -> Result<Vec<WorkingMemoryFact>, WorkingMemoryLedgerError> {
        let mut accepted = BTreeMap::<String, WorkingMemoryFact>::new();
        for fact in self.load_all()? {
            match fact.state {
                WorkingMemoryState::Accepted => {
                    accepted.insert(fact.fact_id.clone(), fact);
                }
                WorkingMemoryState::Superseded => {
                    accepted.remove(&fact.fact_id);
                }
                WorkingMemoryState::Proposed | WorkingMemoryState::Rejected => {}
            }
        }
        Ok(accepted.into_values().collect())
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
                Ok(fact) => {
                    // A ledger is durable input to descendant prompts, not
                    // merely an append target. Revalidate every record on
                    // read so a misplaced or manually-corrupted JSONL entry
                    // cannot cross task-tree boundaries or masquerade as a
                    // reviewed fact.
                    fact.validate()?;
                    if fact.task_tree_id != self.root_session_id {
                        return Err(WorkingMemoryLedgerError::Invalid(format!(
                            "ledger record at line {} belongs to task tree {:?}, not {:?}",
                            index + 1,
                            fact.task_tree_id,
                            self.root_session_id
                        )));
                    }
                    facts.push(fact);
                }
                // A power loss can tear only the final append.  Earlier code
                // silently skipped it, then allowed the next writer to append
                // after the torn bytes; that turns recoverable tail damage into
                // middle-of-journal corruption.  Fail closed until the root
                // explicitly repairs and reviews the ledger.
                Err(error) if Some(index) == last_nonempty => {
                    return Err(WorkingMemoryLedgerError::TornFinalRecord {
                        line: index + 1,
                        message: error.to_string(),
                    });
                }
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
            kind: TaskTreeMemoryFactKind::Fact,
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
    fn unreviewed_or_rejected_revision_cannot_erase_prior_accepted_fact() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        ledger
            .propose(fact("fact-a", 1, "child", "proposal one"))
            .unwrap();
        ledger
            .review(
                "root",
                fact("fact-a", 2, "root", "accepted truth"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        ledger
            .propose(fact("fact-a", 3, "child", "unreviewed replacement"))
            .unwrap();
        assert_eq!(ledger.accepted_facts().unwrap()[0].text, "accepted truth");

        ledger
            .review(
                "root",
                fact("fact-a", 4, "root", "rejected replacement"),
                WorkingMemoryState::Rejected,
            )
            .unwrap();
        assert_eq!(ledger.accepted_facts().unwrap()[0].text, "accepted truth");
    }

    #[test]
    fn root_supersession_explicitly_withdraws_prior_accepted_fact() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        ledger
            .propose(fact("fact-a", 1, "child", "proposal one"))
            .unwrap();
        ledger
            .review(
                "root",
                fact("fact-a", 2, "root", "accepted truth"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        ledger
            .review(
                "root",
                fact("fact-a", 3, "root", "withdrawn"),
                WorkingMemoryState::Superseded,
            )
            .unwrap();
        assert!(ledger.accepted_facts().unwrap().is_empty());
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
    fn root_cannot_accept_a_fact_without_evidence() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        ledger
            .propose(fact("fact-a", 1, "child", "unreviewed"))
            .unwrap();
        let mut reviewed = fact("fact-a", 2, "root", "would be shared without proof");
        reviewed.evidence_ref = None;
        let error = ledger
            .review("root", reviewed, WorkingMemoryState::Accepted)
            .unwrap_err();
        assert!(
            matches!(error, WorkingMemoryLedgerError::Invalid(message) if message.contains("evidence_ref"))
        );
        assert!(ledger.accepted_facts().unwrap().is_empty());
    }

    #[test]
    fn foreign_task_tree_record_fails_closed_before_prompt_injection() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        let mut foreign = fact("fact-a", 1, "child", "must not cross trees");
        foreign.task_tree_id = "different-root".to_owned();
        std::fs::write(
            ledger.path(),
            format!("{}\n", serde_json::to_string(&foreign).unwrap()),
        )
        .unwrap();

        let error = ledger.accepted_facts().unwrap_err();
        assert!(matches!(
            error,
            WorkingMemoryLedgerError::Invalid(message) if message.contains("belongs to task tree")
        ));
    }

    #[test]
    fn legacy_journal_record_without_kind_defaults_to_fact() {
        let mut record = serde_json::to_value(fact("fact-a", 1, "child", "legacy"))
            .expect("test fact serializes");
        record
            .as_object_mut()
            .expect("fact is a JSON object")
            .remove("kind");
        let restored: WorkingMemoryFact =
            serde_json::from_value(record).expect("old journal record remains readable");
        assert_eq!(restored.kind, TaskTreeMemoryFactKind::Fact);
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

    #[tokio::test]
    async fn backend_allows_child_proposal_but_only_root_review() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        let backend = WorkingMemoryLedgerBackend::new(ledger.clone());
        let proposed = BackendFact {
            branch_id: "branch-a".to_owned(),
            fact_id: "fact-a".to_owned(),
            revision: 1,
            kind: TaskTreeMemoryFactKind::Fact,
            evidence_ref: Some("test://evidence".to_owned()),
            confidence: 80,
            text: "child observation".to_owned(),
        };
        let receipt = backend.propose("child", proposed).await.unwrap();
        assert_eq!(receipt.state, "proposed");
        assert!(ledger.accepted_facts().unwrap().is_empty());

        let review = BackendFact {
            branch_id: "branch-a".to_owned(),
            fact_id: "fact-a".to_owned(),
            revision: 2,
            kind: TaskTreeMemoryFactKind::Fact,
            evidence_ref: Some("test://evidence".to_owned()),
            confidence: 95,
            text: "root reviewed observation".to_owned(),
        };
        let error = backend
            .review("child", review.clone(), TaskTreeMemoryReviewState::Accepted)
            .await
            .unwrap_err();
        assert!(error.contains("only root session"));

        let receipt = backend
            .review("root", review, TaskTreeMemoryReviewState::Accepted)
            .await
            .unwrap();
        assert_eq!(receipt.state, "accepted");
        assert_eq!(
            ledger.accepted_facts().unwrap()[0].text,
            "root reviewed observation"
        );
    }

    #[tokio::test]
    async fn backend_binds_branch_provenance_to_the_actual_author_session() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        let backend = WorkingMemoryLedgerBackend::new(ledger.clone());
        backend
            .propose(
                "child-session",
                BackendFact {
                    branch_id: "forged-sibling-branch".to_owned(),
                    fact_id: "fact-a".to_owned(),
                    revision: 1,
                    kind: TaskTreeMemoryFactKind::Evidence,
                    evidence_ref: Some("test://evidence".to_owned()),
                    confidence: 90,
                    text: "observed evidence".to_owned(),
                },
            )
            .await
            .unwrap();

        let stored = ledger.load_all().unwrap();
        assert_eq!(stored.len(), 1);
        assert_eq!(stored[0].author_session_id, "child-session");
        assert_eq!(stored[0].branch_id, "child-session");
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
    fn torn_final_record_blocks_reads_and_new_appends_pending_recovery() {
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
        assert!(matches!(
            ledger.load_all(),
            Err(WorkingMemoryLedgerError::TornFinalRecord { line: 2, .. })
        ));
        assert!(matches!(
            ledger.accepted_facts(),
            Err(WorkingMemoryLedgerError::TornFinalRecord { .. })
        ));
        assert!(matches!(
            ledger.propose(fact("fact-b", 1, "child", "must not append after damage")),
            Err(WorkingMemoryLedgerError::TornFinalRecord { .. })
        ));

        std::fs::write(&path, b"{bad}\n{also-bad}\n").unwrap();
        assert!(matches!(
            ledger.load_all(),
            Err(WorkingMemoryLedgerError::CorruptRecord { line: 1, .. })
        ));
    }
}
