//! Deterministic offline micro-loop. Seam contract: S2 and S4.

use crate::{
    Approval, ApprovalDecision, Artifact, CallId, Evidence, ProjectId, Provenance, Result,
    RunContext, RunId, RunRecord, RunState, ScienceError, ScienceStore,
};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{collections::BTreeMap, path::Path};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ResearchResult {
    pub run: RunRecord,
    pub conclusion: String,
    pub artifacts: Vec<Artifact>,
    pub evidence: Vec<Evidence>,
    pub provenance: Vec<Provenance>,
    pub approvals: Vec<Approval>,
    pub environment: BTreeMap<String, String>,
    pub replay_after: u64,
}

pub fn execute_approved_fixture(
    store: &ScienceStore,
    context: RunContext,
    fixture_path: &Path,
    fixture: &[u8],
) -> Result<ResearchResult> {
    let project = context.project_id.clone();
    let owner = context.owner_id.clone();
    let run_id = context.run_id.clone();
    store.create_run(context)?;
    store.append_event(
        &run_id,
        "SessionActor",
        "run.created",
        serde_json::json!({}),
    )?;
    let call = CallId::new("science_csv_analyze");
    store.request_approval(Approval {
        project_id: project.clone(),
        run_id: run_id.clone(),
        call_id: call.clone(),
        owner_id: owner.clone(),
        decision: ApprovalDecision::Pending,
        decided_at: None,
    })?;
    store.transition(&run_id, RunState::AwaitingApproval, None)?;
    store.decide_approval(&project, &run_id, &owner, &call, ApprovalDecision::Allow)?;
    store.append_event(
        &run_id,
        "LumenApproval",
        "approval.allowed",
        serde_json::json!({"call_id": call.0}),
    )?;
    store.transition(&run_id, RunState::Running, None)?;
    let input = std::str::from_utf8(fixture)
        .map_err(|_| ScienceError::Invalid("fixture must be UTF-8".into()))?;
    let groups = summarize(input)?;
    let summary = render_summary(&groups);
    let svg = render_svg(&groups);
    let csv_artifact = store.put_artifact(
        &project,
        &run_id,
        &owner,
        call.clone(),
        Path::new("summary.csv"),
        summary.as_bytes(),
        "text/csv",
        "table",
    )?;
    let svg_artifact = store.put_artifact(
        &project,
        &run_id,
        &owner,
        call,
        Path::new("means.svg"),
        svg.as_bytes(),
        "image/svg+xml",
        "image",
    )?;
    let input_hash = format!("{:x}", Sha256::digest(fixture));
    store.add_provenance(Provenance {
        run_id: run_id.clone(),
        source_uri: format!("file://{}", fixture_path.display()),
        source_commit: None,
        source_path: Some(fixture_path.display().to_string()),
        license: "CC0-1.0 fixture".into(),
        retrieved_at: Utc::now(),
        input_sha256: input_hash,
        tool: "Lumen SessionActor/science_csv_analyze@1".into(),
        environment: BTreeMap::from([
            ("algorithm".into(), "group-count-mean-v1".into()),
            ("locale".into(), "C".into()),
        ]),
    })?;
    let conclusion = groups
        .iter()
        .map(|(name, values)| format!("{name}: n={}, mean={:.3}", values.len(), mean(values)))
        .collect::<Vec<_>>()
        .join("; ");
    store.add_evidence(Evidence {
        run_id: run_id.clone(),
        claim: conclusion.clone(),
        source: fixture_path.display().to_string(),
        artifact_sha256: Some(csv_artifact.sha256.clone()),
        verified_at: Utc::now(),
    })?;
    store.append_event(
        &run_id,
        "LumenToolDispatch",
        "tool.completed",
        serde_json::json!({"artifacts": [csv_artifact.sha256, svg_artifact.sha256]}),
    )?;
    let run = store.transition(&run_id, RunState::Succeeded, None)?;
    store.append_event(
        &run_id,
        "HostVerification",
        "run.succeeded",
        serde_json::json!({}),
    )?;
    aggregate(store, run, conclusion)
}

pub fn aggregate(
    store: &ScienceStore,
    run: RunRecord,
    conclusion: String,
) -> Result<ResearchResult> {
    let run_id = &run.context.run_id;
    let events = store.events_after(run_id, 0, 1_000)?;
    Ok(ResearchResult {
        artifacts: store.artifacts(run_id)?,
        evidence: store.evidence(run_id)?,
        provenance: store.provenance(run_id)?,
        approvals: store.approvals(run_id)?,
        environment: run.context.environment.clone(),
        replay_after: events.last().map_or(0, |event| event.seq),
        run,
        conclusion,
    })
}

fn summarize(input: &str) -> Result<BTreeMap<String, Vec<f64>>> {
    let mut lines = input.lines();
    if lines.next() != Some("sample_id,condition,value") {
        return Err(ScienceError::Invalid("unexpected CSV header".into()));
    }
    let mut groups = BTreeMap::<String, Vec<f64>>::new();
    for (index, line) in lines.enumerate() {
        let fields = line.split(',').collect::<Vec<_>>();
        if fields.len() != 3 || fields[0].is_empty() || fields[1].is_empty() {
            return Err(ScienceError::Invalid(format!(
                "invalid CSV row {}",
                index + 2
            )));
        }
        let value = fields[2]
            .parse::<f64>()
            .map_err(|_| ScienceError::Invalid(format!("invalid value at row {}", index + 2)))?;
        if !value.is_finite() {
            return Err(ScienceError::Invalid("non-finite value".into()));
        }
        groups.entry(fields[1].into()).or_default().push(value);
    }
    if groups.is_empty() {
        return Err(ScienceError::Invalid("CSV has no rows".into()));
    }
    Ok(groups)
}

fn mean(values: &[f64]) -> f64 {
    values.iter().sum::<f64>() / values.len() as f64
}

fn render_summary(groups: &BTreeMap<String, Vec<f64>>) -> String {
    let body = groups
        .iter()
        .map(|(name, values)| format!("{name},{},{:.3}", values.len(), mean(values)))
        .collect::<Vec<_>>()
        .join("\n");
    format!("condition,count,mean\n{body}\n")
}

fn render_svg(groups: &BTreeMap<String, Vec<f64>>) -> String {
    let bars = groups
        .iter()
        .enumerate()
        .map(|(index, (name, values))| {
            let x = 30 + index * 90;
            let height = (mean(values) * 10.0).round().clamp(0.0, 160.0) as usize;
            let y = 180 - height;
            format!("<rect x=\"{x}\" y=\"{y}\" width=\"50\" height=\"{height}\"/><text x=\"{x}\" y=\"198\">{name}</text>")
        })
        .collect::<Vec<_>>()
        .join("");
    format!(
        "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"400\" height=\"210\" viewBox=\"0 0 400 210\"><title>Condition means</title>{bars}</svg>\n"
    )
}

pub fn fixture_context(root: &Path, project: ProjectId, owner: impl Into<String>) -> RunContext {
    RunContext {
        run_id: RunId::new_v7(),
        project_id: project,
        owner_id: owner.into(),
        workspace_root: root.join("workspace"),
        provider: "offline-deterministic".into(),
        approval_policy: "ask".into(),
        tool_profile: "science-csv-v1".into(),
        artifact_root: root.join("artifacts"),
        environment: BTreeMap::from([
            ("network".into(), "disabled".into()),
            ("locale".into(), "C".into()),
        ]),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const FIXTURE: &[u8] =
        b"sample_id,condition,value\ns1,control,1\ns2,control,3\ns3,treated,4\ns4,treated,8\n";

    #[test]
    fn fixture_is_deterministic_across_reopen() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let result = execute_approved_fixture(
            &store,
            fixture_context(temp.path(), ProjectId::new("p"), "alice"),
            Path::new("fixtures/micro.csv"),
            FIXTURE,
        )
        .unwrap();
        assert_eq!(
            result.conclusion,
            "control: n=2, mean=2.000; treated: n=2, mean=6.000"
        );
        let before = serde_json::to_value(&result).unwrap();
        drop(store);
        let reopened = ScienceStore::new(temp.path());
        let run = reopened.load_run(&result.run.context.run_id).unwrap();
        let replay = aggregate(&reopened, run, result.conclusion.clone()).unwrap();
        assert_eq!(before, serde_json::to_value(replay).unwrap());
    }
}
