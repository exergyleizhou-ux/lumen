use crate::types::requirements::{Expr, ToolRequirement};

use crate::types::tool::{ToolKind, ToolNamespace};

use super::interval::interval_to_human;
use super::types::{ScheduledTask, SchedulerCommand, SchedulerHandle, SchedulerRunStatus};

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize, schemars::JsonSchema)]
pub struct SchedulerListInput {}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize, schemars::JsonSchema)]
#[serde(rename_all = "camelCase")]
pub struct ScheduledTaskSummary {
    pub id: String,
    pub prompt: String,
    pub interval_human: String,
    pub next_fire_at: String,
    pub created_at: String,
    pub recurring: bool,
    #[serde(default)]
    pub consecutive_run_failures: u32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub retry_not_before: Option<String>,
    #[serde(default)]
    pub dead_lettered: bool,
    #[serde(default)]
    pub usage_verification_required: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub daily_token_budget: Option<u64>,
    #[serde(default)]
    pub daily_tokens_used: u64,
    #[serde(default)]
    pub daily_token_budget_exhausted: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_run_status: Option<String>,
    /// `false` means the last run's token total was incomplete and must not
    /// be used as a cost/budget proof.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_run_usage_complete: Option<bool>,
    /// Canonical model identity captured when the latest background child
    /// started. `None` means this is a legacy or otherwise unverifiable
    /// receipt, not the model currently selected in the parent session.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_run_model_id: Option<String>,
    /// Timestamp of the last safe recovery takeover. This intentionally does
    /// not expose the prior scheduler owner identity.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_lease_takeover_at: Option<String>,
}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize, schemars::JsonSchema)]
pub struct SchedulerListOutput {
    pub tasks: Vec<ScheduledTaskSummary>,
    #[serde(default)]
    pub recovery_required: bool,
    #[serde(default)]
    pub quarantined_one_shot_count: usize,
}

impl xai_tool_runtime::ToolOutput for SchedulerListOutput {}

fn summary_from_task(task: ScheduledTask) -> ScheduledTaskSummary {
    let now = chrono::Utc::now();
    let daily_tokens_used = task.daily_token_usage_for(now);
    let daily_token_budget_exhausted = task.daily_token_budget_exhausted(now);
    let next_fire = task
        .next_daily_budget_reset_at(now)
        .map_or_else(
            || task.next_dispatch_at(),
            |reset| task.next_dispatch_at().max(reset),
        )
        .to_rfc3339();
    let created = task.created_at.to_rfc3339();
    let prompt = if task.prompt.len() > 80 {
        let cut = crate::util::floor_char_boundary(&task.prompt, 80);
        format!("{}...", &task.prompt[..cut])
    } else {
        task.prompt
    };
    ScheduledTaskSummary {
        id: task.id,
        prompt,
        interval_human: interval_to_human(task.interval_secs),
        next_fire_at: next_fire,
        created_at: created,
        recurring: task.recurring,
        consecutive_run_failures: task.consecutive_run_failures,
        retry_not_before: task.retry_not_before.map(|time| time.to_rfc3339()),
        dead_lettered: task.dead_lettered,
        usage_verification_required: task.usage_verification_required,
        daily_token_budget: task.daily_token_budget,
        daily_tokens_used,
        daily_token_budget_exhausted,
        last_run_status: task.last_run_receipt.as_ref().map(|receipt| {
            match receipt.status() {
                SchedulerRunStatus::Completed => "completed",
                SchedulerRunStatus::Failed => "failed",
                SchedulerRunStatus::Cancelled => "cancelled",
            }
            .to_owned()
        }),
        last_run_usage_complete: task
            .last_run_receipt
            .as_ref()
            .map(|receipt| !receipt.output_usage_incomplete()),
        last_run_model_id: task
            .last_run_receipt
            .as_ref()
            .and_then(|receipt| receipt.model_id().map(str::to_owned)),
        last_lease_takeover_at: task
            .last_run_lease_takeover
            .as_ref()
            .map(|takeover| takeover.taken_at().to_rfc3339()),
    }
}

#[derive(Debug, Default)]
pub struct SchedulerListTool;

impl crate::types::tool_metadata::ToolMetadata for SchedulerListTool {
    fn kind(&self) -> ToolKind {
        ToolKind::Other
    }

    fn tool_namespace(&self) -> ToolNamespace {
        ToolNamespace::GrokBuild
    }

    fn description_template(&self) -> &str {
        "List all active scheduled tasks with their IDs, prompts, intervals, and next fire times."
    }

    fn requires_expr(&self) -> Expr<ToolRequirement> {
        use super::create::SchedulerCreateTool;
        use crate::types::tool_metadata::ToolMetadata as TM;
        Expr::Value(ToolRequirement::Tool {
            namespace: TM::tool_namespace(&SchedulerCreateTool).to_string(),
            id: xai_tool_runtime::Tool::id(&SchedulerCreateTool).to_string(),
            if_params: None,
        })
    }
}

impl xai_tool_runtime::Tool for SchedulerListTool {
    type Args = SchedulerListInput;
    type Output = SchedulerListOutput;

    fn id(&self) -> xai_tool_protocol::ToolId {
        xai_tool_protocol::ToolId::new("scheduler_list").expect("valid tool id")
    }

    fn description(
        &self,
        _ctx: &::xai_tool_runtime::ListToolsContext,
    ) -> xai_tool_types::ToolDescription {
        xai_tool_types::ToolDescription::new(
            "scheduler_list",
            crate::types::tool_metadata::ToolMetadata::sanitized_description_template(self),
        )
    }

    fn capabilities(&self) -> xai_tool_protocol::ToolCapabilities {
        xai_tool_protocol::ToolCapabilities {
            is_read_only: false,
            tool_scope: Some(xai_tool_protocol::ToolScope::Write),
            ..Default::default()
        }
    }

    #[tracing::instrument(name = "tool.scheduler_list", skip_all)]
    async fn run(
        &self,
        ctx: xai_tool_runtime::ToolCallContext,
        _input: SchedulerListInput,
    ) -> Result<SchedulerListOutput, xai_tool_runtime::ToolError> {
        use crate::types::tool_metadata::shared_resources;
        let resources = shared_resources(&ctx)?;

        let sender = {
            let res = resources.lock().await;
            res.get::<SchedulerHandle>()
                .ok_or_else(|| {
                    xai_tool_runtime::ToolError::custom(
                        "missing_dependency",
                        "missing dependency: SchedulerHandle",
                    )
                })?
                .0
                .clone()
        };

        let (reply_tx, reply_rx) = tokio::sync::oneshot::channel();
        sender
            .send(SchedulerCommand::List { reply: reply_tx })
            .map_err(|_| {
                xai_tool_runtime::ToolError::execution(
                    xai_tool_protocol::ToolId::new("scheduler_list").expect("valid"),
                    "Scheduler actor stopped",
                )
            })?;

        let snapshot = reply_rx.await.map_err(|_| {
            xai_tool_runtime::ToolError::execution(
                xai_tool_protocol::ToolId::new("scheduler_list").expect("valid"),
                "Scheduler actor dropped reply",
            )
        })?;

        let summaries = snapshot.tasks.into_iter().map(summary_from_task).collect();

        Ok(SchedulerListOutput {
            tasks: summaries,
            recovery_required: snapshot.recovery_required,
            quarantined_one_shot_count: snapshot.quarantined_one_shot_count,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::implementations::grok_build::scheduler::types::SchedulerRunReceipt;

    #[test]
    fn legacy_scheduler_list_response_defaults_recovery_health() {
        let restored: SchedulerListOutput =
            serde_json::from_value(serde_json::json!({ "tasks": [] }))
                .expect("old scheduler list response remains readable");
        assert!(!restored.recovery_required);
        assert_eq!(restored.quarantined_one_shot_count, 0);
    }

    #[test]
    fn task_summary_surfaces_failure_health_without_output_text() {
        let now = chrono::Utc::now();
        let mut task = ScheduledTask::new(60, "watch ci".into(), true, true);
        task.last_fired_at = Some(now);
        task.record_terminal_run_status(SchedulerRunStatus::Failed, now);
        task.last_run_receipt = Some(SchedulerRunReceipt::new(
            "run-1".into(),
            SchedulerRunStatus::Failed,
            now,
            7,
            11,
            Some("deepseek-v4-flash".into()),
            true,
        ));
        let summary = summary_from_task(task);

        assert_eq!(summary.consecutive_run_failures, 1);
        assert!(summary.retry_not_before.is_some());
        assert!(!summary.dead_lettered);
        assert!(!summary.usage_verification_required);
        assert_eq!(summary.last_run_status.as_deref(), Some("failed"));
        assert_eq!(summary.last_run_usage_complete, Some(false));
        assert_eq!(
            summary.last_run_model_id.as_deref(),
            Some("deepseek-v4-flash")
        );
    }

    #[test]
    fn task_summary_surfaces_current_daily_token_budget() {
        let now = chrono::Utc::now();
        let mut task = ScheduledTask::new(60, "watch ci".into(), true, true);
        task.set_daily_token_budget(Some(100)).unwrap();
        task.record_verified_daily_token_usage(now, 100);

        let summary = summary_from_task(task);
        assert_eq!(summary.daily_token_budget, Some(100));
        assert_eq!(summary.daily_tokens_used, 100);
        assert!(summary.daily_token_budget_exhausted);
    }

    #[test]
    fn task_summary_surfaces_takeover_time_without_owner_identity() {
        let now = chrono::Utc::now();
        let mut task = ScheduledTask::new(60, "watch ci".into(), true, true);
        task.acquire_run_lease("scheduler:crashed", now, chrono::Duration::seconds(1))
            .unwrap();
        let taken_at = now + chrono::Duration::seconds(2);
        task.acquire_run_lease(
            "scheduler:restored",
            taken_at,
            chrono::Duration::seconds(60),
        )
        .unwrap();

        let encoded = serde_json::to_value(summary_from_task(task)).unwrap();
        assert_eq!(
            encoded["lastLeaseTakeoverAt"],
            serde_json::Value::String(taken_at.to_rfc3339())
        );
        assert!(!encoded.to_string().contains("scheduler:crashed"));
    }
}
