//! Loopback API contract. Seam contract: S1 and S2.

use crate::{ProjectId, RunId, ScienceError, ScienceStore, csv};
use axum::{
    Router,
    body::Body,
    extract::{Path as AxumPath, Query, State},
    http::{HeaderMap, StatusCode, Uri},
    response::{IntoResponse, Response},
    routing::get,
};
use serde::Deserialize;
use std::{
    fs,
    path::{Path, PathBuf},
    sync::Arc,
};
use uuid::Uuid;

#[derive(Debug)]
pub struct TokenFile {
    path: PathBuf,
    token: String,
}

impl TokenFile {
    pub fn create(path: impl Into<PathBuf>) -> crate::Result<Self> {
        let path = path.into();
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)?;
        }
        let token = format!("{}{}", Uuid::new_v4().simple(), Uuid::new_v4().simple());
        #[cfg(unix)]
        {
            use std::io::Write;
            use std::os::unix::fs::OpenOptionsExt;
            let mut file = fs::OpenOptions::new()
                .write(true)
                .create_new(true)
                .mode(0o600)
                .open(&path)?;
            file.write_all(token.as_bytes())?;
            file.sync_all()?;
        }
        #[cfg(not(unix))]
        fs::write(&path, token.as_bytes())?;
        Ok(Self { path, token })
    }
    pub fn token(&self) -> &str {
        &self.token
    }
}

impl Drop for TokenFile {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.path);
    }
}

#[derive(Debug, Clone)]
pub struct ApiState {
    pub store: ScienceStore,
    pub token: String,
    pub owner_id: String,
}

pub fn router(state: ApiState) -> Router {
    Router::new()
        .route("/projects/{project}/runs/{run}", get(run))
        .route("/projects/{project}/runs/{run}/events", get(events))
        .route("/projects/{project}/runs/{run}/replay", get(events))
        .route("/projects/{project}/runs/{run}/result", get(result))
        .route(
            "/projects/{project}/runs/{run}/artifacts/{*artifact}",
            get(artifact),
        )
        .with_state(Arc::new(state))
}

#[derive(Deserialize)]
struct Replay {
    after: Option<u64>,
    limit: Option<usize>,
    token: Option<String>,
}

fn authorize(
    state: &ApiState,
    headers: &HeaderMap,
    uri: &Uri,
    query_token: Option<&str>,
) -> std::result::Result<(), Response> {
    if query_token.is_some()
        || uri
            .query()
            .is_some_and(|query| query.split('&').any(|pair| pair.starts_with("token=")))
    {
        return Err((StatusCode::BAD_REQUEST, "query token forbidden").into_response());
    }
    let host = headers
        .get("host")
        .and_then(|value| value.to_str().ok())
        .unwrap_or("");
    let host_name = host.split(':').next().unwrap_or("");
    if !matches!(host_name, "127.0.0.1" | "localhost" | "[::1]") {
        return Err((StatusCode::FORBIDDEN, "host forbidden").into_response());
    }
    if let Some(origin) = headers.get("origin").and_then(|value| value.to_str().ok())
        && !(origin.starts_with("http://127.0.0.1:") || origin.starts_with("http://localhost:"))
    {
        return Err((StatusCode::FORBIDDEN, "origin forbidden").into_response());
    }
    let expected = format!("Bearer {}", state.token);
    if headers
        .get("authorization")
        .and_then(|value| value.to_str().ok())
        != Some(expected.as_str())
    {
        return Err((StatusCode::UNAUTHORIZED, "unauthorized").into_response());
    }
    Ok(())
}

fn owned(
    state: &ApiState,
    project: &str,
    run: &str,
) -> std::result::Result<crate::RunRecord, Response> {
    let record = state.store.load_run(&RunId::new(run)).map_err(map_error)?;
    if record.context.project_id != ProjectId::new(project)
        || record.context.owner_id != state.owner_id
    {
        return Err((StatusCode::FORBIDDEN, "run forbidden").into_response());
    }
    Ok(record)
}

async fn run(
    State(state): State<Arc<ApiState>>,
    AxumPath((project, run_id)): AxumPath<(String, String)>,
    headers: HeaderMap,
    uri: Uri,
) -> Response {
    if let Err(response) = authorize(&state, &headers, &uri, None) {
        return response;
    }
    match owned(&state, &project, &run_id) {
        Ok(run) => axum::Json(run).into_response(),
        Err(response) => response,
    }
}

async fn events(
    State(state): State<Arc<ApiState>>,
    AxumPath((project, run_id)): AxumPath<(String, String)>,
    Query(query): Query<Replay>,
    headers: HeaderMap,
    uri: Uri,
) -> Response {
    if let Err(response) = authorize(&state, &headers, &uri, query.token.as_deref()) {
        return response;
    }
    if let Err(response) = owned(&state, &project, &run_id) {
        return response;
    }
    match state.store.events_after(
        &RunId::new(run_id),
        query.after.unwrap_or(0),
        query.limit.unwrap_or(100),
    ) {
        Ok(events) => axum::Json(events).into_response(),
        Err(error) => map_error(error),
    }
}

async fn result(
    State(state): State<Arc<ApiState>>,
    AxumPath((project, run_id)): AxumPath<(String, String)>,
    headers: HeaderMap,
    uri: Uri,
) -> Response {
    if let Err(response) = authorize(&state, &headers, &uri, None) {
        return response;
    }
    let run = match owned(&state, &project, &run_id) {
        Ok(run) => run,
        Err(response) => return response,
    };
    let evidence = match state.store.evidence(&run.context.run_id) {
        Ok(items) => items,
        Err(error) => return map_error(error),
    };
    let conclusion = evidence
        .first()
        .map_or_else(String::new, |item| item.claim.clone());
    match csv::aggregate(&state.store, run, conclusion) {
        Ok(result) => axum::Json(result).into_response(),
        Err(error) => map_error(error),
    }
}

async fn artifact(
    State(state): State<Arc<ApiState>>,
    AxumPath((project, run_id, artifact)): AxumPath<(String, String, String)>,
    headers: HeaderMap,
    uri: Uri,
) -> Response {
    if let Err(response) = authorize(&state, &headers, &uri, None) {
        return response;
    }
    if let Err(response) = owned(&state, &project, &run_id) {
        return response;
    }
    let path = Path::new(&artifact);
    match state.store.artifact_bytes(
        &ProjectId::new(project),
        &RunId::new(run_id),
        &state.owner_id,
        path,
    ) {
        Ok(bytes) => Response::new(Body::from(bytes)),
        Err(error) => map_error(error),
    }
}

fn map_error(error: ScienceError) -> Response {
    let status = match error {
        ScienceError::Ownership => StatusCode::FORBIDDEN,
        ScienceError::Io(ref io) if io.kind() == std::io::ErrorKind::NotFound => {
            StatusCode::NOT_FOUND
        }
        ScienceError::Invalid(_) => StatusCode::BAD_REQUEST,
        _ => StatusCode::INTERNAL_SERVER_ERROR,
    };
    (status, error.to_string()).into_response()
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::Request;
    use tower::ServiceExt;

    const CSV: &[u8] = b"sample_id,condition,value\na,A,2\nb,A,4\n";

    fn request(uri: &str, token: Option<&str>, host: &str, origin: Option<&str>) -> Request<Body> {
        let mut builder = Request::builder().uri(uri).header("host", host);
        if let Some(token) = token {
            builder = builder.header("authorization", format!("Bearer {token}"));
        }
        if let Some(origin) = origin {
            builder = builder.header("origin", origin);
        }
        builder.body(Body::empty()).unwrap()
    }

    #[tokio::test]
    async fn all_read_surfaces_require_auth_and_owner() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let result = csv::execute_approved_fixture(
            &store,
            csv::fixture_context(temp.path(), ProjectId::new("p"), "alice"),
            Path::new("fixture.csv"),
            CSV,
        )
        .unwrap();
        let run = &result.run.context.run_id.0;
        let app = router(ApiState {
            store,
            token: "secret".into(),
            owner_id: "alice".into(),
        });
        for suffix in [
            "",
            "/events",
            "/replay",
            "/result",
            "/artifacts/summary.csv",
        ] {
            let uri = format!("/projects/p/runs/{run}{suffix}");
            assert_eq!(
                app.clone()
                    .oneshot(request(&uri, None, "127.0.0.1:8000", None))
                    .await
                    .unwrap()
                    .status(),
                StatusCode::UNAUTHORIZED
            );
            assert_eq!(
                app.clone()
                    .oneshot(request(&uri, Some("secret"), "127.0.0.1:8000", None))
                    .await
                    .unwrap()
                    .status(),
                StatusCode::OK
            );
        }
        assert_eq!(
            app.clone()
                .oneshot(request(
                    &format!("/projects/q/runs/{run}"),
                    Some("secret"),
                    "127.0.0.1:8000",
                    None
                ))
                .await
                .unwrap()
                .status(),
            StatusCode::FORBIDDEN
        );
    }

    #[tokio::test]
    async fn query_token_host_origin_and_traversal_are_rejected() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let result = csv::execute_approved_fixture(
            &store,
            csv::fixture_context(temp.path(), ProjectId::new("p"), "alice"),
            Path::new("fixture.csv"),
            CSV,
        )
        .unwrap();
        let base = format!("/projects/p/runs/{}", result.run.context.run_id.0);
        let app = router(ApiState {
            store,
            token: "secret".into(),
            owner_id: "alice".into(),
        });
        assert_eq!(
            app.clone()
                .oneshot(request(
                    &format!("{base}/events?token=secret"),
                    Some("secret"),
                    "127.0.0.1:1",
                    None
                ))
                .await
                .unwrap()
                .status(),
            StatusCode::BAD_REQUEST
        );
        assert_eq!(
            app.clone()
                .oneshot(request(&base, Some("secret"), "evil.test", None))
                .await
                .unwrap()
                .status(),
            StatusCode::FORBIDDEN
        );
        assert_eq!(
            app.clone()
                .oneshot(request(
                    &base,
                    Some("secret"),
                    "127.0.0.1:1",
                    Some("https://evil.test")
                ))
                .await
                .unwrap()
                .status(),
            StatusCode::FORBIDDEN
        );
        let traversal = format!("{base}/artifacts/%2e%2e%2fsummary.csv");
        assert_ne!(
            app.oneshot(request(&traversal, Some("secret"), "127.0.0.1:1", None))
                .await
                .unwrap()
                .status(),
            StatusCode::OK
        );
    }

    #[test]
    fn token_file_is_0600_and_removed_on_drop() {
        let temp = tempfile::tempdir().unwrap();
        let path = temp.path().join("token");
        let token = TokenFile::create(&path).unwrap();
        assert_eq!(token.token().len(), 64);
        assert!(path.exists());
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(
                fs::metadata(&path).unwrap().permissions().mode() & 0o777,
                0o600
            );
        }
        drop(token);
        assert!(!path.exists());
    }
}
