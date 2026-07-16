//! Automatic verification feedback for successful writer tool results.
//!
//! This layer intentionally changes only the model-facing `prompt_text`; the
//! structured tool output remains the exact edit result used by ACP, hunk
//! tracking, and persistence.

use crate::types::output::{SearchReplaceOutput, ToolOutput};
use std::path::PathBuf;

const MAX_FEEDBACK_CHARS: usize = 16_000;

pub(crate) async fn feedback_for_output(
    workspace_root: PathBuf,
    output: &ToolOutput,
) -> Option<String> {
    let changed_file = match output {
        ToolOutput::SearchReplace(SearchReplaceOutput::EditsApplied(applied)) => {
            applied.absolute_path.clone()
        }
        _ => return None,
    };

    let outcome = tokio::task::spawn_blocking(move || {
        let cfg = lumen_verify::config::Config::default();
        lumen_verify::run_after_edit(&workspace_root, &changed_file, &cfg)
    })
    .await;

    let feedback = match outcome {
        Ok(Ok(None)) => return None,
        Ok(Ok(Some(result))) if result.step_results.iter().all(|step| step.skipped) => {
            "[verify-after-edit] SKIPPED: no allowed Go verifier executable was available on PATH. The edit remains unverified.".to_string()
        }
        Ok(Ok(Some(result))) if result.ok => {
            let ran = result
                .step_results
                .iter()
                .filter(|step| !step.skipped)
                .count();
            let skipped = result.step_results.len().saturating_sub(ran);
            if skipped == 0 {
                format!(
                    "[verify-after-edit] PASS: {ran} fixed Go build/vet/test step(s) passed after this edit."
                )
            } else {
                format!(
                    "[verify-after-edit] PASS: {ran} fixed Go verification step(s) passed; {skipped} unavailable step(s) were skipped."
                )
            }
        }
        Ok(Ok(Some(result))) => {
            let diagnostics = lumen_verify::format_diagnostics(&result.step_results);
            format!(
                "[verify-after-edit] FAILED: automatic Go build/vet/test found errors after this edit.\n\
                 Fix these diagnostics and edit again; the next successful write will verify again.\n\
                 {diagnostics}"
            )
        }
        Ok(Err(error)) => format!(
            "[verify-after-edit] ERROR: automatic verification could not run safely: {error:#}. \
             Treat this edit as unverified and report the blocker if it cannot be corrected."
        ),
        Err(error) => format!(
            "[verify-after-edit] ERROR: verifier worker failed: {error}. \
             Treat this edit as unverified and report the blocker."
        ),
    };

    Some(truncate_feedback(feedback))
}

fn truncate_feedback(mut feedback: String) -> String {
    if feedback.chars().count() <= MAX_FEEDBACK_CHARS {
        return feedback;
    }
    feedback = feedback.chars().take(MAX_FEEDBACK_CHARS).collect();
    feedback.push_str("\n[verify-after-edit diagnostics truncated]");
    feedback
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn feedback_truncation_preserves_utf8_boundary() {
        let input = "错".repeat(MAX_FEEDBACK_CHARS + 10);
        let output = truncate_feedback(input);
        assert!(output.ends_with("[verify-after-edit diagnostics truncated]"));
        assert!(output.is_char_boundary(output.len()));
    }
}
