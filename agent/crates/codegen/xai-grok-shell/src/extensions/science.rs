//! Non-test ACP product entry for Lumen Science. Seam contract: S1, S2, S4.

use super::{ExtResult, parse_params, to_raw_response};
use crate::agent::MvpAgent;
use agent_client_protocol as acp;
use serde::Deserialize;
use std::{collections::BTreeMap, path::PathBuf, time::Duration};
use xai_grok_science::{ProjectId, RunContext, RunId, ScienceStore};

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct RunCsvParams {
    session_id: String,
    project_id: String,
    owner_id: String,
    store_root: PathBuf,
    artifact_root: PathBuf,
    fixture_path: PathBuf,
    #[serde(default = "default_approval_timeout_ms")]
    approval_timeout_ms: u64,
}

fn default_approval_timeout_ms() -> u64 {
    120_000
}

fn internal(error: impl std::fmt::Display) -> acp::Error {
    acp::Error::internal_error().data(error.to_string())
}

fn canonical_dir_within(path: PathBuf, workspace: &std::path::Path) -> Result<PathBuf, acp::Error> {
    std::fs::create_dir_all(&path).map_err(internal)?;
    let canonical = std::fs::canonicalize(path).map_err(internal)?;
    if !canonical.starts_with(workspace) {
        return Err(acp::Error::invalid_params().data("science path must be inside session cwd"));
    }
    Ok(canonical)
}

pub async fn handle(agent: &MvpAgent, args: &acp::ExtRequest) -> ExtResult {
    if args.method.as_ref() != "x.ai/science/run_csv" {
        return Err(acp::Error::method_not_found());
    }
    let params: RunCsvParams = parse_params(args)?;
    if params.project_id.is_empty() || params.owner_id.is_empty() {
        return Err(acp::Error::invalid_params().data("projectId and ownerId are required"));
    }
    if !(1..=300_000).contains(&params.approval_timeout_ms) {
        return Err(acp::Error::invalid_params().data("approvalTimeoutMs must be in 1..=300000"));
    }
    let session_id = acp::SessionId::new(params.session_id);
    let handle = agent
        .get_session_handle(&session_id)
        .ok_or_else(|| acp::Error::invalid_params().data("session not found"))?;
    let workspace = std::fs::canonicalize(&handle.info.cwd).map_err(internal)?;
    let fixture_path = std::fs::canonicalize(params.fixture_path).map_err(internal)?;
    if !fixture_path.starts_with(&workspace) || !fixture_path.is_file() {
        return Err(
            acp::Error::invalid_params().data("fixturePath must be a file inside session cwd")
        );
    }
    let store_root = canonical_dir_within(params.store_root, &workspace)?;
    let artifact_root = canonical_dir_within(params.artifact_root, &workspace)?;
    let fixture = std::fs::read(&fixture_path).map_err(internal)?;
    let context = RunContext {
        run_id: RunId::new_v7(),
        project_id: ProjectId::new(params.project_id),
        session_id: session_id.0.to_string(),
        owner_id: params.owner_id,
        workspace_root: workspace,
        provider: "offline-deterministic".into(),
        approval_policy: "production-session-permission".into(),
        tool_profile: "science-csv-v1".into(),
        artifact_root,
        environment: BTreeMap::from([
            ("network".into(), "disabled".into()),
            ("locale".into(), "C".into()),
        ]),
    };
    let result = agent
        .run_science_csv(
            &session_id,
            ScienceStore::new(store_root),
            context,
            fixture_path,
            fixture,
            Duration::from_millis(params.approval_timeout_ms),
        )
        .await
        .map_err(internal)?;
    to_raw_response(&result)
}
