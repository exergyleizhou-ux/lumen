//! P4 connector admission policy. Seam contract: S3.
//!
//! This module never opens a socket or reads a credential. A future Lumen tool
//! dispatcher must obtain an [`AuthorizedTarget`] before it can invoke an SSH,
//! SCP, MCP, or private-compute connector.

use crate::{ProjectId, Result, RunContext, ScienceError};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct RemoteTarget {
    pub host: String,
    pub port: u16,
    /// SHA-256 fingerprint of the approved host key, never a private key.
    pub host_key_sha256: String,
    pub max_timeout_ms: u64,
    pub allow_data_egress: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ConnectorPolicy {
    pub project_id: ProjectId,
    pub owner_id: String,
    pub targets: Vec<RemoteTarget>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ConnectorRequest {
    pub host: String,
    pub port: u16,
    pub host_key_sha256: String,
    pub timeout_ms: u64,
    pub data_egress: bool,
}

/// A request accepted by a project-scoped policy. This deliberately excludes
/// passwords, tokens, private keys, commands, and any transport handle.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthorizedTarget {
    pub host: String,
    pub port: u16,
    pub host_key_sha256: String,
    pub timeout_ms: u64,
    pub data_egress: bool,
}

pub fn authorize(
    policy: &ConnectorPolicy,
    context: &RunContext,
    request: &ConnectorRequest,
) -> Result<AuthorizedTarget> {
    if policy.project_id != context.project_id || policy.owner_id != context.owner_id {
        return Err(ScienceError::Ownership);
    }
    validate_request(request)?;
    let target = policy
        .targets
        .iter()
        .find(|target| {
            target.host == request.host
                && target.port == request.port
                && target.host_key_sha256 == request.host_key_sha256
        })
        .ok_or_else(|| ScienceError::Invalid("remote target is not allowlisted".into()))?;
    if request.timeout_ms > target.max_timeout_ms {
        return Err(ScienceError::Invalid(
            "remote timeout exceeds project policy".into(),
        ));
    }
    if request.data_egress && !target.allow_data_egress {
        return Err(ScienceError::Invalid(
            "remote data egress is forbidden by project policy".into(),
        ));
    }
    Ok(AuthorizedTarget {
        host: target.host.clone(),
        port: target.port,
        host_key_sha256: target.host_key_sha256.clone(),
        timeout_ms: request.timeout_ms,
        data_egress: request.data_egress,
    })
}

fn validate_request(request: &ConnectorRequest) -> Result<()> {
    if request.host.is_empty()
        || !request.host.is_ascii()
        || request.host.contains(char::is_whitespace)
        || request.host.contains('/')
        || request.host.contains('@')
        || request.port == 0
        || request.timeout_ms == 0
        || request.host_key_sha256.len() != 64
        || !request
            .host_key_sha256
            .bytes()
            .all(|byte| byte.is_ascii_hexdigit())
    {
        return Err(ScienceError::Invalid(
            "invalid remote connector request".into(),
        ));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{RunContext, RunId};
    use std::{collections::BTreeMap, path::PathBuf};

    const FINGERPRINT: &str = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

    fn context(project: &str, owner: &str) -> RunContext {
        RunContext {
            run_id: RunId::new_v7(),
            project_id: ProjectId::new(project),
            session_id: "session".into(),
            owner_id: owner.into(),
            workspace_root: PathBuf::from("/workspace"),
            provider: "offline".into(),
            approval_policy: "ask".into(),
            tool_profile: "science".into(),
            artifact_root: PathBuf::from("/workspace/artifacts"),
            environment: BTreeMap::new(),
        }
    }

    fn policy() -> ConnectorPolicy {
        ConnectorPolicy {
            project_id: ProjectId::new("p"),
            owner_id: "alice".into(),
            targets: vec![RemoteTarget {
                host: "hpc.example.test".into(),
                port: 22,
                host_key_sha256: FINGERPRINT.into(),
                max_timeout_ms: 30_000,
                allow_data_egress: false,
            }],
        }
    }

    fn request() -> ConnectorRequest {
        ConnectorRequest {
            host: "hpc.example.test".into(),
            port: 22,
            host_key_sha256: FINGERPRINT.into(),
            timeout_ms: 10_000,
            data_egress: false,
        }
    }

    #[test]
    fn exact_project_owner_host_key_and_timeout_are_required() {
        assert!(authorize(&policy(), &context("p", "alice"), &request()).is_ok());
        assert!(matches!(
            authorize(&policy(), &context("other", "alice"), &request()),
            Err(ScienceError::Ownership)
        ));
        assert!(matches!(
            authorize(&policy(), &context("p", "other"), &request()),
            Err(ScienceError::Ownership)
        ));
        let mut wrong_key = request();
        wrong_key.host_key_sha256 = "f".repeat(64);
        assert!(authorize(&policy(), &context("p", "alice"), &wrong_key).is_err());
        let mut long = request();
        long.timeout_ms = 30_001;
        assert!(authorize(&policy(), &context("p", "alice"), &long).is_err());
    }

    #[test]
    fn egress_and_malformed_targets_fail_closed() {
        let mut egress = request();
        egress.data_egress = true;
        assert!(authorize(&policy(), &context("p", "alice"), &egress).is_err());
        let mut malformed = request();
        malformed.host = "user@hpc.example.test".into();
        assert!(authorize(&policy(), &context("p", "alice"), &malformed).is_err());
    }
}
