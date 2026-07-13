# Lumen Production Architecture

## Authority and boundaries

Oasis is the hosted identity, policy, quota, and billing control plane. Lumen is
the only execution runtime and durable Run-state authority. Code and Science Lab
are profiles over the same Run, event, approval, usage, Artifact, and evidence
contracts; Oasis consumes their owner-filtered Workbench snapshots and does not
maintain a second agent state machine.

In hosted mode, Oasis supplies a short-lived Workbench JWT. Lumen validates the
issuer, audience, algorithm, signature, expiry, user, workspace, and permissions
before a business handler runs. The authenticated `(user_id, workspace_id)` is
propagated into owner-scoped stores, workspace roots, controller registries,
quota reservations, Artifacts, approvals, and evidence exports. Cross-owner
lookup is deliberately indistinguishable from absence.

## Runtime flow

1. Code or Lab authenticates the request and reserves quota with Oasis before
   creating a Run.
2. The shared Runtime persists the owned Run and monotonically ordered events in
   Postgres. Local desktop mode uses SQLite and explicit `local/local` identity.
3. A tenant-scoped controller executes with immutable startup provider policy
   and a guarded workspace. Typed tool effects drive approval and verification.
4. Dangerous work atomically consumes an argument-bound approval before tool
   invocation. Tool results drive deterministic Artifact capture.
5. Usage, Artifact metadata/bytes, terminal state, and quota completion are
   persisted fail-closed. Reconnects replay durable events; the browser is never
   authoritative.
6. Owner-scoped snapshots, approval summaries, Artifact downloads, and evidence
   bundles are exposed to Oasis.

## Production topology

TLS/platform ingress forwards the exact Host, Origin, Cookie, Authorization, and
request ID to loopback Caddy. Caddy proxies to private Code and Lab containers;
neither runtime publishes a host port. Both depend on Oasis-migrated Postgres,
encrypted object/workspace volumes, the private Oasis control plane, and one
startup-resolved platform provider. `/healthz` proves process liveness while
`/readyz` checks the dependencies needed to accept work.

The production Compose and scripts are release-candidate material only. No
public deployment, remote push, or merge to `main` is performed by this branch.
