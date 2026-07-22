//! P4 SSH/SCP Science admission through the existing Lumen permission manager.
//!
//! This is intentionally separate from `handle.rs`: that file has concurrent
//! owner work, while this extension only adds a new SessionHandle capability.

use super::{SessionCommand, SessionHandle};
use agent_client_protocol as acp;
use tokio::sync::oneshot;
use xai_grok_science::{
    ApprovalDecision, RunContext, ScienceError, ScienceStore,
    connector::{AdmissionTicket, ConnectorPolicy, ConnectorRequest},
};

impl SessionHandle {
    /// Executes the P4 ordering invariant:
    ///
    /// `SessionActor durable admission -> Lumen permission manager ->
    /// SessionActor durable terminal decision`.
    ///
    /// A denied admission returns `Ok(None)` without asking permission. An
    /// allowed permission returns an opaque ticket only; this method has no
    /// transport implementation and cannot open a socket or start a remote job.
    pub async fn admit_science_ssh_scp_with_approval_timeout(
        &self,
        store: ScienceStore,
        context: RunContext,
        policy: ConnectorPolicy,
        request: ConnectorRequest,
        approval_timeout: std::time::Duration,
    ) -> xai_grok_science::Result<Option<AdmissionTicket>> {
        use xai_grok_workspace::permission::{AccessKind, Decision};

        let (begin_tx, begin_rx) = oneshot::channel();
        self.cmd_tx
            .send(SessionCommand::BeginScienceSshScpAdmission {
                store,
                context,
                policy,
                request,
                respond_to: begin_tx,
            })
            .map_err(|_| ScienceError::Invalid("session actor unavailable".into()))?;
        let Some(prepared) = begin_rx
            .await
            .map_err(|_| ScienceError::Invalid("session actor stopped".into()))??
        else {
            return Ok(None);
        };

        // This is deliberately generic: the target identity remains in the
        // project policy and redacted Science audit, not in a shell command or
        // permission-manager free-form reason.
        let call_id = acp::ToolCallId::new(std::sync::Arc::from(prepared.ticket.call_id.0.clone()));
        let update = acp::ToolCallUpdate::new(
            call_id,
            acp::ToolCallUpdateFields::new()
                .kind(Some(acp::ToolKind::Other))
                .title(Some("Lumen Science SSH/SCP connector admission".into())),
        );
        let permission = tokio::time::timeout(
            approval_timeout,
            self.permission_handle.request(
                AccessKind::Bash("Lumen Science SSH/SCP connector transport admission".into()),
                update,
                Some(self.info.id.0.to_string()),
                None,
                None,
            ),
        )
        .await;
        let decision = match permission {
            Err(_) => ApprovalDecision::Timeout,
            Ok(Decision::Allow) => ApprovalDecision::Allow,
            Ok(Decision::Ask)
            | Ok(Decision::Reject(_))
            | Ok(Decision::PolicyDeny(_))
            | Ok(Decision::FollowupMessage(_)) => ApprovalDecision::Deny,
            Ok(Decision::Cancelled) => ApprovalDecision::Cancel,
        };
        let (finish_tx, finish_rx) = oneshot::channel();
        self.cmd_tx
            .send(SessionCommand::FinishScienceSshScpAdmission {
                prepared,
                decision,
                respond_to: finish_tx,
            })
            .map_err(|_| ScienceError::Invalid("session actor unavailable".into()))?;
        finish_rx
            .await
            .map_err(|_| ScienceError::Invalid("session actor stopped".into()))?
    }
}
