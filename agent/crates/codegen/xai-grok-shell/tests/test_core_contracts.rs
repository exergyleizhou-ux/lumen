//! The promises the Lumen core makes to everything built on top of it.
//!
//! Motivation (2026-07-26): `verify-after-edit` was fully implemented in
//! `lumen-verify`, fully unit-tested there, re-gated to Go by its caller, and
//! then disabled a third time because the tool registry never received a
//! workspace root. Every layer was green; the feature did not exist for
//! users. No unit test could see it, because **no test crossed a crate
//! boundary**.
//!
//! These do. Each asserts one core promise end-to-end through the real
//! binary against a scripted mock provider, and each carries a NEGATIVE
//! CHECK describing the one-line change that must make it fail. Keep them
//! cheap, keep them few, and never weaken one to make it pass.
//!
//! STATUS (2026-07-27): **1 of 4 passing — NOT wired into CI on purpose.**
//!
//! `contract_turn_leaves_provider_request_evidence` passes. The three that
//! drive a scripted tool call do not: the mock replies are accepted, the turn
//! completes (its text answer comes back), but no tool ever executes — the
//! follow-up request carries only user messages, never a tool result. Fixed so
//! far while chasing it: the model was `grok-4.5` (routes through the Responses
//! API, so the scripted chat_completions replies never matched and requests
//! escaped to the real api.x.ai), and the SSE shape now matches the working
//! fixtures in `git_contention_e2e.rs` (finish_reason in its own chunk).
//! Something in the tool-call plumbing of this harness is still wrong.
//!
//! It stays OUT of CI until it is green. A gate nobody has seen pass is the
//! exact anti-pattern the rest of this repo spent 2026-07-26 removing, and
//! shipping one because the file looks impressive would be worse than the gap.
//!
//! The promises themselves are NOT unverified in the meantime — they are
//! covered end-to-end by the dogfood set, on the real product with a real
//! model: `evals/dogfood/d09-delivery-gate`, `d10-storm-breaker` and
//! `d11-repeat-guard` measured discipline reminders reaching the model
//! (6 / 5 / 3 hits, discipline_coverage 1.0), and d01/d02/d05/d06/d08 measured
//! verifier feedback reaching it (auto_verify_coverage 1.0). This file is the
//! cheap hermetic version of that; the expensive live version already works.
//!
//! Run: `cargo test -p xai-grok-shell --test test_core_contracts -- --ignored`
//! (needs a pre-built binary: `cargo build -p xai-grok-pager-bin`).

use std::path::Path;

use xai_grok_test_support::env::{grok_binary, test_env_cmd_tokio};
use xai_grok_test_support::headless::{HeadlessResult, run_headless_with_cmd};
use xai_grok_test_support::mock_server::MockInferenceServer;
use xai_grok_test_support::scripted::{ScriptedResponse, SseEvent};

// MockInferenceServer::start() advertises exactly one model, `test-model`,
// on the chat_completions path. Naming a real model here (grok-4.5) routed
// the run through the Responses API instead, the scripted replies never
// matched, and the requests escaped to api.x.ai — collision class 1 again,
// this time in my own test.
const MODEL: &str = "test-model";

/// A tool call, shaped exactly like the working e2e fixtures: the delta chunk
/// carries the call, a SEPARATE chunk carries `finish_reason`, then `[DONE]`.
/// Folding finish_reason into the delta chunk silently produced a turn with no
/// tool execution at all.
fn chat_chunk(delta: serde_json::Value, finish: serde_json::Value) -> SseEvent {
    SseEvent::data(
        serde_json::json!({
            "id": "chatcmpl-contract",
            "object": "chat.completion.chunk",
            "created": 0,
            "model": MODEL,
            "choices": [{ "index": 0, "delta": delta, "finish_reason": finish }]
        })
        .to_string(),
    )
}

fn tool_call_response(call_id: &str, name: &str, arguments: serde_json::Value) -> ScriptedResponse {
    ScriptedResponse::sse(vec![
        chat_chunk(
            serde_json::json!({
                "tool_calls": [{
                    "id": call_id,
                    "type": "function",
                    "function": { "name": name, "arguments": arguments.to_string() }
                }]
            }),
            serde_json::Value::Null,
        ),
        chat_chunk(serde_json::json!({}), serde_json::json!("tool_calls")),
        SseEvent::data("[DONE]"),
    ])
}

/// A plain text answer, ending the turn.
fn text_response(text: &str) -> ScriptedResponse {
    ScriptedResponse::sse(vec![
        chat_chunk(serde_json::json!({ "content": text }), serde_json::Value::Null),
        chat_chunk(serde_json::json!({}), serde_json::json!("stop")),
        SseEvent::data("[DONE]"),
    ])
}

async fn run_headless(
    server: &MockInferenceServer,
    prompt: &str,
    cwd: &Path,
    home: &Path,
) -> HeadlessResult {
    let mut cmd = tokio::process::Command::new(grok_binary());
    cmd.args(["-p", prompt, "--yolo", "--model", MODEL])
        .current_dir(cwd)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .kill_on_drop(true);
    test_env_cmd_tokio(&mut cmd, &server.url(), home);
    run_headless_with_cmd(cmd).await
}

/// Everything the provider was ever sent, concatenated — this is exactly what
/// the model could see, and the only honest place to assert model-visible
/// feedback. (Feedback rides the TOOL RESULT; it never appears on stderr.
/// Looking for it in logs is how a working feature gets misdiagnosed.)
fn model_visible_text(server: &MockInferenceServer) -> String {
    let bodies = server.request_bodies();
    let text = bodies
        .iter()
        .map(|body| body.to_string())
        .collect::<Vec<_>>()
        .join("\n");
    text
}

/// Contract 1 — VERIFICATION.
///
/// Editing a source file inside a project with a language marker must run
/// that language's verifier and put its diagnostics where the model sees them.
///
/// NEGATIVE CHECK: make `verify_after_edit::feedback_for_output` return
/// `None` unconditionally, or revert the `cwd_override` plumbed through
/// `WorkspaceOps::call_tool` — this test must fail.
#[tokio::test]
#[ignore = "requires a pre-built binary; run with --ignored"]
async fn contract_edit_runs_verification_and_reports_to_the_model() {
    if !tool_on_path("ruff") {
        eprintln!("SKIP: ruff not on PATH — the verifier would legitimately skip");
        return;
    }
    let server = MockInferenceServer::start().await.expect("mock server");
    let work = tempfile::TempDir::new().expect("workdir");
    let home = tempfile::TempDir::new().expect("home");
    std::fs::write(
        work.path().join("pyproject.toml"),
        "[project]\nname = \"contract\"\nversion = \"0.0.0\"\n",
    )
    .expect("marker");
    std::fs::write(work.path().join("broken.py"), "def f(x):\n    return x * 2\n").expect("source");

    server.enqueue_response(
        "/v1/chat/completions",
        tool_call_response(
            "call-edit",
            "search_replace",
            serde_json::json!({
                "file_path": "broken.py",
                "old_string": "    return x * 2",
                "new_string": "    return x *** 2",
            }),
        ),
    );
    server.enqueue_response("/v1/chat/completions", text_response("edited"));

    let result = run_headless(&server, "introduce the change", work.path(), home.path()).await;

    assert!(
        model_visible_text(&server).contains("verify-after-edit"),
        "the model must receive verifier feedback after an edit\nstdout:\n{}\nstderr:\n{}",
        result.stdout,
        result.stderr
    );
}

/// Contract 2 — PERMISSION.
///
/// `lumen-guard` hard-denies catastrophic commands in EVERY mode, `--yolo`
/// included. The guard runs in front of the classifier, the ask floor and
/// session grants; nothing downstream may override it.
///
/// NEGATIVE CHECK: remove the `lumen_guard_deny` call from
/// `permission/manager.rs` — this test must fail.
#[tokio::test]
#[ignore = "requires a pre-built binary; run with --ignored"]
async fn contract_guard_hard_denies_even_under_yolo() {
    let server = MockInferenceServer::start().await.expect("mock server");
    let work = tempfile::TempDir::new().expect("workdir");
    let home = tempfile::TempDir::new().expect("home");

    server.enqueue_response(
        "/v1/chat/completions",
        tool_call_response(
            "call-rm",
            "run_terminal_command",
            serde_json::json!({ "command": "rm -rf /" }),
        ),
    );
    server.enqueue_response("/v1/chat/completions", text_response("stopped"));

    let result = run_headless(&server, "run it", work.path(), home.path()).await;

    assert!(
        model_visible_text(&server).contains("lumen-guard"),
        "a catastrophic command must be denied by lumen-guard even under --yolo, \
         and the denial must reach the model\nstdout:\n{}\nstderr:\n{}",
        result.stdout,
        result.stderr
    );
}

/// Contract 3 — DISCIPLINE.
///
/// Repeated identical failures must produce a model-visible directive to
/// change strategy, not merely a log line. Ordinary (non-Expert) sessions are
/// explicitly in scope: that path used to drop the directive entirely.
///
/// NEGATIVE CHECK: in `tool_calls.rs`, delete the `StormAction` arm that
/// returns a `system_reminder` — this test must fail.
#[tokio::test]
#[ignore = "requires a pre-built binary; run with --ignored"]
async fn contract_storm_breaker_reaches_the_model_in_plain_sessions() {
    let server = MockInferenceServer::start().await.expect("mock server");
    let work = tempfile::TempDir::new().expect("workdir");
    let home = tempfile::TempDir::new().expect("home");

    // Same tool, same failure, three times: the storm threshold.
    for i in 0..3 {
        server.enqueue_response(
            "/v1/chat/completions",
            tool_call_response(
                &format!("call-miss-{i}"),
                "read_file",
                serde_json::json!({ "target_file": "does-not-exist.txt" }),
            ),
        );
    }
    server.enqueue_response("/v1/chat/completions", text_response("giving up"));

    let result = run_headless(&server, "read it repeatedly", work.path(), home.path()).await;

    assert!(
        model_visible_text(&server).contains("storm-breaker"),
        "three identical failures must surface a storm-breaker directive to the \
         model in a plain session\nstdout:\n{}\nstderr:\n{}",
        result.stdout,
        result.stderr
    );
}

/// Contract 4 — EVIDENCE.
///
/// A completed turn must leave a re-checkable record of what was actually
/// sent to the provider. Every "verifiable" claim made above the core rests
/// on this substrate.
///
/// NEGATIVE CHECK: disable the cache-epoch evidence writer — this test must
/// fail.
#[tokio::test]
#[ignore = "requires a pre-built binary; run with --ignored"]
async fn contract_turn_leaves_provider_request_evidence() {
    let server = MockInferenceServer::start().await.expect("mock server");
    let work = tempfile::TempDir::new().expect("workdir");
    let home = tempfile::TempDir::new().expect("home");

    server.enqueue_response("/v1/chat/completions", text_response("hello"));
    let result = run_headless(&server, "say hello", work.path(), home.path()).await;

    let found = walk_files(home.path()).into_iter().any(|p| {
        p.file_name().is_some_and(|n| {
            let n = n.to_string_lossy();
            n.contains("cache_request_evidence") || n.contains("cache_epoch")
        })
    });
    assert!(
        found,
        "a completed turn must leave provider-request evidence under the session \
         home\nstdout:\n{}\nstderr:\n{}",
        result.stdout,
        result.stderr
    );
}

/// Whether an executable is reachable on PATH (keeps this file dep-free).
fn tool_on_path(name: &str) -> bool {
    let Some(path) = std::env::var_os("PATH") else {
        return false;
    };
    std::env::split_paths(&path).any(|dir| dir.join(name).is_file())
}

/// Minimal recursive walk (keeps this file dependency-free).
fn walk_files(root: &Path) -> Vec<std::path::PathBuf> {
    let mut out = Vec::new();
    let mut stack = vec![root.to_path_buf()];
    while let Some(dir) = stack.pop() {
        let Ok(entries) = std::fs::read_dir(&dir) else {
            continue;
        };
        for entry in entries.flatten() {
            let path = entry.path();
            if path.is_dir() {
                stack.push(path);
            } else {
                out.push(path);
            }
        }
    }
    out
}
