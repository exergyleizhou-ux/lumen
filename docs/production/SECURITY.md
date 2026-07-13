# Lumen Production Security

## Authentication and tenant isolation

- Hosted requests require a valid short-lived Oasis Workbench JWT. HS256 secrets
  shorter than 32 bytes fail startup; algorithm, issuer, audience, expiry, user,
  workspace, and permissions are checked explicitly.
- Runs, events, cancellation, approvals, usage, Artifacts, evidence, projects,
  files, controllers, and quota leases are owner scoped. Unknown and cross-owner
  resources return the same not-found result.
- Tenant identifiers are safe path components. Descriptor-rooted file operations
  and no-follow writes reject traversal, symlink escape, and replacement races.
  Hosted controllers and Lab registries are bounded and tenant keyed.

## Secrets and provider policy

- Hosted provider selection and credentials are immutable startup inputs.
  Request keys, tenant provider files, `.env` reload, and `/model` switching are
  rejected; request handling does not mutate process environment.
- LangGraph receives a scrubbed environment containing only the selected
  platform provider variables. Tokens, authorization/cookie values, prompts,
  reasoning, tool arguments, file contents, and provider keys are excluded from
  logs, snapshots, reviews, and redacted evidence members.
- Secrets belong in the deployment secret manager, never Git, images, Compose
  overrides, shell history, or evidence files. Exact values must be rotated at
  source after any suspected disclosure.

## Execution and data integrity

- Approval grants bind Run, step, tool call, typed effects, scoped arguments,
  canonical hash, owner, expiry, and decision. Consumption is atomic and
  crash-safe; edited arguments create a new approval and cannot reuse a grant.
- Event and terminal-state persistence fail closed. Sequence allocation is
  contiguous, terminal races admit one winner, and replay/idempotency boundaries
  prevent duplicate effects, usage, and Artifact replacement.
- Artifact bytes are quota-reserved before I/O, hash checked, metadata-bound to
  Run/tool call, and compensated on failure. Downloads use authenticated IDs,
  sanitized filenames, and `nosniff`, never caller-controlled paths.
- Hosted JSON and multipart bodies are capped. CORS, CSP frame ancestors, and
  Workbench `postMessage` use one exact configured HTTP(S) origin; wildcard and
  suffix matching are rejected.

## Residual operational controls

Production must provide TLS, private service networking, encrypted and backed-up
volumes, secret rotation, access-controlled logs, Oasis/platform metrics, alerting,
snapshot validation, and an incident process. These infrastructure controls are
documented but cannot be proven without the authorized production environment.
