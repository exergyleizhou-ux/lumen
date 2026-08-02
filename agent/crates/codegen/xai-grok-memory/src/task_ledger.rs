//! Append-only shared working-memory ledger for one task tree.
//!
//! This deliberately does **not** reuse session JSONL or curated `MEMORY.md`.
//! A branch agent may propose a fact with evidence, but only the root session
//! can promote/reject it.  Readers therefore share verified task state rather
//! than recursively amplifying an unreviewed child instruction.

use std::collections::BTreeMap;
use std::fs::OpenOptions;
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::sync::{LazyLock, Mutex};

use fs2::FileExt;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::claim_authority::{
    ClaimAuthority, ClaimAuthorityActor, ClaimDenyReason, ClaimTransitionRequest,
};
use crate::{MemoryScope, MemoryStorage};
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
    /// Ephemeral only — never written to the durable JSONL ledger.
    Draft,
    Proposed,
    /// Root SessionActor marked host verification; still not shared truth.
    HostVerified,
    Accepted,
    Rejected,
    Superseded,
    /// Root-owned hard withdrawal after acceptance (stronger than Superseded).
    Revoked,
}

impl WorkingMemoryState {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Draft => "draft",
            Self::Proposed => "proposed",
            Self::HostVerified => "host_verified",
            Self::Accepted => "accepted",
            Self::Rejected => "rejected",
            Self::Superseded => "superseded",
            Self::Revoked => "revoked",
        }
    }

    pub const fn is_shared_truth(self) -> bool {
        matches!(self, Self::Accepted)
    }
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
    /// ClaimAuthority rejected the transition with a machine-readable code.
    ClaimDenied {
        reason: ClaimDenyReason,
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
            Self::ClaimDenied { reason } => {
                write!(f, "claim authority denied transition: {reason}")
            }
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
    /// Host-stamped authority role for review. Session-id alone never grants
    /// RootSessionActor acceptance — the host must inject the correct role
    /// when building the backend (root tool path → RootSessionActor, child →
    /// Child, advisor/TUI → their non-root role).
    review_actor: ClaimAuthorityActor,
}

/// Result of a user-authorized promotion of reviewed working-memory facts into
/// workspace long-term memory.  The fact identities are returned so the shell
/// can give the user an auditable, human-readable receipt without exposing a
/// writable long-term-memory capability to any model or child agent.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WorkingMemoryPromotion {
    pub promoted: Vec<(String, u64)>,
}

/// Immutable read view used by a ContextManifest. It is computed from the
/// validated append-only journal and never accepts caller-supplied revisions.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AcceptedLedgerSnapshot {
    pub task_tree_id: String,
    pub record_count: u64,
    pub accepted_count: u64,
    pub accepted_set_hash: String,
    pub journal_hash: String,
}

impl WorkingMemoryPromotion {
    pub fn promoted_count(&self) -> usize {
        self.promoted.len()
    }
}

/// Receipt for an explicit root-owned repair of a torn final ledger record.
///
/// The discarded bytes are retained verbatim in `backup_path` before the
/// ledger is truncated. This is intentionally a recovery receipt, not an
/// accepted working-memory fact: repair restores the journal's readability but
/// does not certify or promote any claim.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WorkingMemoryLedgerRepair {
    pub repaired_line: usize,
    pub retained_records: usize,
    pub discarded_bytes: usize,
    pub discarded_tail_hash: String,
    pub backup_path: PathBuf,
}

impl WorkingMemoryLedgerBackend {
    /// Root SessionActor tool path: may HostVerify/Accept/Reject/Supersede.
    pub fn new(ledger: WorkingMemoryLedger) -> Self {
        Self::with_review_actor(ledger, ClaimAuthorityActor::RootSessionActor)
    }

    /// Child agent tool path: may propose only.
    pub fn for_child(ledger: WorkingMemoryLedger) -> Self {
        Self::with_review_actor(ledger, ClaimAuthorityActor::Child)
    }

    /// Explicit host-stamped role (Advisor/TUI/daemon/Kairos never accept).
    pub fn with_review_actor(
        ledger: WorkingMemoryLedger,
        review_actor: ClaimAuthorityActor,
    ) -> Self {
        Self {
            ledger,
            review_actor,
        }
    }

    pub fn review_actor(&self) -> ClaimAuthorityActor {
        self.review_actor
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
            state: state.as_str(),
        };
        // Use the host-stamped review_actor, not session-id equality. A
        // non-root role that presents the root session id cannot launder Accept.
        self.ledger
            .review_with_authority(self.review_actor, reviewer_session_id, fact, state)
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

    pub fn root_session_id(&self) -> &str {
        &self.root_session_id
    }

    pub fn propose(&self, mut fact: WorkingMemoryFact) -> Result<(), WorkingMemoryLedgerError> {
        fact.state = WorkingMemoryState::Proposed;
        let actor = if fact.author_session_id == self.root_session_id {
            ClaimAuthorityActor::RootSessionActor
        } else {
            ClaimAuthorityActor::Child
        };
        self.propose_with_authority(actor, fact)
    }

    /// Propose a claim with an explicit authority role. Advisor/daemon/UI
    /// roles are rejected — only child and root SessionActor may propose.
    pub fn propose_with_authority(
        &self,
        actor: ClaimAuthorityActor,
        mut fact: WorkingMemoryFact,
    ) -> Result<(), WorkingMemoryLedgerError> {
        fact.state = WorkingMemoryState::Proposed;
        let expected = self.next_revision_for(&fact.fact_id)?;
        self.authorize_transition(ClaimTransitionRequest {
            actor,
            actor_session_id: fact.author_session_id.as_str(),
            root_session_id: self.root_session_id.as_str(),
            ledger_task_tree_id: self.root_session_id.as_str(),
            fact_task_tree_id: fact.task_tree_id.as_str(),
            from: self.latest_state_for(&fact.fact_id)?,
            to: WorkingMemoryState::Proposed,
            evidence_ref: fact.evidence_ref.as_deref(),
            expected_revision: expected,
            actual_revision: fact.revision,
            grant_cancelled: false,
        })?;
        self.append_checked(fact, false)
    }

    /// Append a reviewed revision. Only the root session may change a fact out
    /// of `Proposed`, and the revision must directly follow the last one for
    /// that fact. Prefer [`Self::review_with_authority`] when the caller role
    /// is not the root SessionActor (Advisor/TUI/MCP must fail closed).
    pub fn review(
        &self,
        reviewer_session_id: &str,
        fact: WorkingMemoryFact,
        state: WorkingMemoryState,
    ) -> Result<(), WorkingMemoryLedgerError> {
        let actor = if reviewer_session_id == self.root_session_id {
            ClaimAuthorityActor::RootSessionActor
        } else {
            ClaimAuthorityActor::Child
        };
        self.review_with_authority(actor, reviewer_session_id, fact, state)
    }

    /// Review with an explicit authority role. Only
    /// [`ClaimAuthorityActor::RootSessionActor`] may HostVerify / Accept /
    /// Reject / Supersede / Revoke.
    pub fn review_with_authority(
        &self,
        actor: ClaimAuthorityActor,
        reviewer_session_id: &str,
        mut fact: WorkingMemoryFact,
        state: WorkingMemoryState,
    ) -> Result<(), WorkingMemoryLedgerError> {
        if state == WorkingMemoryState::Proposed || state == WorkingMemoryState::Draft {
            return Err(WorkingMemoryLedgerError::Invalid(
                "review state must not be proposed or draft".to_owned(),
            ));
        }
        // Non-root session ids never review — keep the historical error shape
        // so shell/tool callers continue to match UnauthorizedReview.
        if reviewer_session_id != self.root_session_id {
            return Err(WorkingMemoryLedgerError::UnauthorizedReview {
                reviewer: reviewer_session_id.to_owned(),
                root: self.root_session_id.clone(),
            });
        }
        // Root session id with a non-root *role* is still fail-closed: Advisor /
        // TUI / daemon / MCP must not launder acceptance by borrowing root id.
        if !actor.is_root_session_actor() {
            return Err(WorkingMemoryLedgerError::ClaimDenied {
                reason: match actor {
                    ClaimAuthorityActor::Advisor => ClaimDenyReason::AdvisorCannotAccept,
                    ClaimAuthorityActor::Kairos => ClaimDenyReason::KairosCannotAccept,
                    ClaimAuthorityActor::Tui => ClaimDenyReason::TuiCannotAccept,
                    ClaimAuthorityActor::Mcp => ClaimDenyReason::McpCannotAccept,
                    ClaimAuthorityActor::ToolOutput => ClaimDenyReason::ToolOutputCannotAccept,
                    ClaimAuthorityActor::Daemon => ClaimDenyReason::DaemonCannotAccept,
                    ClaimAuthorityActor::Child => ClaimDenyReason::ChildCannotAccept,
                    ClaimAuthorityActor::Unknown => ClaimDenyReason::UnknownActorCannotAccept,
                    ClaimAuthorityActor::RootSessionActor => ClaimDenyReason::NonRootCannotReview,
                },
            });
        }
        let expected = self.next_revision_for(&fact.fact_id)?;
        let from = self.latest_state_for(&fact.fact_id)?;
        ClaimAuthority::validate(&ClaimTransitionRequest {
            actor,
            actor_session_id: reviewer_session_id,
            root_session_id: self.root_session_id.as_str(),
            ledger_task_tree_id: self.root_session_id.as_str(),
            fact_task_tree_id: fact.task_tree_id.as_str(),
            from,
            to: state,
            evidence_ref: fact.evidence_ref.as_deref(),
            expected_revision: expected,
            actual_revision: fact.revision,
            grant_cancelled: false,
        })
        .map_err(|reason| WorkingMemoryLedgerError::ClaimDenied { reason })?;
        // Accepted facts are injected into descendant prompts as shared task
        // truth. ClaimAuthority already requires evidence; keep the durable
        // append guard identical so a future validator bug cannot leak unproven
        // claims into the journal.
        if state == WorkingMemoryState::Accepted
            && fact
                .evidence_ref
                .as_deref()
                .is_none_or(|reference| reference.trim().is_empty())
        {
            return Err(WorkingMemoryLedgerError::ClaimDenied {
                reason: ClaimDenyReason::MissingEvidence,
            });
        }
        fact.author_session_id = reviewer_session_id.to_owned();
        fact.state = state;
        self.append_checked(fact, true)
    }

    fn authorize_transition(
        &self,
        request: ClaimTransitionRequest<'_>,
    ) -> Result<(), WorkingMemoryLedgerError> {
        ClaimAuthority::validate(&request)
            .map_err(|reason| WorkingMemoryLedgerError::ClaimDenied { reason })
    }

    fn latest_state_for(
        &self,
        fact_id: &str,
    ) -> Result<Option<WorkingMemoryState>, WorkingMemoryLedgerError> {
        Ok(self
            .load_all()?
            .into_iter()
            .filter(|current| current.fact_id == fact_id)
            .max_by_key(|current| current.revision)
            .map(|current| current.state))
    }

    fn next_revision_for(&self, fact_id: &str) -> Result<u64, WorkingMemoryLedgerError> {
        let current = self
            .load_all()?
            .into_iter()
            .filter(|current| current.fact_id == fact_id)
            .map(|current| current.revision)
            .max();
        Ok(current.map_or(1, |revision| revision.saturating_add(1)))
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
                WorkingMemoryState::Superseded | WorkingMemoryState::Revoked => {
                    accepted.remove(&fact.fact_id);
                }
                WorkingMemoryState::Proposed
                | WorkingMemoryState::Rejected
                | WorkingMemoryState::HostVerified
                | WorkingMemoryState::Draft => {}
            }
        }
        Ok(accepted.into_values().collect())
    }

    /// Freeze the currently accepted view and the validated journal boundary.
    /// The resulting hashes are suitable for admission checks; a later
    /// proposal or review cannot mutate this value in place.
    pub fn accepted_snapshot(&self) -> Result<AcceptedLedgerSnapshot, WorkingMemoryLedgerError> {
        let records = self.load_all()?;
        let accepted = self.accepted_facts()?;
        let accepted_bytes = serde_json::to_vec(&accepted)?;
        let journal_bytes = serde_json::to_vec(&records)?;
        Ok(AcceptedLedgerSnapshot {
            task_tree_id: self.root_session_id.clone(),
            record_count: records.len() as u64,
            accepted_count: accepted.len() as u64,
            accepted_set_hash: format!("sha256:{:x}", Sha256::digest(accepted_bytes)),
            journal_hash: format!("sha256:{:x}", Sha256::digest(journal_bytes)),
        })
    }

    /// Copy only reusable, root-reviewed task facts into the workspace's
    /// curated long-term memory.
    ///
    /// This deliberately is not part of [`TaskTreeMemoryBackend`]: a model or
    /// child agent may propose and a root agent may review task-local facts,
    /// but only a direct user command in the root session may decide that an
    /// accepted fact should outlive this task tree.  Progress, blockers,
    /// assumptions, and raw evidence references are useful for coordination
    /// but are not durable project knowledge, so they never cross this
    /// boundary.
    ///
    /// The generated HTML marker makes the operation idempotent for a given
    /// `(task_tree, fact_id, revision)` even after restart.  It is intentionally
    /// stored next to the human-readable entry rather than in a second journal:
    /// a crash cannot acknowledge a promotion that was never written to
    /// `MEMORY.md`.
    pub fn promote_accepted_facts_to_workspace_memory(
        &self,
        storage: &MemoryStorage,
    ) -> Result<WorkingMemoryPromotion, WorkingMemoryLedgerError> {
        if storage.is_ephemeral() {
            return Err(WorkingMemoryLedgerError::Invalid(
                "cannot promote task-tree memory from an ephemeral workspace".to_owned(),
            ));
        }
        let memory_path = storage.workspace_memory_file();
        let existing = match std::fs::read_to_string(&memory_path) {
            Ok(content) => content,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => String::new(),
            Err(error) => return Err(error.into()),
        };
        let facts = self
            .accepted_facts()?
            .into_iter()
            .filter(|fact| {
                matches!(
                    fact.kind,
                    TaskTreeMemoryFactKind::Fact | TaskTreeMemoryFactKind::Decision
                )
            })
            .filter(|fact| !existing.contains(&promotion_marker(fact)))
            .collect::<Vec<_>>();
        if facts.is_empty() {
            return Ok(WorkingMemoryPromotion {
                promoted: Vec::new(),
            });
        }

        let mut content = String::from("## Root-reviewed task-tree knowledge\n\n");
        let mut promoted = Vec::with_capacity(facts.len());
        for fact in facts {
            let evidence = fact.evidence_ref.as_deref().ok_or_else(|| {
                WorkingMemoryLedgerError::Invalid(format!(
                    "accepted ledger fact {:?} revision {} lacks a non-empty evidence_ref",
                    fact.fact_id, fact.revision
                ))
            })?;
            content.push_str(&format!(
                "<!-- {} -->\n- **{} `{}` r{}**: {}\n  Evidence: `{}`\n",
                promotion_marker(&fact),
                fact.kind.label(),
                fact.fact_id,
                fact.revision,
                fact.text,
                evidence,
            ));
            promoted.push((fact.fact_id, fact.revision));
        }
        storage.append_to_memory(MemoryScope::Workspace, &content)?;
        Ok(WorkingMemoryPromotion { promoted })
    }

    pub fn load_all(&self) -> Result<Vec<WorkingMemoryFact>, WorkingMemoryLedgerError> {
        let mut file = match std::fs::File::open(&self.path) {
            Ok(file) => file,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
            Err(error) => return Err(error.into()),
        };
        let mut bytes = Vec::new();
        file.read_to_end(&mut bytes)?;
        match self.inspect_bytes(&bytes)? {
            LedgerInspection::Valid { facts } => Ok(facts),
            LedgerInspection::TornFinalRecord { line, message, .. } => {
                Err(WorkingMemoryLedgerError::TornFinalRecord { line, message })
            }
        }
    }

    /// Remove only a torn final record after the actual root session has
    /// explicitly authorized recovery.
    ///
    /// This never repairs a malformed middle record, never guesses at valid
    /// JSON, and never silently discards bytes: the exact tail is fsynced into
    /// a sibling recovery artifact before the ledger is truncated. The caller
    /// receives that artifact path for operator review.
    pub fn repair_torn_final_record(
        &self,
        reviewer_session_id: &str,
    ) -> Result<WorkingMemoryLedgerRepair, WorkingMemoryLedgerError> {
        if reviewer_session_id != self.root_session_id {
            return Err(WorkingMemoryLedgerError::UnauthorizedReview {
                reviewer: reviewer_session_id.to_owned(),
                root: self.root_session_id.clone(),
            });
        }
        let _process_lock = APPEND_LOCK.lock().map_err(|_| {
            WorkingMemoryLedgerError::Invalid("working-memory append lock poisoned".to_owned())
        })?;
        let mut file = OpenOptions::new().read(true).write(true).open(&self.path)?;
        file.lock_exclusive()?;
        let result = (|| {
            file.seek(SeekFrom::Start(0))?;
            let mut bytes = Vec::new();
            file.read_to_end(&mut bytes)?;
            let LedgerInspection::TornFinalRecord {
                line,
                retained_records,
                discarded_start,
                ..
            } = self.inspect_bytes(&bytes)?
            else {
                return Err(WorkingMemoryLedgerError::Invalid(
                    "working-memory ledger has no torn final record to repair".to_owned(),
                ));
            };
            let discarded_tail = &bytes[discarded_start..];
            let discarded_tail_hash = blake3::hash(discarded_tail).to_hex().to_string();
            let backup_path =
                self.write_repair_backup(line, discarded_tail, &discarded_tail_hash)?;

            file.set_len(discarded_start as u64)?;
            file.seek(SeekFrom::End(0))?;
            file.sync_all()?;

            Ok(WorkingMemoryLedgerRepair {
                repaired_line: line,
                retained_records,
                discarded_bytes: discarded_tail.len(),
                discarded_tail_hash,
                backup_path,
            })
        })();
        file.unlock()?;
        result
    }

    fn inspect_bytes(&self, bytes: &[u8]) -> Result<LedgerInspection, WorkingMemoryLedgerError> {
        let lines = physical_lines(bytes);
        let last_nonempty = lines
            .iter()
            .rposition(|(start, end)| !trim_ascii_whitespace(&bytes[*start..*end]).is_empty());
        let mut facts = Vec::new();
        for (index, (start, end)) in lines.iter().copied().enumerate() {
            let record = trim_ascii_whitespace(&bytes[start..end]);
            if record.is_empty() {
                continue;
            }
            match serde_json::from_slice::<WorkingMemoryFact>(record) {
                Ok(fact) => {
                    self.validate_loaded_fact(&fact, index + 1)?;
                    facts.push(fact);
                }
                // A power loss can tear only the final append. Earlier code
                // silently skipped it, then allowed the next writer to append
                // after the torn bytes; that turns recoverable tail damage
                // into middle-of-journal corruption. Keep the exact bytes for
                // an explicit root-owned repair instead.
                Err(error) if Some(index) == last_nonempty => {
                    return Ok(LedgerInspection::TornFinalRecord {
                        line: index + 1,
                        retained_records: facts.len(),
                        discarded_start: start,
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
        Ok(LedgerInspection::Valid { facts })
    }

    fn validate_loaded_fact(
        &self,
        fact: &WorkingMemoryFact,
        line: usize,
    ) -> Result<(), WorkingMemoryLedgerError> {
        // A ledger is durable input to descendant prompts, not merely an
        // append target. Revalidate every record on read so a misplaced or
        // manually-corrupted JSONL entry cannot cross task-tree boundaries or
        // masquerade as a reviewed fact.
        fact.validate()?;
        if fact.state == WorkingMemoryState::Draft {
            return Err(WorkingMemoryLedgerError::Invalid(format!(
                "ledger record at line {line} is draft; drafts are not durable"
            )));
        }
        if fact.state == WorkingMemoryState::Accepted
            && fact
                .evidence_ref
                .as_deref()
                .is_none_or(|reference| reference.trim().is_empty())
        {
            return Err(WorkingMemoryLedgerError::Invalid(format!(
                "accepted ledger record at line {line} lacks a non-empty evidence_ref"
            )));
        }
        if fact.task_tree_id != self.root_session_id {
            return Err(WorkingMemoryLedgerError::Invalid(format!(
                "ledger record at line {line} belongs to task tree {:?}, not {:?}",
                fact.task_tree_id, self.root_session_id
            )));
        }
        Ok(())
    }

    fn write_repair_backup(
        &self,
        line: usize,
        discarded_tail: &[u8],
        discarded_tail_hash: &str,
    ) -> Result<PathBuf, WorkingMemoryLedgerError> {
        let parent = self.path.parent().ok_or_else(|| {
            WorkingMemoryLedgerError::Invalid("ledger path has no parent".to_owned())
        })?;
        let backup_dir = parent.join("task-ledger-repairs");
        std::fs::create_dir_all(&backup_dir)?;
        let hash_prefix = &discarded_tail_hash[..16];
        let backup_path = backup_dir.join(format!("torn-line-{line}-{hash_prefix}.tail"));
        match OpenOptions::new()
            .create_new(true)
            .write(true)
            .open(&backup_path)
        {
            Ok(mut backup) => {
                backup.write_all(discarded_tail)?;
                backup.sync_all()?;
            }
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
                let existing = std::fs::read(&backup_path)?;
                if existing != discarded_tail {
                    return Err(WorkingMemoryLedgerError::Invalid(format!(
                        "refusing repair: existing backup {:?} does not match torn tail",
                        backup_path
                    )));
                }
            }
            Err(error) => return Err(error.into()),
        }
        Ok(backup_path)
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
        if fact.state == WorkingMemoryState::Draft {
            return Err(WorkingMemoryLedgerError::ClaimDenied {
                reason: ClaimDenyReason::DraftNotPersistable,
            });
        }
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

enum LedgerInspection {
    Valid {
        facts: Vec<WorkingMemoryFact>,
    },
    TornFinalRecord {
        line: usize,
        retained_records: usize,
        discarded_start: usize,
        message: String,
    },
}

fn physical_lines(bytes: &[u8]) -> Vec<(usize, usize)> {
    let mut lines = Vec::new();
    let mut start = 0;
    for (index, byte) in bytes.iter().copied().enumerate() {
        if byte == b'\n' {
            lines.push((start, index));
            start = index + 1;
        }
    }
    if start < bytes.len() {
        lines.push((start, bytes.len()));
    }
    lines
}

fn trim_ascii_whitespace(bytes: &[u8]) -> &[u8] {
    let start = bytes
        .iter()
        .position(|byte| !byte.is_ascii_whitespace())
        .unwrap_or(bytes.len());
    let end = bytes
        .iter()
        .rposition(|byte| !byte.is_ascii_whitespace())
        .map_or(start, |index| index + 1);
    &bytes[start..end]
}

/// Stable, delimiter-safe identity for one promoted revision.  Fact IDs and
/// session IDs are model-visible strings, so never place them unescaped in the
/// marker itself.
fn promotion_marker(fact: &WorkingMemoryFact) -> String {
    let identity = format!(
        "{}\u{1f}{}\u{1f}{}",
        fact.task_tree_id, fact.fact_id, fact.revision
    );
    format!(
        "lumen-task-tree-promotion:{}",
        blake3::hash(identity.as_bytes()).to_hex()
    )
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
    fn accepted_snapshot_changes_only_at_a_new_journal_boundary() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        let empty = ledger.accepted_snapshot().unwrap();
        assert_eq!(empty.record_count, 0);
        assert_eq!(empty.accepted_count, 0);

        ledger
            .propose(fact("snapshot-fact", 1, "child", "observed"))
            .unwrap();
        let proposed = ledger.accepted_snapshot().unwrap();
        assert_eq!(proposed.accepted_count, 0);
        assert_ne!(empty.journal_hash, proposed.journal_hash);

        ledger
            .review(
                "root",
                fact("snapshot-fact", 2, "root", "observed"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        let accepted = ledger.accepted_snapshot().unwrap();
        assert_eq!(accepted.accepted_count, 1);
        assert_ne!(proposed.accepted_set_hash, accepted.accepted_set_hash);
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
        assert!(matches!(
            error,
            WorkingMemoryLedgerError::ClaimDenied {
                reason: ClaimDenyReason::MissingEvidence
            }
        ));
        assert!(ledger.accepted_facts().unwrap().is_empty());
    }

    #[test]
    fn advisor_tui_daemon_cannot_accept_even_with_root_session_id() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        ledger
            .propose(fact("fact-a", 1, "child", "unreviewed"))
            .unwrap();
        for actor in [
            ClaimAuthorityActor::Advisor,
            ClaimAuthorityActor::Tui,
            ClaimAuthorityActor::Daemon,
            ClaimAuthorityActor::Kairos,
            ClaimAuthorityActor::Mcp,
        ] {
            let error = ledger
                .review_with_authority(
                    actor,
                    "root",
                    fact("fact-a", 2, "root", "spoofed"),
                    WorkingMemoryState::Accepted,
                )
                .unwrap_err();
            assert!(
                matches!(error, WorkingMemoryLedgerError::ClaimDenied { reason } if reason.code().contains("cannot_accept")),
                "actor {actor:?} error {error}"
            );
        }
        assert!(ledger.accepted_facts().unwrap().is_empty());
    }

    #[test]
    fn happy_path_proposal_host_verified_accepted_snapshot_manifest() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        ledger
            .propose(fact("fact-a", 1, "child", "observed"))
            .unwrap();
        ledger
            .review(
                "root",
                fact("fact-a", 2, "root", "observed"),
                WorkingMemoryState::HostVerified,
            )
            .unwrap();
        assert!(ledger.accepted_facts().unwrap().is_empty());
        ledger
            .review(
                "root",
                fact("fact-a", 3, "root", "observed"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        let snapshot = ledger.accepted_snapshot().unwrap();
        assert_eq!(snapshot.accepted_count, 1);
        let mut manifest = crate::context_manifest::ContextManifestV1 {
            schema_version: 1,
            task_tree_id: "root".into(),
            node_id: "child".into(),
            root_session_id: "root".into(),
            immediate_parent_id: Some("root".into()),
            lineage_path: vec!["root".into(), "child".into()],
            immutable_assignment_ref: "artifact://assignment".into(),
            immutable_assignment_hash: "sha256:assignment".into(),
            user_objective_ref: "artifact://objective".into(),
            task_contract_hash: "sha256:contract".into(),
            accepted_snapshot_ref: String::new(),
            accepted_snapshot_hash: String::new(),
            tool_catalog_hash: "sha256:tools".into(),
            permitted_tool_contract_hashes: vec!["sha256:a".into()],
            capability_grant_id: "grant-1".into(),
            policy_revision: 1,
            admission_profile: "governed_tree_development".into(),
            budget_reservation_id: "budget-1".into(),
            deadline_unix: 2_000_000_000,
            permitted_artifact_refs: vec![],
            model_selection_ref: None,
            parent_compaction_hash: None,
            producer_version: "2.0.0-alpha.1".into(),
            created_at_unix: 1_000_000_000,
        };
        manifest
            .bind_accepted_snapshot(&snapshot, "ledger://accepted")
            .unwrap();
        let hash = manifest.manifest_hash().unwrap();
        let admitted = crate::context_manifest::admit_context_manifest(
            &crate::context_manifest::ManifestAdmissionRequest {
                mode: crate::context_manifest::ManifestAdmissionMode::GovernedSpawn,
                manifest: Some(&manifest),
                live_snapshot: Some(&snapshot),
                expected_manifest_hash: Some(&hash),
                expected_root_session_id: Some("root"),
                expected_node_id: Some("child"),
                expected_parent_id: Some("root"),
            },
        )
        .unwrap();
        assert_eq!(admitted, hash);
        // Same hash must remain stable for resume.
        assert_eq!(manifest.manifest_hash().unwrap(), hash);
    }

    #[test]
    fn malformed_accepted_record_without_evidence_fails_closed_on_reload() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        let mut malformed = fact("fact-a", 1, "root", "unproven durable claim");
        malformed.state = WorkingMemoryState::Accepted;
        malformed.evidence_ref = None;
        std::fs::write(
            ledger.path(),
            format!("{}\n", serde_json::to_string(&malformed).unwrap()),
        )
        .unwrap();

        let error = ledger.accepted_facts().unwrap_err();
        assert!(matches!(
            error,
            WorkingMemoryLedgerError::Invalid(message)
                if message.contains("accepted ledger record") && message.contains("evidence_ref")
        ));
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
        let root_backend = WorkingMemoryLedgerBackend::new(ledger.clone());
        let child_backend = WorkingMemoryLedgerBackend::for_child(ledger.clone());
        let proposed = BackendFact {
            branch_id: "branch-a".to_owned(),
            fact_id: "fact-a".to_owned(),
            revision: 1,
            kind: TaskTreeMemoryFactKind::Fact,
            evidence_ref: Some("test://evidence".to_owned()),
            confidence: 80,
            text: "child observation".to_owned(),
        };
        let receipt = child_backend.propose("child", proposed).await.unwrap();
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
        // Child-stamped backend cannot accept even with root session id string.
        let error = child_backend
            .review("root", review.clone(), TaskTreeMemoryReviewState::Accepted)
            .await
            .unwrap_err();
        assert!(
            error.contains("claim.") || error.contains("child") || error.contains("only root"),
            "unexpected: {error}"
        );

        let receipt = root_backend
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
    async fn advisor_stamped_backend_cannot_accept_with_root_session_id() {
        let temp = tempfile::tempdir().unwrap();
        let ledger = WorkingMemoryLedger::with_path("root", temp.path().join("ledger.jsonl"));
        ledger.propose(fact("fact-a", 1, "child", "obs")).unwrap();
        let advisor = WorkingMemoryLedgerBackend::with_review_actor(
            ledger.clone(),
            ClaimAuthorityActor::Advisor,
        );
        let error = advisor
            .review(
                "root",
                BackendFact {
                    branch_id: "root".into(),
                    fact_id: "fact-a".into(),
                    revision: 2,
                    kind: TaskTreeMemoryFactKind::Fact,
                    evidence_ref: Some("test://e".into()),
                    confidence: 90,
                    text: "spoof".into(),
                },
                TaskTreeMemoryReviewState::Accepted,
            )
            .await
            .unwrap_err();
        assert!(
            error.contains("advisor") || error.contains("claim."),
            "{error}"
        );
        assert!(ledger.accepted_facts().unwrap().is_empty());
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
    fn user_authorized_promotion_is_evidence_preserving_and_idempotent() {
        let temp = tempfile::tempdir().unwrap();
        let storage = MemoryStorage::with_paths(
            temp.path().join("global-memory"),
            temp.path().join("workspace-memory"),
        );
        let ledger = WorkingMemoryLedger::for_task_tree(&storage, "root");

        ledger
            .propose(fact("build-status", 1, "child", "cargo check completed"))
            .unwrap();
        ledger
            .review(
                "root",
                fact("build-status", 2, "root", "cargo check passed"),
                WorkingMemoryState::Accepted,
            )
            .unwrap();
        let mut transient = fact("current-progress", 1, "child", "still running");
        transient.kind = TaskTreeMemoryFactKind::Progress;
        ledger.propose(transient.clone()).unwrap();
        transient.revision = 2;
        transient.text = "complete soon".to_owned();
        ledger
            .review("root", transient, WorkingMemoryState::Accepted)
            .unwrap();

        let first = ledger
            .promote_accepted_facts_to_workspace_memory(&storage)
            .unwrap();
        assert_eq!(first.promoted, vec![("build-status".to_owned(), 2)]);
        let content = std::fs::read_to_string(storage.workspace_memory_file()).unwrap();
        assert!(content.contains("cargo check passed"));
        assert!(content.contains("test://evidence"));
        assert!(!content.contains("complete soon"));

        let second = ledger
            .promote_accepted_facts_to_workspace_memory(&storage)
            .unwrap();
        assert_eq!(second.promoted_count(), 0);
        assert_eq!(
            std::fs::read_to_string(storage.workspace_memory_file()).unwrap(),
            content,
            "a repeated user command must not duplicate an already-promoted revision"
        );
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

    #[test]
    fn root_repair_preserves_torn_tail_before_restoring_appendability() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("ledger.jsonl");
        let ledger = WorkingMemoryLedger::with_path("root", &path);
        ledger.propose(fact("fact-a", 1, "child", "valid")).unwrap();
        let torn_tail = b"{torn\n\n";
        std::fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap()
            .write_all(torn_tail)
            .unwrap();

        let repair = ledger.repair_torn_final_record("root").unwrap();
        assert_eq!(repair.repaired_line, 2);
        assert_eq!(repair.retained_records, 1);
        assert_eq!(repair.discarded_bytes, torn_tail.len());
        assert_eq!(
            repair.discarded_tail_hash,
            blake3::hash(torn_tail).to_hex().to_string()
        );
        assert_eq!(std::fs::read(&repair.backup_path).unwrap(), torn_tail);

        let facts = ledger.load_all().unwrap();
        assert_eq!(facts.len(), 1);
        ledger
            .propose(fact("fact-b", 1, "child", "append after repair"))
            .unwrap();
        assert_eq!(ledger.load_all().unwrap().len(), 2);
    }

    #[test]
    fn non_root_cannot_repair_or_discard_a_torn_tail() {
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
        let before = std::fs::read(&path).unwrap();

        assert!(matches!(
            ledger.repair_torn_final_record("child"),
            Err(WorkingMemoryLedgerError::UnauthorizedReview { .. })
        ));
        assert_eq!(std::fs::read(&path).unwrap(), before);
        assert!(matches!(
            ledger.load_all(),
            Err(WorkingMemoryLedgerError::TornFinalRecord { .. })
        ));
    }

    #[test]
    fn repair_refuses_middle_corruption_without_truncating() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("ledger.jsonl");
        let ledger = WorkingMemoryLedger::with_path("root", &path);
        let valid_after = serde_json::to_string(&fact("fact-b", 1, "child", "later")).unwrap();
        let bytes = format!("{{bad}}\n{valid_after}\n").into_bytes();
        std::fs::write(&path, &bytes).unwrap();

        assert!(matches!(
            ledger.repair_torn_final_record("root"),
            Err(WorkingMemoryLedgerError::CorruptRecord { line: 1, .. })
        ));
        assert_eq!(std::fs::read(&path).unwrap(), bytes);
    }

    #[test]
    fn final_non_utf8_tail_is_recoverable_but_middle_non_utf8_is_not() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("ledger.jsonl");
        let ledger = WorkingMemoryLedger::with_path("root", &path);
        ledger.propose(fact("fact-a", 1, "child", "valid")).unwrap();
        std::fs::OpenOptions::new()
            .append(true)
            .open(&path)
            .unwrap()
            .write_all(b"{\xff")
            .unwrap();
        assert!(matches!(
            ledger.load_all(),
            Err(WorkingMemoryLedgerError::TornFinalRecord { line: 2, .. })
        ));
        ledger.repair_torn_final_record("root").unwrap();
        assert_eq!(ledger.load_all().unwrap().len(), 1);

        std::fs::write(&path, b"{\xff\n{also-bad}").unwrap();
        assert!(matches!(
            ledger.repair_torn_final_record("root"),
            Err(WorkingMemoryLedgerError::CorruptRecord { line: 1, .. })
        ));
    }
}
