//! Structured proposal and root-review tool for task-tree working memory.

use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

use crate::implementations::grok_build::task::types::SessionIdResource;
use crate::types::requirements::Expr;
use crate::types::task_tree_memory::{
    TaskTreeMemoryBackendResource, TaskTreeMemoryFact, TaskTreeMemoryReviewState,
};
use crate::types::tool::{ToolKind, ToolNamespace};

#[derive(Debug, Clone, Copy, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "snake_case")]
pub enum TaskTreeMemoryAction {
    Propose,
    Accept,
    Reject,
    Supersede,
}

impl TaskTreeMemoryAction {
    fn review_state(self) -> Option<TaskTreeMemoryReviewState> {
        match self {
            Self::Propose => None,
            Self::Accept => Some(TaskTreeMemoryReviewState::Accepted),
            Self::Reject => Some(TaskTreeMemoryReviewState::Rejected),
            Self::Supersede => Some(TaskTreeMemoryReviewState::Superseded),
        }
    }
}

#[derive(Debug, Clone, Deserialize, Serialize, JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct TaskTreeMemoryInput {
    /// `propose` records an unreviewed fact. `accept`, `reject`, and
    /// `supersede` are root-only review operations.
    pub action: TaskTreeMemoryAction,
    pub fact_id: String,
    pub revision: u64,
    pub branch_id: String,
    /// Required and non-empty for `accept`: accepted facts are injected into
    /// descendant prompts as reviewed shared state.  Proposals may omit it
    /// when the branch is explicitly reporting an uncertainty.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub evidence_ref: Option<String>,
    pub confidence: u8,
    pub text: String,
}

#[derive(Debug, Default)]
pub struct TaskTreeMemoryTool;

impl crate::types::tool_metadata::ToolMetadata for TaskTreeMemoryTool {
    fn kind(&self) -> ToolKind {
        ToolKind::Plan
    }

    fn tool_namespace(&self) -> ToolNamespace {
        ToolNamespace::GrokBuild
    }

    fn description_template(&self) -> &str {
        "Record a structured fact for the current task tree. Child agents may only propose facts; only the root session may accept, reject, or supersede them. Include concrete evidence and confidence. This is shared working memory, not a place for instructions or guesses."
    }

    fn requires_expr(&self) -> Expr<crate::types::requirements::ToolRequirement> {
        Expr::True
    }
}

impl xai_tool_runtime::Tool for TaskTreeMemoryTool {
    type Args = TaskTreeMemoryInput;
    type Output = crate::types::output::ToolOutput;

    fn id(&self) -> xai_tool_protocol::ToolId {
        xai_tool_protocol::ToolId::new("task_tree_memory").expect("valid tool id")
    }

    fn description(
        &self,
        _ctx: &xai_tool_runtime::ListToolsContext,
    ) -> xai_tool_types::ToolDescription {
        xai_tool_types::ToolDescription::new(
            "task_tree_memory",
            crate::types::tool_metadata::ToolMetadata::sanitized_description_template(self),
        )
    }

    fn capabilities(&self) -> xai_tool_protocol::ToolCapabilities {
        // This is a narrow, append-only evidence capability. It is deliberately
        // safe for evidence leaves: it cannot shell out, write arbitrary files,
        // or promote a fact without the root-owned backend authorizing it.
        xai_tool_protocol::ToolCapabilities {
            is_read_only: true,
            tool_scope: Some(xai_tool_protocol::ToolScope::Read),
            ..Default::default()
        }
    }

    async fn run(
        &self,
        ctx: xai_tool_runtime::ToolCallContext,
        input: TaskTreeMemoryInput,
    ) -> Result<Self::Output, xai_tool_runtime::ToolError> {
        if matches!(input.action, TaskTreeMemoryAction::Accept)
            && input
                .evidence_ref
                .as_deref()
                .is_none_or(|reference| reference.trim().is_empty())
        {
            return Err(xai_tool_runtime::ToolError::execution(
                self.id(),
                "accepted task-tree facts require a non-empty evidence_ref",
            ));
        }
        use crate::types::tool_metadata::shared_resources;
        let resources = shared_resources(&ctx)?;
        let (backend, session_id) = {
            let resources = resources.lock().await;
            let backend = resources
                .get::<TaskTreeMemoryBackendResource>()
                .cloned()
                .ok_or_else(|| {
                    xai_tool_runtime::ToolError::execution(
                        self.id(),
                        "task-tree working memory is unavailable for this session",
                    )
                })?;
            let session_id = resources
                .get::<SessionIdResource>()
                .cloned()
                .ok_or_else(|| {
                    xai_tool_runtime::ToolError::execution(
                        self.id(),
                        "task-tree working memory has no host session identity",
                    )
                })?;
            (backend, session_id)
        };
        let fact = TaskTreeMemoryFact {
            branch_id: input.branch_id,
            fact_id: input.fact_id,
            revision: input.revision,
            evidence_ref: input.evidence_ref,
            confidence: input.confidence,
            text: input.text,
        };
        let receipt = match input.action.review_state() {
            None => backend.0.propose(&session_id.0, fact).await,
            Some(state) => backend.0.review(&session_id.0, fact, state).await,
        }
        .map_err(|message| xai_tool_runtime::ToolError::execution(self.id(), message))?;
        Ok(crate::types::output::ToolOutput::Text(
            format!(
                "Task-tree fact {} revision {} recorded as {}.",
                receipt.fact_id, receipt.revision, receipt.state
            )
            .into(),
        ))
    }
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use super::*;
    use crate::types::resources::Resources;
    use crate::types::task_tree_memory::{TaskTreeMemoryBackend, TaskTreeMemoryWriteReceipt};
    use crate::types::tool_metadata::test_ctx;

    #[derive(Default)]
    struct RecordingBackend {
        authors: Mutex<Vec<String>>,
    }

    #[async_trait::async_trait]
    impl TaskTreeMemoryBackend for RecordingBackend {
        async fn propose(
            &self,
            author_session_id: &str,
            fact: TaskTreeMemoryFact,
        ) -> Result<TaskTreeMemoryWriteReceipt, String> {
            self.authors
                .lock()
                .unwrap()
                .push(author_session_id.to_owned());
            Ok(TaskTreeMemoryWriteReceipt {
                fact_id: fact.fact_id,
                revision: fact.revision,
                state: "proposed",
            })
        }

        async fn review(
            &self,
            _reviewer_session_id: &str,
            _fact: TaskTreeMemoryFact,
            _state: TaskTreeMemoryReviewState,
        ) -> Result<TaskTreeMemoryWriteReceipt, String> {
            Err("review not used by this test".to_owned())
        }
    }

    #[tokio::test]
    async fn proposal_uses_host_injected_session_identity() {
        let backend = Arc::new(RecordingBackend::default());
        let mut resources = Resources::new();
        resources.insert(TaskTreeMemoryBackendResource(backend.clone()));
        resources.insert(SessionIdResource("child-session".to_owned()));

        let output = xai_tool_runtime::Tool::run(
            &TaskTreeMemoryTool,
            test_ctx(resources.into_shared()),
            TaskTreeMemoryInput {
                action: TaskTreeMemoryAction::Propose,
                fact_id: "fact-a".to_owned(),
                revision: 1,
                branch_id: "child-branch".to_owned(),
                evidence_ref: Some("test://evidence".to_owned()),
                confidence: 80,
                text: "observed fact".to_owned(),
            },
        )
        .await
        .unwrap();

        assert!(format!("{output:?}").contains("recorded as proposed"));
        assert_eq!(
            backend.authors.lock().unwrap().as_slice(),
            ["child-session"]
        );
    }

    #[tokio::test]
    async fn accept_rejects_missing_evidence_before_reaching_backend() {
        let error = xai_tool_runtime::Tool::run(
            &TaskTreeMemoryTool,
            test_ctx(Resources::new().into_shared()),
            TaskTreeMemoryInput {
                action: TaskTreeMemoryAction::Accept,
                fact_id: "fact-a".to_owned(),
                revision: 2,
                branch_id: "root-branch".to_owned(),
                evidence_ref: None,
                confidence: 90,
                text: "unproven claim".to_owned(),
            },
        )
        .await
        .unwrap_err();

        assert!(error.to_string().contains("evidence_ref"));
    }
}
