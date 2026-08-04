//! S3 (NG-03D / INV-13): children must never self-commit/push/merge.
//!
//! The hard deny lives in the real `prepare_tool_call` dispatch path
//! (`acp_session_impl/tool_calls.rs`), next to the NG-02A ToolContract
//! admission: command-execution tools at child depth scan their raw arguments
//! for git mutation verbs and are refused before any execution happens. The
//! pure scanner is `xai_grok_memory::tool_contract::child_git_mutation_in`;
//! this test drives the shipped shell entry point end to end.

use super::support::*;
use super::*;

#[tokio::test(flavor = "multi_thread")]
async fn child_dispatch_hard_denies_git_mutation_commands() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) =
                tokio::sync::mpsc::unbounded_channel::<xai_acp_lib::AcpClientMessage>();
            let (persistence_tx, _persistence_rx) =
                tokio::sync::mpsc::unbounded_channel::<PersistenceMsg>();
            let mut actor = create_test_actor(0, 256_000, 85, gateway_tx, persistence_tx).await;
            // The toolset must know the command-execution tool so it resolves
            // to ToolKind::Execute (the git gate is scoped to Execute tools).
            let agent = test_agent_with_tools(vec![
                xai_grok_tools::registry::types::ToolConfig::for_tool::<
                    xai_grok_tools::implementations::opencode::bash::BashTool,
                >(),
            ])
            .await;
            let definitions = agent.tool_bridge().tool_definitions_builtins_only().await;
            let bash_name = definitions
                .iter()
                .map(|definition| definition.function.name.clone())
                .find(|name| name.contains("bash"))
                .expect("BashTool must register a client name containing 'bash'");
            *actor.agent.borrow_mut() = agent;
            actor.tool_context.subagent_depth = 1;

            // The command text executes a git mutation -> hard deny, no
            // execution attempt, no provider call.
            let call = ToolCallResponse {
                id: "call_1".to_string(),
                kind: "function".to_string(),
                function: crate::sampling::types::ToolCallFunction::new(
                    &bash_name,
                    r#"{"command":"git commit -m 'wip' && git push","description":"test"}"#,
                ),
            };
            let mut deferred = Vec::new();
            let result = tokio::time::timeout(
                std::time::Duration::from_secs(5),
                actor.prepare_tool_call(call, &mut deferred),
            )
            .await
            .expect("prepare_tool_call must not hang")
            .expect("prepare_tool_call must not error");
            assert!(
                matches!(result, Err(ToolLoop::NonExistingTool)),
                "child git mutation must be hard denied before execution; got {result:?}"
            );
        })
        .await;
}

#[tokio::test(flavor = "multi_thread")]
async fn child_dispatch_hard_denies_git_merge_even_with_option_values() {
    let local = tokio::task::LocalSet::new();
    local
        .run_until(async {
            let (gateway_tx, _gateway_rx) =
                tokio::sync::mpsc::unbounded_channel::<xai_acp_lib::AcpClientMessage>();
            let (persistence_tx, _persistence_rx) =
                tokio::sync::mpsc::unbounded_channel::<PersistenceMsg>();
            let mut actor = create_test_actor(0, 256_000, 85, gateway_tx, persistence_tx).await;
            let agent = test_agent_with_tools(vec![
                xai_grok_tools::registry::types::ToolConfig::for_tool::<
                    xai_grok_tools::implementations::opencode::bash::BashTool,
                >(),
            ])
            .await;
            let definitions = agent.tool_bridge().tool_definitions_builtins_only().await;
            let bash_name = definitions
                .iter()
                .map(|definition| definition.function.name.clone())
                .find(|name| name.contains("bash"))
                .expect("BashTool must register");
            *actor.agent.borrow_mut() = agent;
            actor.tool_context.subagent_depth = 2;

            let call = ToolCallResponse {
                id: "call_2".to_string(),
                kind: "function".to_string(),
                function: crate::sampling::types::ToolCallFunction::new(
                    &bash_name,
                    r#"{"command":"git -C /tmp/repo merge feature-branch","description":"test"}"#,
                ),
            };
            let mut deferred = Vec::new();
            let result = tokio::time::timeout(
                std::time::Duration::from_secs(5),
                actor.prepare_tool_call(call, &mut deferred),
            )
            .await
            .expect("prepare_tool_call must not hang")
            .expect("prepare_tool_call must not error");
            assert!(
                matches!(result, Err(ToolLoop::NonExistingTool)),
                "git -C <dir> merge must be denied at child depth; got {result:?}"
            );
        })
        .await;
}
