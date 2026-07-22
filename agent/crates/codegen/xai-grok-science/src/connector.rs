//! P4 connector admission policy. Seam contract: S3.
//!
//! This module never opens a socket or reads a credential. A future Lumen tool
//! dispatcher must obtain an [`AuthorizedTarget`] before it can invoke an SSH,
//! SCP, MCP, or private-compute connector. P4 currently defines the SSH/SCP
//! capability only; it is deliberately not a generic transport admission API.

use crate::{
    Approval, ApprovalDecision, CallId, ProjectId, Result, RunContext, RunState, ScienceError,
    ScienceStore,
};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

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

/// Safe-to-persist outcome of connector admission. It intentionally contains
/// neither the requested hostname nor the host-key fingerprint: those values
/// can identify infrastructure and must not be copied into general event/error
/// surfaces. `target_sha256` is only a stable audit correlation identifier.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AdmissionAudit {
    pub connector: String,
    pub outcome: AdmissionOutcome,
    pub target_sha256: String,
    pub timeout_ms: u64,
    pub data_egress: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AdmissionOutcome {
    Allowed,
    Denied,
}

/// Durable result of an SSH/SCP admission attempt. `Ready` means only that
/// policy admission passed and a Lumen approval is pending; it never means a
/// connection was opened or a remote job was started.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AdmissionStart {
    Ready(Box<AdmissionTicket>),
    Denied,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AdmissionTicket {
    pub context: RunContext,
    pub call_id: CallId,
    pub target: AuthorizedTarget,
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

/// Builds the only connector payload that may be placed in a durable Science
/// event. Call this on both allow and deny paths; callers must not substitute a
/// raw request or a formatted error, which could leak a host key or future
/// credential-bearing request fields.
pub fn admission_audit(request: &ConnectorRequest, outcome: AdmissionOutcome) -> AdmissionAudit {
    let mut hasher = Sha256::new();
    hasher.update(b"lumen-science-ssh-scp-target-v1\0");
    hasher.update(request.host.as_bytes());
    hasher.update(b":");
    hasher.update(request.port.to_be_bytes());
    AdmissionAudit {
        connector: "ssh-scp-v1".into(),
        outcome,
        target_sha256: format!("{:x}", hasher.finalize()),
        timeout_ms: request.timeout_ms,
        data_egress: request.data_egress,
    }
}

/// Creates the durable, project-owned admission record before a caller reaches
/// the Lumen permission manager. This function is pure with respect to remote
/// systems: it never resolves DNS, opens a socket, starts a process, or reads a
/// credential. A rejected request becomes a terminal `Denied` run with a
/// redacted replayable event; an accepted request becomes `AwaitingApproval`.
pub fn start_ssh_scp_admission(
    store: &ScienceStore,
    context: RunContext,
    policy: &ConnectorPolicy,
    request: &ConnectorRequest,
) -> Result<AdmissionStart> {
    store.create_run(context.clone())?;
    let authorization = authorize(policy, &context, request);
    match authorization {
        Ok(target) => {
            store.append_event(
                &context.run_id,
                "ScienceConnectorAdmission",
                "connector.admission",
                serde_json::to_value(admission_audit(request, AdmissionOutcome::Allowed))?,
            )?;
            let call_id = CallId::new(format!("science-ssh-scp-{}", context.run_id.0));
            store.request_approval(Approval {
                project_id: context.project_id.clone(),
                run_id: context.run_id.clone(),
                call_id: call_id.clone(),
                owner_id: context.owner_id.clone(),
                decision: ApprovalDecision::Pending,
                decided_at: None,
            })?;
            store.transition(&context.run_id, RunState::AwaitingApproval, None)?;
            Ok(AdmissionStart::Ready(Box::new(AdmissionTicket {
                context,
                call_id,
                target,
            })))
        }
        Err(error) => {
            store.append_event(
                &context.run_id,
                "ScienceConnectorAdmission",
                "connector.admission",
                serde_json::to_value(admission_audit(request, AdmissionOutcome::Denied))?,
            )?;
            store.transition(
                &context.run_id,
                RunState::Denied,
                Some("connector admission denied".into()),
            )?;
            let _ = error;
            Ok(AdmissionStart::Denied)
        }
    }
}

fn validate_request(request: &ConnectorRequest) -> Result<()> {
    if request.host.is_empty()
        || !request.host.is_ascii()
        || request.host.contains(char::is_whitespace)
        || request.host.contains('/')
        || request.host.contains('@')
        || is_non_remote_or_ip_literal(&request.host)
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

/// SSH/SCP P4 accepts a DNS name only. We intentionally reject loopback,
/// `.localhost`, all numeric IP literals, and names that syntactically look
/// like a private network endpoint. This module never resolves DNS, so there
/// is no TOCTOU-prone name-to-address decision here. A future real transport
/// must re-check the resolved address set against this same public-only policy
/// immediately before opening its socket.
fn is_non_remote_or_ip_literal(host: &str) -> bool {
    let lower = host.to_ascii_lowercase();
    lower == "localhost"
        || lower.ends_with(".localhost")
        || lower.ends_with(".local")
        || lower.parse::<std::net::IpAddr>().is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{RunContext, RunId, ScienceStore};
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
        for host in ["localhost", "worker.localhost", "cluster.local", "127.0.0.1", "10.0.0.7", "::1"] {
            malformed.host = host.into();
            assert!(authorize(&policy(), &context("p", "alice"), &malformed).is_err());
        }
    }

    #[test]
    fn durable_audit_is_redacted_and_stable() {
        let request = request();
        let audit = admission_audit(&request, AdmissionOutcome::Denied);
        let json = serde_json::to_string(&audit).unwrap();
        assert_eq!(audit.connector, "ssh-scp-v1");
        assert_eq!(audit.outcome, AdmissionOutcome::Denied);
        assert_eq!(audit.target_sha256.len(), 64);
        assert!(!json.contains(&request.host));
        assert!(!json.contains(&request.host_key_sha256));
        assert_eq!(audit, admission_audit(&request, AdmissionOutcome::Denied));
    }

    #[test]
    fn admission_is_durable_redacted_and_never_executes_a_transport() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let context = context("p", "alice");
        let run_id = context.run_id.clone();
        let start = start_ssh_scp_admission(&store, context, &policy(), &request()).unwrap();
        let AdmissionStart::Ready(ticket) = start else {
            panic!("allowlisted target must await Lumen approval");
        };
        assert_eq!(ticket.target.host, "hpc.example.test");
        assert_eq!(store.load_run(&run_id).unwrap().state, RunState::AwaitingApproval);
        assert_eq!(store.approvals(&run_id).unwrap()[0].decision, ApprovalDecision::Pending);
        let events = store.events_after(&run_id, 0, 10).unwrap();
        assert_eq!(events.len(), 1);
        let event = &events[0];
        assert_eq!(event.kind, "connector.admission");
        let serialized = serde_json::to_string(event).unwrap();
        assert!(!serialized.contains("hpc.example.test"));
        assert!(!serialized.contains(FINGERPRINT));
    }

    #[test]
    fn denied_admission_creates_no_pending_permission() {
        let temp = tempfile::tempdir().unwrap();
        let store = ScienceStore::new(temp.path());
        let context = context("p", "alice");
        let run_id = context.run_id.clone();
        let mut bad = request();
        bad.host = "localhost".into();
        assert_eq!(
            start_ssh_scp_admission(&store, context, &policy(), &bad).unwrap(),
            AdmissionStart::Denied
        );
        assert_eq!(store.load_run(&run_id).unwrap().state, RunState::Denied);
        assert!(store.approvals(&run_id).unwrap().is_empty());
        let event = store.events_after(&run_id, 0, 10).unwrap().pop().unwrap();
        assert_eq!(event.payload["outcome"], "denied");
        assert!(!serde_json::to_string(&event).unwrap().contains("localhost"));
    }
}
