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
    pub verified_files: Vec<PathBuf>,
    /// Formatted diagnostics (empty when all checks passed or skipped).
    pub diagnostics: String,
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
}
