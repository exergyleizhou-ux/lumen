//! Lumen verify-after-edit crate.
//!
//! Detects the language/project type from changed files, generates verification
//! steps (build / vet / test), executes them with a timeout, parses diagnostics,
//! and tracks a repair cycle state machine (max 3 cycles by default).
//!
//! # CLI
//!
//! ```text
//! lumen-verify --root <dir> --changed a.go,b.go
//! ```
//!
//! Exit code 0 = all steps passed.  Non-zero = failures found (diagnostics on
//! stderr / JSON on stdout).

pub mod config;
pub mod detect;
pub mod repair;
pub mod runner;
pub mod steps;

use anyhow::Result;
use std::path::{Path, PathBuf};

/// Full result of a verification run.
#[derive(Debug, serde::Serialize)]
pub struct VerifyResult {
    /// Whether all steps passed.
    pub ok: bool,
    /// Individual step results.
    pub step_results: Vec<StepResult>,
    /// Current repair cycle index (0-based).
    pub repair_cycle: u32,
}

/// Result of a single verification step.
#[derive(Debug, serde::Serialize)]
pub struct StepResult {
    pub language: String,
    pub command: String,
    pub ok: bool,
    pub output: String,
    /// Parsed diagnostics (if any).
    pub diagnostics: Vec<Diagnostic>,
    /// Duration in milliseconds.
    pub duration_ms: u64,
}

/// A parsed diagnostic (compiler / linter message).
#[derive(Debug, Clone, serde::Serialize)]
pub struct Diagnostic {
    pub file: String,
    pub line: Option<u32>,
    pub col: Option<u32>,
    pub severity: String,
    pub message: String,
}

/// Run verification for a set of changed files under a project root.
///
/// Returns `VerifyResult`.  If `max_repair` cycles have been exhausted the
/// `repair_cycle` field will equal `max_repair`.
pub fn run(root: &Path, changed_files: &[PathBuf], cfg: &config::Config) -> Result<VerifyResult> {
    let languages = detect::detect_languages(changed_files);
    if languages.is_empty() {
        return Ok(VerifyResult {
            ok: true,
            step_results: vec![],
            repair_cycle: 0,
        });
    }

    let all_steps = steps::generate_steps(root, &languages, cfg);
    let mut step_results = Vec::new();
    let mut all_ok = true;

    for step in &all_steps {
        let step_result = runner::run_step(root, step, cfg.timeout_secs)?;
        if !step_result.ok {
            all_ok = false;
        }
        step_results.push(step_result);
    }

    Ok(VerifyResult {
        ok: all_ok,
        step_results,
        repair_cycle: 0, // caller increments
    })
}

/// Format diagnostics into a human-readable string suitable for model feedback.
pub fn format_diagnostics(results: &[StepResult]) -> String {
    let mut out = String::new();
    for sr in results {
        if sr.diagnostics.is_empty() && sr.ok {
            continue;
        }
        out.push_str(&format!("\n## {} — {}\n", sr.language, sr.command));
        if sr.ok {
            out.push_str("  ✓ passed\n");
        } else {
            out.push_str(&format!("  ✗ FAILED ({}ms)\n", sr.duration_ms));
            for d in &sr.diagnostics {
                let loc = match (d.line, d.col) {
                    (Some(l), Some(c)) => format!("{}:{}:{}", d.file, l, c),
                    (Some(l), None) => format!("{}:{}", d.file, l),
                    _ => d.file.clone(),
                };
                out.push_str(&format!("    {}: {}: {}\n", d.severity, loc, d.message));
            }
        }
    }
    if out.is_empty() {
        out.push_str("All verification steps passed.\n");
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_detect_go_from_extension() {
        let files = vec![PathBuf::from("main.go")];
        let langs = detect::detect_languages(&files);
        assert!(langs.contains(&"go".to_string()));
    }

    #[test]
    fn test_detect_py_from_extension() {
        let files = vec![PathBuf::from("app.py")];
        let langs = detect::detect_languages(&files);
        assert!(langs.contains(&"python".to_string()));
    }

    #[test]
    fn test_detect_ts_from_extension() {
        let files = vec![PathBuf::from("index.ts")];
        let langs = detect::detect_languages(&files);
        assert!(langs.contains(&"typescript".to_string()));
    }

    #[test]
    fn test_detect_empty_no_langs() {
        let empty: &[PathBuf] = &[];
        let langs = detect::detect_languages(empty);
        assert!(langs.is_empty());
    }

    #[test]
    fn test_format_diagnostics_empty() {
        let results = vec![StepResult {
            language: "go".into(),
            command: "go build ./...".into(),
            ok: true,
            output: String::new(),
            diagnostics: vec![],
            duration_ms: 123,
        }];
        let s = format_diagnostics(&results);
        assert!(s.contains("passed"));
    }

    #[test]
    fn test_format_diagnostics_with_errors() {
        let results = vec![StepResult {
            language: "go".into(),
            command: "go build ./...".into(),
            ok: false,
            output: String::new(),
            diagnostics: vec![Diagnostic {
                file: "main.go".into(),
                line: Some(10),
                col: Some(5),
                severity: "ERROR".into(),
                message: "undefined: foo".into(),
            }],
            duration_ms: 234,
        }];
        let s = format_diagnostics(&results);
        assert!(s.contains("FAILED"));
        assert!(s.contains("main.go:10:5"));
        assert!(s.contains("undefined: foo"));
    }
}
