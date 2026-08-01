//! Host-owned capability for reviewed task-tree working memory.
//!
//! The tool crate defines the port; the shell owns the concrete JSONL ledger.
//! This avoids a dependency cycle and, more importantly, prevents a model from
//! supplying a path or root identity to gain arbitrary file-write access.

use std::sync::Arc;

/// A structured fact proposal or reviewed revision.
#[derive(Debug, Clone)]
pub struct TaskTreeMemoryFact {
    pub branch_id: String,
    pub fact_id: String,
    pub revision: u64,
    pub evidence_ref: Option<String>,
    pub confidence: u8,
    pub text: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TaskTreeMemoryReviewState {
    Accepted,
    Rejected,
    Superseded,
}

impl TaskTreeMemoryReviewState {
    pub const fn label(self) -> &'static str {
        match self {
            Self::Accepted => "accepted",
            Self::Rejected => "rejected",
            Self::Superseded => "superseded",
        }
    }
}

/// Result returned after one append-only ledger write.
#[derive(Debug, Clone)]
pub struct TaskTreeMemoryWriteReceipt {
    pub fact_id: String,
    pub revision: u64,
    pub state: &'static str,
}

/// SessionActor-owned backend for the task-tree ledger.
///
/// The backend receives the actual caller session ID from an injected resource;
/// neither it nor the model accepts a filesystem path or a root-session ID.
#[async_trait::async_trait]
pub trait TaskTreeMemoryBackend: Send + Sync + 'static {
    async fn propose(
        &self,
        author_session_id: &str,
        fact: TaskTreeMemoryFact,
    ) -> Result<TaskTreeMemoryWriteReceipt, String>;

    async fn review(
        &self,
        reviewer_session_id: &str,
        fact: TaskTreeMemoryFact,
        state: TaskTreeMemoryReviewState,
    ) -> Result<TaskTreeMemoryWriteReceipt, String>;
}

/// Ephemeral resource injected only into sessions that belong to a task tree
/// with workspace memory enabled.
#[derive(Clone)]
pub struct TaskTreeMemoryBackendResource(pub Arc<dyn TaskTreeMemoryBackend>);

impl std::fmt::Debug for TaskTreeMemoryBackendResource {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("TaskTreeMemoryBackendResource").finish()
    }
}

crate::register_resource!(
    "grok_build",
    "TaskTreeMemoryBackendResource",
    TaskTreeMemoryBackendResource
);
