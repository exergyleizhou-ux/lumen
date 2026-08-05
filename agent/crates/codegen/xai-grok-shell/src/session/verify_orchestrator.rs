//! Verification-obligation loop (DEBT-033 B2): profile-triggered
//! verify-after-edit wiring for the session loop.
//!
//! Design constraints:
//! - Zero behavior change by default: verification runs only when an edit
//!   tool completed, the session effort is `Max` (or the profile forces
//!   verify-first), and a lumen-verify config is enabled.
//! - Session-scoped `RepairLoop` state lives in a process-local registry
//!   (same pattern as `prompt_cache_registry`): no SessionActor struct churn.
//! - The lumen-verify pipeline (`run_after_edit`) is project-marker-aware,
//!   workspace-boundary-checked and never runs package runners.
//! - INV-VO-03: exceeding the repair cap fails the loop closed; the effect is
//!   never silently signed off.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{Mutex, OnceLock};

use lumen_discipline::{
    DEFAULT_MAX_REPAIR_ATTEMPTS, RepairAttemptStatus, RepairLoop, RepairLoopState,
};

/// Edit tools whose completed call triggers verification.
const EDIT_TOOLS: &[&str] = &[
    "search_replace",
    "write_file",
    "edit_file",
    "multi_edit",
    "delete_range",
    "delete_symbol",
    "move_file",
];

/// Argument keys that carry the edited file path (matches the telemetry
/// extraction keys in the tool loop, plus move_file's pair).
const FILE_PATH_KEYS: &[&str] = &[
    "file_path",
    "target_file",
    "filePath",
    "path",
    "source_path",
    "destination_path",
];

pub fn is_edit_tool(tool_name: &str) -> bool {
    EDIT_TOOLS.contains(&tool_name)
}

/// Extract changed-file paths from a completed edit tool call.
pub fn edit_tool_changed_files(tool_name: &str, raw_arguments: &str) -> Vec<PathBuf> {
    if !is_edit_tool(tool_name) {
        return Vec::new();
    }
    let Ok(parsed) = serde_json::from_str::<serde_json::Value>(raw_arguments) else {
        return Vec::new();
    };
    FILE_PATH_KEYS
        .iter()
        .filter_map(|key| parsed.get(*key).and_then(|v| v.as_str()))
        .map(PathBuf::from)
        .collect()
}

/// Whether the session should verify after this edit. Default-off: `Max`
/// effort or an explicit verify-first profile, plus an enabled verify config.
pub fn should_verify(effort_is_max: bool, profile_forces_verify_first: bool, verify_enabled: bool) -> bool {
    verify_enabled && (effort_is_max || profile_forces_verify_first)
}

/// Result of one orchestrated verification pass.
#[derive(Debug, Clone)]
pub struct VerifyOutcome {
    pub state: RepairLoopState,
    pub attempts: u32,
    pub max_attempts: u32,
    pub verified_files: Vec<PathBuf>,
    /// Formatted diagnostics (empty when all checks passed or skipped).
    pub diagnostics: String,
}

/// Cap on diagnostics embedded in a repair instruction (a user turn should
/// stay small; full diagnostics are in the journal event).
const REPAIR_INSTRUCTION_DIAGNOSTICS_CAP_CHARS: usize = 2000;

/// Build the repair instruction injected into the conversation when
/// verification failed with attempts remaining (DEBT-033 B2): the next model
/// request sees it and fixes the failing change.
pub fn build_repair_instruction(attempts: u32, max_attempts: u32, diagnostics: &str) -> String {
    let mut instruction = format!(
        "Your last edit failed verification (attempt {attempts} of {max_attempts}). Fix the change so the checks pass."
    );
    if !diagnostics.is_empty() {
        instruction.push_str("\nVerification diagnostics:\n");
        let capped: String = diagnostics.chars().take(REPAIR_INSTRUCTION_DIAGNOSTICS_CAP_CHARS).collect();
        instruction.push_str(&capped);
        if capped.chars().count() < diagnostics.chars().count() {
            instruction.push_str("\n[diagnostics truncated]");
        }
    }
    instruction
}

/// Run the lumen-verify pipeline for the changed files and fold the result
/// into the session repair loop. Never panics; errors degrade to an
/// `Inconclusive` attempt (which still consumes the cap, INV-VO-02: an
/// unverifiable effect is never signed off).
pub fn run_verification(
    workspace_root: &Path,
    changed_files: &[PathBuf],
    verify_cfg: &lumen_verify::config::Config,
    loop_state: &mut RepairLoop,
) -> VerifyOutcome {
    let mut diagnostics = String::new();
    let mut verified = Vec::new();
    for file in changed_files {
        match lumen_verify::run_after_edit(workspace_root, file, verify_cfg) {
            Ok(Some(result)) => {
                verified.push(file.clone());
                if !result.ok {
                    diagnostics.push_str(&lumen_verify::format_diagnostics(&result.step_results));
                }
                if result.ok {
                    loop_state.record(RepairAttemptStatus::Succeeded, 0);
                } else {
                    loop_state.record(RepairAttemptStatus::Failed, 0);
                }
                if !loop_state.may_repair() {
                    break;
                }
            }
            Ok(None) => {
                // Project marker not found / tool missing: inconclusive.
                loop_state.record(RepairAttemptStatus::Inconclusive, 0);
            }
            Err(error) => {
                tracing::warn!(%error, file = %file.display(), "verify after edit failed to run");
                loop_state.record(RepairAttemptStatus::Inconclusive, 0);
            }
        }
    }
    VerifyOutcome {
        state: loop_state.state,
        attempts: loop_state.attempts,
        max_attempts: loop_state.max_repair_attempts,
        verified_files: verified,
        diagnostics,
    }
}

// ---------------------------------------------------------------------------
// Session-scoped repair-loop registry (process-local, like prompt_cache_registry)
// ---------------------------------------------------------------------------

#[derive(Debug)]
struct Entry {
    last_used: std::time::Instant,
    loop_state: RepairLoop,
}

static REGISTRY: OnceLock<Mutex<HashMap<String, Entry>>> = OnceLock::new();
const MAX_TRACKED_SESSIONS: usize = 256;

fn map() -> &'static Mutex<HashMap<String, Entry>> {
    REGISTRY.get_or_init(|| Mutex::new(HashMap::new()))
}

fn enforce_cap(guard: &mut HashMap<String, Entry>) {
    while guard.len() > MAX_TRACKED_SESSIONS {
        let Some(oldest) = guard
            .iter()
            .min_by_key(|(_, e)| e.last_used)
            .map(|(k, _)| k.clone())
        else {
            return;
        };
        guard.remove(&oldest);
    }
}

/// Get (or create) the session's repair loop.
pub fn session_repair_loop(session_id: &str) -> RepairLoop {
    let Ok(mut guard) = map().lock() else {
        return RepairLoop::new(DEFAULT_MAX_REPAIR_ATTEMPTS, u32::MAX);
    };
    let entry = guard.entry(session_id.to_string()).or_insert_with(|| Entry {
        last_used: std::time::Instant::now(),
        loop_state: RepairLoop::new(DEFAULT_MAX_REPAIR_ATTEMPTS, u32::MAX),
    });
    entry.last_used = std::time::Instant::now();
    entry.loop_state
}

/// Store the updated loop state back (the session owns it between turns).
pub fn store_session_repair_loop(session_id: &str, loop_state: RepairLoop) {
    let Ok(mut guard) = map().lock() else {
        return;
    };
    guard
        .entry(session_id.to_string())
        .and_modify(|e| {
            e.last_used = std::time::Instant::now();
            e.loop_state = loop_state;
        })
        .or_insert_with(|| Entry {
            last_used: std::time::Instant::now(),
            loop_state,
        });
    enforce_cap(&mut guard);
}

/// Drop the loop when the session ends (best-effort).
pub fn drop_session(session_id: &str) {
    if let Ok(mut guard) = map().lock() {
        guard.remove(session_id);
    }
}

/// Derive the task-tree lifecycle journal dir from the session's memory
/// workspace (same layout as `subagent_coordinator`).
pub fn task_tree_journal_dir(memory_root: Option<&Path>) -> Option<PathBuf> {
    memory_root.map(|root| root.join("task-tree-lifecycle"))
}

/// Append a governed verify/repair event to the task-tree lifecycle journal
/// (same pattern as `subagent_coordinator`: journal dir + blake3-truncated
/// root-session filename). Best-effort: journaling failures degrade to a
/// warning, never to a verification failure (evidence is non-authority for
/// the pipeline itself, INV-VO events are audit records).
///
/// The call-site wiring (memory-root plumbing + root-session authorization)
/// is the documented next-cycle hook; this helper is fully unit-tested so the
/// wiring is a plain call.
pub fn append_verify_event(
    journal_dir: &Path,
    root_session_id: &str,
    kind: xai_grok_memory::lifecycle_journal::GovernedLifecycleEventKind,
    detail: Option<serde_json::Value>,
) -> std::io::Result<()> {
    std::fs::create_dir_all(journal_dir)?;
    let file_name = format!("{}.jsonl", &blake3::hash(root_session_id.as_bytes()).to_hex()[..16]);
    let mut journal = xai_grok_memory::lifecycle_journal::LifecycleJournal::at_path(
        root_session_id,
        journal_dir.join(file_name),
    );
    let sequence = journal.events().len() as u64;
    let occurred_at = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let mut event = xai_grok_memory::lifecycle_journal::GovernedLifecycleEventV1 {
        event_id: format!("verify-{sequence}"),
        task_tree_id: root_session_id.to_string(),
        node_id: "verify-orchestrator".into(),
        owner_session_id: root_session_id.to_string(),
        sequence,
        causal_parent: journal.events().last().map(|e| e.sequence),
        kind,
        source: xai_grok_memory::lifecycle_journal::GovernedLifecycleEventSource::Actor,
        lease_id: None,
        contract_hash: None,
        policy_revision: 1,
        evidence_refs: Vec::new(),
        occurred_at,
        payload_hash: String::new(),
        prev_payload_hash: None,
        detail,
    };
    event.payload_hash = event
        .compute_payload_hash()
        .map_err(|e| std::io::Error::other(format!("payload hash: {e}")))?;
    journal
        .append(event)
        .map_err(|e| std::io::Error::other(format!("journal append: {e}")))?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn edit_tool_recognition() {
        assert!(is_edit_tool("search_replace"));
        assert!(is_edit_tool("multi_edit"));
        assert!(is_edit_tool("move_file"));
        assert!(!is_edit_tool("run_terminal_command"));
        assert!(!is_edit_tool("read_file"));
    }

    #[test]
    fn changed_files_parsed_from_arguments() {
        let files = edit_tool_changed_files(
            "search_replace",
            r#"{"target_file": "src/lib.rs", "old_string": "a", "new_string": "b"}"#,
        );
        assert_eq!(files, vec![PathBuf::from("src/lib.rs")]);
        // Non-edit tools never yield files.
        assert!(edit_tool_changed_files("read_file", r#"{"target_file":"x"}"#).is_empty());
        // Garbage arguments are ignored.
        assert!(edit_tool_changed_files("edit_file", "not json").is_empty());
        // move_file carries both source and destination: both are edits.
        let moved = edit_tool_changed_files(
            "move_file",
            r#"{"source_path": "a.rs", "destination_path": "b.rs"}"#,
        );
        assert_eq!(moved.len(), 2);
    }

    #[test]
    fn should_verify_is_default_off() {
        assert!(!should_verify(false, false, true), "default: no Max, no forced verify");
        assert!(should_verify(true, false, true), "Max effort triggers");
        assert!(should_verify(false, true, true), "profile verify-first triggers");
        assert!(!should_verify(true, true, false), "disabled config wins");
    }

    #[test]
    fn repair_loop_registry_round_trips() {
        let sid = "verify-registry-test";
        drop_session(sid);
        let mut loop_state = session_repair_loop(sid);
        assert!(loop_state.may_repair());
        loop_state.record(RepairAttemptStatus::Failed, 0);
        store_session_repair_loop(sid, loop_state);
        let reloaded = session_repair_loop(sid);
        assert_eq!(reloaded.attempts, 1);
        drop_session(sid);
        assert!(session_repair_loop(sid).may_repair(), "fresh session starts open");
    }

    #[test]
    fn verify_disabled_config_is_inconclusive_and_never_signs_off() {
        // A disabled config returns Ok(None) → Inconclusive; the loop stays
        // active but consumes an attempt, and the state is never Succeeded.
        let mut loop_state = RepairLoop::new(3, u32::MAX);
        let dir = tempfile::tempdir().unwrap();
        let cfg = lumen_verify::config::Config {
            enabled: false,
            ..Default::default()
        };
        let outcome = run_verification(
            dir.path(),
            &[PathBuf::from("nope.go")],
            &cfg,
            &mut loop_state,
        );
        assert_eq!(loop_state.attempts, 1);
        assert_eq!(outcome.state, RepairLoopState::Active);
        assert!(outcome.verified_files.is_empty());
    }

    #[test]
    fn loop_exhausts_after_cap_with_failing_verifications() {
        let mut loop_state = RepairLoop::new(2, u32::MAX);
        let dir = tempfile::tempdir().unwrap();
        let cfg = lumen_verify::config::Config {
            enabled: false,
            ..Default::default()
        };
        // Two inconclusive (skipped) attempts consume the cap.
        run_verification(dir.path(), &[PathBuf::from("a.go")], &cfg, &mut loop_state);
        let outcome = run_verification(dir.path(), &[PathBuf::from("a.go")], &cfg, &mut loop_state);
        assert_eq!(loop_state.attempts, 2);
        assert!(
            matches!(outcome.state, RepairLoopState::Exhausted { .. }),
            "cap exhaustion must fail closed, got {:?}",
            outcome.state
        );
    }

    #[test]
    fn verify_events_append_and_chain_verify() {
        use xai_grok_memory::lifecycle_journal::{
            GovernedLifecycleEventKind, LifecycleJournal,
        };
        let dir = tempfile::tempdir().unwrap();
        let root = "root-session-verify-events";
        append_verify_event(
            dir.path(),
            root,
            GovernedLifecycleEventKind::VerificationStarted,
            Some(serde_json::json!({"tool": "search_replace", "attempt": 0})),
        )
        .unwrap();
        append_verify_event(
            dir.path(),
            root,
            GovernedLifecycleEventKind::VerificationSucceeded,
            None,
        )
        .unwrap();
        append_verify_event(
            dir.path(),
            root,
            GovernedLifecycleEventKind::RepairExhausted,
            Some(serde_json::json!({"reason": "attempts_exhausted"})),
        )
        .unwrap();

        let file_name = format!("{}.jsonl", &blake3::hash(root.as_bytes()).to_hex()[..16]);
        let journal = LifecycleJournal::at_path(root, dir.path().join(file_name));
        assert_eq!(journal.events().len(), 3);
        assert!(journal.verify_chain().is_ok(), "chained verify events must verify");
        assert_eq!(journal.events()[0].kind, GovernedLifecycleEventKind::VerificationStarted);
        assert_eq!(
            journal.events()[0].detail.as_ref().unwrap()["tool"],
            "search_replace"
        );
        assert_eq!(journal.events()[2].kind, GovernedLifecycleEventKind::RepairExhausted);
        assert_eq!(journal.events()[2].detail.as_ref().unwrap()["reason"], "attempts_exhausted");
        // Causal chain: each event links to its predecessor.
        assert_eq!(journal.events()[1].causal_parent, Some(0));
        assert_eq!(journal.events()[2].causal_parent, Some(1));
    }

    #[test]
    fn repair_instruction_mentions_attempt_and_caps_diagnostics() {
        let with_diag = build_repair_instruction(1, 3, "line 5: compile error");
        assert!(with_diag.contains("attempt 1 of 3"));
        assert!(with_diag.contains("line 5: compile error"));
        // No diagnostics → clean instruction without a diagnostics section.
        let bare = build_repair_instruction(2, 3, "");
        assert!(bare.contains("attempt 2 of 3"));
        assert!(!bare.contains("Verification diagnostics"));
        // Oversized diagnostics are capped with a truncation marker.
        let huge = "x".repeat(5000);
        let capped = build_repair_instruction(3, 3, &huge);
        assert!(capped.contains("[diagnostics truncated]"));
        assert!(capped.chars().count() < 3000);
    }
}
