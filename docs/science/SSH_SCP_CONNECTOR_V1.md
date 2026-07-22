# Lumen Science SSH/SCP connector capability v1

Seam contracts: **S3, S4**. Status: P4 offline admission contract; no socket,
DNS lookup, credential lookup, subprocess, or remote-job recovery exists yet.

## Exact capability

This capability is only a project-scoped SSH/SCP remote-target admission. It
does not grant shell execution, port forwarding, arbitrary SCP paths, MCP, or
private-compute access. A future transport must introduce a separate contract
for each operation and must not widen this one by adding an operation string.

An input is accepted only if all of these checks pass before Lumen asks for
permission or dispatches a tool:

1. `RunContext.project_id` and `owner_id` exactly match the policy.
2. Hostname, port, and SHA-256 host-key fingerprint exactly match one approved
   target. The requested timeout is positive and not greater than that target's
   maximum; requested egress requires explicit target permission.
3. Hostnames are ASCII names with no whitespace, slash, or userinfo. Literal
   IP addresses, `localhost`, `*.localhost`, and `*.local` are rejected.

Admission does no DNS resolution. This keeps P4 offline and avoids treating a
DNS answer as an authorization decision. The real SSH transport is not in this
change; when it is added, it must resolve immediately before connect, reject
loopback/link-local/private/reserved answers, pin the supplied host-key hash,
and create no retryable background job.

## Dispatch order and audit

`SessionHandle::admit_science_ssh_scp_with_approval_timeout` calls
`SessionActor` admission **before** it requests the existing Lumen permission
manager. Rejection therefore produces no socket, DNS query, subprocess,
credential lookup, or `WorkspaceOps::call_tool` call. After a Science run
exists, both branches append a `connector.admission` event using
`AdmissionAudit`; it stores an irreversible target correlation hash, not the
hostname, host-key fingerprint, password, token, private key, command, or
transport handle. The terminal permission result is sent back through
`SessionActor` and appended as `connector.permission`. Allow returns only an
opaque admission ticket; it cannot dispatch a transport yet.

Cancellation, timeout, and restart policy are inherited from the existing
Science approval contract: no pending remote job is recovered or automatically
retried. A real transport must prove process/socket cleanup for both timeout
and cancellation before it can be considered P4 complete.
