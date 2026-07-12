# Lumen Production Finalization Baseline

Recorded on 2026-07-12 before production-finalization changes.

## Repository

- Worktree: `/Users/lei/lumen/.worktrees/lumen-production-runtime`
- Branch: `feat/lumen-production-runtime`
- HEAD: `29317554fda563f8cbcabcf648a9712705a69e30` (`feat(lab-ui): publish workbench runtime snapshots`)
- The worktree was clean before this document was created.

## Verified baseline

The following commands completed successfully at the recorded HEAD:

```text
go test -race ./internal/science/lab ./internal/runstate
go vet ./...
go build ./...
go test ./...
git diff --check main...HEAD
```

The Go test output was served from the build cache.

## Confirmed runtime and API facts

- `runstate.Manager` is the authoritative in-process Run state machine and `runstate.Store` defines `CreateRun`, `UpdateRun`, `GetRun`, `AppendEvent`, and `Events` persistence boundaries.
- `runstate.Run` has durable lifecycle, profile, session, parent, timestamps, and version fields, but does not have persisted `user_id` or `workspace_id` ownership fields.
- Code exposes `GET /v1/runs/{id}`, `GET /v1/runs/{id}/events`, and `POST /v1/runs/{id}/cancel`.
- Lab exposes `GET /api/lab/runs/{id}`, `GET /api/lab/runs/{id}/events`, `POST /api/lab/runs/{id}/cancel`, and `GET /api/lab/artifacts?project_id=...`.
- `workspace.Context` carries `WorkspaceID`, `Root`, `UserID`, environment overrides, and a path-resolving backend through `context.Context`.
- Tools can declare typed `tool.Effects`; tools without an explicit declaration retain the conservative compatibility fallback.
- Code Run execution is detached from the SSE request and has an explicit active-Run cancel registry; the browser restores durable Runs from replayable events.
- Lab uses the same `runstate.Manager`, links provenance records to Run IDs, supports detached execution/cancellation/replay, and publishes a versioned runtime snapshot to its parent window.
- Code HTTP handling still shares one mutable Controller behind a global turn mutex and still accepts a request API key that can update process environment.
- Run lookup, replay, cancellation, file access, Lab projects, and artifacts are not currently owner-scoped at the Store/Manager boundary.
- No hosted Workbench JWT verifier or hosted-mode HTTP authorization layer exists in this baseline.

## Phase 2 owner isolation (2026-07-12)

- Runs now persist immutable `user_id` and `workspace_id`; owner-scoped Manager APIs return `run not found` across tenant boundaries, including after SQLite restart.
- Code and Lab run lookup, event replay, active cancellation, and Code approval decisions enforce the authenticated owner; local mode uses the explicit `local/local` owner.
- Hosted Code allocates Controllers by user/workspace/session with distinct workspace contexts and fail-fast global, per-user, and per-workspace capacity limits.
- Hosted Lab uses a bounded tenant registry. Each tenant receives a guarded `<HOSTED_WORKSPACE_ROOT>/<user>/<workspace>/science` root, project store, controller pool, session, and artifact namespace; idle entries are LRU-evicted.
- Tenant identifiers are strict safe path components, and workspace resolution rejects traversal and symlink escapes.
- Phase 2 gates passed: targeted race suite, uncached full test suite, `go vet ./...`, and `git diff --check`.

## Phase 3 request isolation and usage capture (2026-07-12)

- Code and Lab provider credentials are immutable controller inputs. Browser API keys never pass through `os.Setenv`, and hosted controller configuration does not load `.env` during a request.
- Hosted Code rejects body `api_key` with stable code `provider_key_forbidden`; provider/model selection is limited to the startup-resolved allowlist and remains immutable for a session.
- Hosted `/model` switching is disabled because it would bypass that allowlist. Local temporary keys remain supported as per-run `ProviderConfig` values without changing process environment.
- Concurrent controllers receive defensive copies of distinct provider configurations. Hosted workspace root and base PATH are snapshotted in server configuration at startup; derived PATH overrides, tool profile, and permission mode stay controller/workspace scoped.
- Local Code HTTP reuses its startup-configured controller or configures with `ProcessEnvImmutable`; neither chat nor workflow loads `.env` or changes process environment. Hosted Lab ignores tenant provider files and uses an immutable startup platform provider, while its config endpoint rejects tenant key/base URL/model/provider fields.
- Hosted controllers use `ProviderOnly`, so config-file providers are not instantiated as fallback backends. Hosted LangGraph exclusively receives the startup platform provider and never resolves tenant science configuration.
- LangGraph child processes receive a sanitized environment with all ambient provider keys, endpoints, and model selectors removed; provider-only runner variables inject exactly the selected platform provider and disable Python-side fallback. Hosted LangGraph readiness is derived from that platform provider rather than process environment keys.
- Code and Lab capture stamped `event.Usage` into the `usage.Store` boundary with owner, workspace, run/event identity, provider/model, input/output/cache tokens, and integer estimated cost. The Phase 3 memory store enforces `(run_id,event_id)` idempotency; non-duplicate store failures emit an explicit error event instead of silently losing billable usage. Phase 4 can replace it with Postgres.
- Phase 3 gates passed: the required targeted race suite (plus `internal/usage` and Lab integration), uncached `go test ./...`, and `git diff --check`.

## Phase 4 durable runtime evidence (2026-07-13)

- Hosted Code and Lab select the shared Postgres runtime when `WORKBENCH_DATABASE_URL` is set; local desktop continues to use SQLite. Startup fails closed when the configured hosted database is unavailable.
- `runstate/pgstore` implements the Oasis `workbench_runs` and `workbench_events` contract. Run transitions use version compare-and-swap, refresh stale in-memory state after conflict, and event replay is idempotent on `(run_id, seq)`.
- Approval records persist owner, risk, typed effects, command/file/network/remote scope, estimated cost, outputs, editable arguments, canonical SHA-256 argument hash, expiry, decision, and decision actor. Code and Lab revalidate owner, expiry, decision and the actual execution arguments after the browser responds; changed arguments cannot execute under an old grant.
- Usage has a Postgres implementation with the migration's `(run_id, event_id)` replay boundary. Artifact metadata uses an adapter-compatible object backend so Lumen can consume the Oasis local/S3 semantics without embedding another object-storage client.
- Code and Lab expose owner-gated per-Run Artifact and Evidence endpoints. Bundles contain `manifest.json`, `run.json`, redacted `events.jsonl`, `approvals.json`, `verification.json`, `provenance.jsonl`, `usage.json`, artifact bytes, and `SHA256SUMS`; generation verifies artifact hashes and enforces safe names, a 100 MiB default limit, and a 30-second default timeout.
- Phase 4 gates passed: real-Postgres CAS/idempotency integration, targeted race suite, `go vet ./...`, `go build ./...`, uncached full `go test ./...`, and `git diff --check`.
