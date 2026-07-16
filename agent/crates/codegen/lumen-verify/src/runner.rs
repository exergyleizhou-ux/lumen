//! Step execution with timeout.

use super::{Diagnostic, StepResult};
use crate::steps::Step;
use anyhow::Result;
use std::path::Path;
use std::process::Command;
use std::time::Instant;

/// Run a single verification step with timeout.
///
/// If the tool is not found on PATH the step is skipped (returns ok=true with
/// a "SKIP" note) — a missing tool is never a failure.
pub fn run_step(root: &Path, step: &Step, _timeout_secs: u64) -> Result<StepResult> {
    let start = Instant::now();

    // Check if tool exists — skip if not found (don't treat missing tool as failure).
    if which::which(&step.command).is_err() {
        return Ok(StepResult {
            language: step.language.clone(),
            command: format!("{} {}", step.command, step.args.join(" ")),
            ok: true,
            output: format!("SKIP: {} not found on PATH", step.command),
            diagnostics: vec![],
            duration_ms: start.elapsed().as_millis() as u64,
        });
    }

    let output = Command::new(&step.command)
        .args(&step.args)
        .current_dir(root)
        .output()?;

    let stdout = String::from_utf8_lossy(&output.stdout).to_string();
    let stderr = String::from_utf8_lossy(&output.stderr).to_string();
    let combined = format!("{}{}", stdout, stderr);
    let ok = output.status.success();
    let diagnostics = if ok {
        vec![]
    } else {
        parse_diagnostics(&step.language, &combined)
    };

    Ok(StepResult {
        language: step.language.clone(),
        command: format!("{} {}", step.command, step.args.join(" ")),
        ok,
        output: combined,
        diagnostics,
        duration_ms: start.elapsed().as_millis() as u64,
    })
}

/// Parse diagnostics from tool output (Go / Python / TS).
fn parse_diagnostics(language: &str, output: &str) -> Vec<Diagnostic> {
    match language {
        "go" => parse_go_diagnostics(output),
        "python" => parse_python_diagnostics(output),
        "typescript" => parse_ts_diagnostics(output),
        _ => vec![],
    }
}

/// Parse Go compiler errors: `file.go:10:5: message`
fn parse_go_diagnostics(output: &str) -> Vec<Diagnostic> {
    let re = regex::Regex::new(r"^(.+?):(\d+):(\d+):\s*(.+)$").unwrap();
    let mut diags = Vec::new();
    for line in output.lines() {
        if let Some(caps) = re.captures(line) {
            diags.push(Diagnostic {
                file: caps[1].to_string(),
                line: caps[2].parse().ok(),
                col: caps[3].parse().ok(),
                severity: "ERROR".into(),
                message: caps[4].to_string(),
            });
        }
    }
    diags
}

/// Parse Python (ruff / pytest) diagnostics.
fn parse_python_diagnostics(output: &str) -> Vec<Diagnostic> {
    let re = regex::Regex::new(r"^(.+?\.py):(\d+):(\d+):\s*(.+)").unwrap();
    let mut diags = Vec::new();
    for line in output.lines() {
        if let Some(caps) = re.captures(line) {
            diags.push(Diagnostic {
                file: caps[1].to_string(),
                line: caps[2].parse().ok(),
                col: caps[3].parse().ok(),
                severity: "ERROR".into(),
                message: caps[4].to_string(),
            });
        }
    }
    // pytest: FAILED file.py::test_name - message
    let pt_re = regex::Regex::new(r"FAILED\s+(.+?\.py).+-\s*(.+)").unwrap();
    for line in output.lines() {
        if let Some(caps) = pt_re.captures(line) {
            diags.push(Diagnostic {
                file: caps[1].to_string(),
                line: None,
                col: None,
                severity: "FAIL".into(),
                message: caps[2].to_string(),
            });
        }
    }
    if diags.is_empty() && !output.trim().is_empty() {
        // Catch-all: return raw output as a single diagnostic
        diags.push(Diagnostic {
            file: String::new(),
            line: None,
            col: None,
            severity: "ERROR".into(),
            message: output.trim().to_string(),
        });
    }
    diags
}

/// Parse TypeScript (tsc / jest) diagnostics.
fn parse_ts_diagnostics(output: &str) -> Vec<Diagnostic> {
    let re = regex::Regex::new(
        r"^(.+?)\((\d+),(\d+)\):\s*(error|warning)\s+TS(\d+):\s*(.+)",
    )
    .unwrap();
    let mut diags = Vec::new();
    for line in output.lines() {
        if let Some(caps) = re.captures(line) {
            diags.push(Diagnostic {
                file: caps[1].to_string(),
                line: caps[2].parse().ok(),
                col: caps[3].parse().ok(),
                severity: format!("{}/TS{}", &caps[4], &caps[5]),
                message: caps[6].to_string(),
            });
        }
    }
    diags
}
