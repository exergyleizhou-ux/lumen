//! Lumen Science product dispatch. Seam contract: S2 and S4.

use super::*;
use crate::session::commands::{PreparedScienceCsv, PreparedScienceSshScpAdmission};

const CSV_TOOL_SCRIPT: &str = r#"import csv, html, sys
from collections import defaultdict
from pathlib import Path

source, summary_path, svg_path = map(Path, sys.argv[1:4])
groups = defaultdict(list)
with source.open(newline='', encoding='utf-8') as handle:
    reader = csv.DictReader(handle)
    if reader.fieldnames != ['sample_id', 'condition', 'value']:
        raise SystemExit('unexpected CSV header')
    for row in reader:
        groups[row['condition']].append(float(row['value']))
if not groups:
    raise SystemExit('CSV has no rows')
rows = []
bars = []
for index, name in enumerate(sorted(groups)):
    values = groups[name]
    mean = sum(values) / len(values)
    rows.append(f'{name},{len(values)},{mean:.3f}')
    x = 30 + index * 90
    height = max(0, min(160, round(mean * 10)))
    y = 180 - height
    escaped = html.escape(name, quote=True)
    bars.append(f'<rect x="{x}" y="{y}" width="50" height="{height}"/><text x="{x}" y="198">{escaped}</text>')
summary_path.write_text('condition,count,mean\n' + '\n'.join(rows) + '\n', encoding='utf-8')
svg_path.write_text('<svg xmlns="http://www.w3.org/2000/svg" width="400" height="210" viewBox="0 0 400 210"><title>Condition means</title>' + ''.join(bars) + '</svg>\n', encoding='utf-8')
"#;

fn quote(value: &str) -> xai_grok_science::Result<String> {
    shlex::try_quote(value)
        .map(|quoted| quoted.into_owned())
        .map_err(|_| xai_grok_science::ScienceError::Invalid("NUL in science tool path".into()))
}

impl SessionActor {
    /// P4 admission runs inside the sole Lumen session actor. It is called
    /// before the handle asks the existing permission manager, and the Science
    /// crate itself performs no I/O outside its durable local store.
    pub(super) fn prepare_science_ssh_scp_admission(
        &self,
        store: xai_grok_science::ScienceStore,
        context: xai_grok_science::RunContext,
        policy: xai_grok_science::connector::ConnectorPolicy,
        request: xai_grok_science::connector::ConnectorRequest,
    ) -> xai_grok_science::Result<Option<PreparedScienceSshScpAdmission>> {
        match xai_grok_science::connector::start_ssh_scp_admission(
            &store, context, &policy, &request,
        )? {
            xai_grok_science::connector::AdmissionStart::Ready(ticket) => {
                Ok(Some(PreparedScienceSshScpAdmission {
                    store,
                    ticket: *ticket,
                }))
            }
            xai_grok_science::connector::AdmissionStart::Denied => Ok(None),
        }
    }

    pub(super) fn finish_science_ssh_scp_admission(
        &self,
        prepared: PreparedScienceSshScpAdmission,
        decision: xai_grok_science::ApprovalDecision,
    ) -> xai_grok_science::Result<Option<xai_grok_science::connector::AdmissionTicket>> {
        xai_grok_science::connector::finish_ssh_scp_admission(
            &prepared.store,
            prepared.ticket,
            decision,
        )
    }

    pub(super) fn prepare_science_csv(
        &self,
        store: xai_grok_science::ScienceStore,
        context: xai_grok_science::RunContext,
        fixture_path: std::path::PathBuf,
        fixture: Vec<u8>,
    ) -> xai_grok_science::Result<PreparedScienceCsv> {
        let ticket = xai_grok_science::csv::begin_fixture(&store, context.clone())?;
        let staging = context
            .artifact_root
            .join(&ticket.run_id.0)
            .join("tool-staging");
        std::fs::create_dir_all(&staging)?;
        let input_path = staging.join("input.csv");
        let summary_path = staging.join("summary.csv");
        let svg_path = staging.join("means.svg");
        std::fs::write(&input_path, &fixture)?;
        let command = format!(
            "python3 -c {} {} {} {}",
            quote(CSV_TOOL_SCRIPT)?,
            quote(&input_path.to_string_lossy())?,
            quote(&summary_path.to_string_lossy())?,
            quote(&svg_path.to_string_lossy())?,
        );
        Ok(PreparedScienceCsv {
            store,
            ticket,
            fixture_path,
            fixture,
            command,
            summary_path,
            svg_path,
        })
    }

    pub(super) async fn finish_science_csv(
        &self,
        prepared: PreparedScienceCsv,
        decision: xai_grok_science::ApprovalDecision,
        reason: String,
    ) -> xai_grok_science::Result<xai_grok_science::csv::ResearchResult> {
        if decision != xai_grok_science::ApprovalDecision::Allow {
            let terminal = xai_grok_science::csv::finish_without_execution(
                &prepared.store,
                &prepared.ticket,
                decision,
                reason,
            )?;
            return Err(xai_grok_science::ScienceError::Invalid(format!(
                "science run {} finished {:?}",
                prepared.ticket.run_id.0, terminal.state
            )));
        }

        xai_grok_science::csv::mark_allowed(&prepared.store, &prepared.ticket)?;
        let tool_name = self
            .agent
            .borrow()
            .tool_bridge()
            .toolset()
            .tool_name_for_kind(xai_grok_tools::types::tool::ToolKind::Execute)
            .ok_or_else(|| {
                xai_grok_science::ScienceError::Invalid(
                    "session toolset has no execute tool".into(),
                )
            })?;
        prepared.store.append_event(
            &prepared.ticket.run_id,
            "LumenToolDispatch",
            "tool.started",
            serde_json::json!({
                "tool": tool_name,
                "call_id": prepared.ticket.call_id.0,
                "dispatch": "WorkspaceOps::call_tool"
            }),
        )?;
        let args = serde_json::to_value(BashToolInput {
            command: prepared.command.clone(),
            timeout: Some(30_000),
            description: "Compute deterministic Lumen Science CSV summary and SVG".into(),
            is_background: false,
        })
        .map_err(xai_grok_science::ScienceError::Serde)?;
        let dispatched = self
            .workspace_ops
            .call_tool(
                &tool_name,
                args,
                &prepared.ticket.call_id.0,
                Some(&self.session_info.id.0),
            )
            .await;
        let output = match dispatched {
            Ok(output) => output,
            Err(error) => {
                let reason = format!("formal tool dispatch failed: {error}");
                let _ = xai_grok_science::csv::fail_running(
                    &prepared.store,
                    &prepared.ticket,
                    reason.clone(),
                );
                return Err(xai_grok_science::ScienceError::Invalid(reason));
            }
        };
        match output.output {
            ToolsToolOutput::Bash(ref bash) if bash.exit_code == 0 && !bash.timed_out => {}
            ToolsToolOutput::Bash(ref bash) => {
                let reason = format!(
                    "science compute tool failed: exit={} timed_out={}",
                    bash.exit_code, bash.timed_out
                );
                let _ = xai_grok_science::csv::fail_running(
                    &prepared.store,
                    &prepared.ticket,
                    reason.clone(),
                );
                return Err(xai_grok_science::ScienceError::Invalid(reason));
            }
            _ => {
                let reason = "science execute tool returned a non-bash output".to_string();
                let _ = xai_grok_science::csv::fail_running(
                    &prepared.store,
                    &prepared.ticket,
                    reason.clone(),
                );
                return Err(xai_grok_science::ScienceError::Invalid(reason));
            }
        }
        let summary = std::fs::read(&prepared.summary_path)?;
        let svg = std::fs::read(&prepared.svg_path)?;
        xai_grok_science::csv::finish_from_tool_output(
            &prepared.store,
            prepared.ticket,
            &prepared.fixture_path,
            &prepared.fixture,
            &summary,
            &svg,
            format!("{tool_name} via WorkspaceOps::call_tool"),
        )
    }
}
