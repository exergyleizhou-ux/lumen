//! Verification step generation.

use super::config::Config;
use std::path::Path;

/// A single verification step to execute.
#[derive(Debug, Clone)]
pub struct Step {
    pub language: String,
    pub label: String,
    pub command: String,
    pub args: Vec<String>,
}

/// Generate verification steps for a set of detected languages.
///
/// Steps are ordered: build → vet → test.  Tools that aren't installed are
/// silently skipped (each step is optional unless marked required).
pub fn generate_steps(_root: &Path, languages: &[String], _cfg: &Config) -> Vec<Step> {
    let mut steps = Vec::new();

    for lang in languages {
        match lang.as_str() {
            "go" => {
                steps.push(Step {
                    language: "go".into(),
                    label: "go build".into(),
                    command: "go".into(),
                    args: vec!["build".into(), "./...".into()],
                });
                steps.push(Step {
                    language: "go".into(),
                    label: "go vet".into(),
                    command: "go".into(),
                    args: vec!["vet".into(), "./...".into()],
                });
                steps.push(Step {
                    language: "go".into(),
                    label: "go test".into(),
                    command: "go".into(),
                    args: vec!["test".into(), "./...".into()],
                });
            }
            "python" => {
                // Only run if tools exist (runner checks availability).
                steps.push(Step {
                    language: "python".into(),
                    label: "ruff check".into(),
                    command: "ruff".into(),
                    args: vec!["check".into(), ".".into()],
                });
                steps.push(Step {
                    language: "python".into(),
                    label: "pytest".into(),
                    command: "pytest".into(),
                    args: vec!["-q".into()],
                });
            }
            "typescript" => {
                steps.push(Step {
                    language: "typescript".into(),
                    label: "tsc --noEmit".into(),
                    command: "npx".into(),
                    args: vec!["tsc".into(), "--noEmit".into()],
                });
                steps.push(Step {
                    language: "typescript".into(),
                    label: "jest".into(),
                    command: "npx".into(),
                    args: vec!["jest".into(), "--passWithNoTests".into()],
                });
            }
            _ => {}
        }
    }

    steps
}
