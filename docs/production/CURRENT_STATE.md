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

### Phase 4 production verification corrections

- Integration now loads and asserts the current Oasis `000036_workbench_runtime.up.sql` contract and exercises all five real migrated tables, including their owner foreign keys. This caught and corrected the approval `estimated_cost` column name.
- Non-injected hosted Code and Lab startup requires both `WORKBENCH_DATABASE_URL` and `WORKBENCH_OBJECT_DIR`; neither surface can silently use SQLite or memory stores in hosted production.
- The local object adapter uses the same `objects/<object_key>` layout and traversal rules as Oasis local storage. Object keys are generated from authenticated workspace, Run, and random Artifact IDs, never request paths. The adapter is replaceable by an Oasis-compatible S3 implementation through `artifact.ObjectBackend`.
- Successful Code and Lab runs persist files created or modified during the Run with bytes, SHA-256, size, MIME/path/model metadata, and owner-scoped Postgres records before transitioning to success. Object, metadata, or usage persistence failure fails and cancels the Run.
- The final correction gates passed against the live temporary Oasis Postgres migration: full-schema integration, focused race suite, `go vet ./...`, `go build ./...`, full `go test ./...`, and diff checks.

### Phase 4 atomic execution and evidence reacceptance

- Oasis migration `000037_workbench_runtime_execution` adds approval step identity and an atomic pending/consumed/executed/failed lifecycle, plus first-class Artifact step/tool/model/input references. Lumen's integration test asserts and exercises both `000036` and `000037` against the real temporary Postgres.
- Permission review context binds the actual Run, Step, ToolCall, typed effects and parsed command/file/remote/network/output scope. An approved grant is atomically consumed before tool invocation and completed afterward; a crash leaves it consumed, so replay cannot execute the dangerous call again.
- Edited arguments invalidate the prior approval and create a new pending approval. The original waiter is not released and the API explicitly returns `reapproval_required` with the replacement ID.
- Artifact capture is driven by matching ToolDispatch/ToolResult events, not file mtimes. IDs and object keys are deterministic per Run/ToolCall, metadata is idempotent, and a metadata failure compensates by deleting the newly written object.
- Evidence export redacts arguments, reasons, commands, reasoning, tokens and secrets throughout every member; verification and provenance are structured records. Deadline and size limits cover store queries, serialization, checksums, Artifact reads and ZIP generation.
- Replacement approvals update the live waiter identity, so the second decision validates and consumes only the new grant; end-to-end Code and Lab tests preserve the invalidated original.
- Postgres Artifact persistence reserves metadata transactionally before writing bytes. Exact replay does not touch the object, changed ID/SHA or Run/ToolCall conflicts fail, and commit compensation deletes only the object created by that attempt. Concurrent and restart replay tests verify the original bytes remain intact.
- Evidence redaction preserves numeric usage counters such as `input_tokens`, `output_tokens`, and cache-token counts while still removing credential and authorization token values.

## Phase 5 unified Workbench bridge (2026-07-13)

- Code and Lab publish the strict `lumen.workbench.snapshot` v2 contract. The constructors explicitly copy only workspace/project identity, Run id/sequence/status/terminal, pending-approval count, structured verification state, and Artifact count; prompt, reasoning, tool arguments, keys, and file content cannot enter the message through object spreading.
- Lab continues publishing its original v1 snapshot unchanged alongside v2 for compatibility.
- `GET /v1/runs/{id}/workbench-snapshot` and `GET /api/lab/runs/{id}/workbench-snapshot` derive v2 runtime fields from owner-scoped durable Run, ordered event, approval, and Artifact stores. Cross-owner requests remain indistinguishable from missing Runs.
- Code and Lab chat retry requests require a new non-empty `prompt` and may include `parent_run_id`. The parent must be an owned terminal Run; the new Run records lineage and never overwrites or reconstructs the original prompt.
- Code and Lab expose owner-scoped per-Run approval reviews containing only identity, risk, typed effects, cost, lifecycle timestamps, decision, and execution state. Potentially sensitive reasons, commands, paths, targets, argument bodies/hashes, and decision actors are omitted; decisions continue through the existing owner-scoped approve endpoints.
- Code and Lab Artifact bytes are downloaded by authenticated Run ID plus Artifact ID, never a user path. Responses use sanitized attachment filenames and `nosniff`; cross-owner and unknown IDs both return not found.
- A skipped verification is represented as `not_run`, never `failed` or `passed`. `WORKBENCH_PARENT_ORIGIN` is validated as one exact HTTP(S) origin and injected as `window.__LUMEN_WORKBENCH_ORIGIN__`; bridge delivery falls back to the current origin and rejects wildcard or path-bearing targets.

## Phase 6 hosted quota enforcement (2026-07-13)

- Hosted Code and Lab reserve user/workspace concurrency through Oasis before inserting a Run. Oasis returns the durable workspace policy for wall time, steps, events, event size, Artifact size, tokens, compute and storage; a transport or malformed-policy failure closes the Run boundary instead of falling back to process-local counters.
- Usage debits are machine-authenticated and idempotent by Run/Event. Input, output, cache-read and cache-write buckets are reported with integer cost microunits; failed and canceled Runs retain actual usage. Completion is idempotent and releases the durable reservation.
- Active Runs heartbeat a 120-second durable Oasis lease at most every 60 seconds. Completion retries transient failures; if the process dies or completion remains unreachable, Oasis transactionally reaps the expired lease during reconciliation so concurrency slots cannot leak permanently.
- Artifact bytes reserve single-file and total storage quota before object I/O, then explicitly commit only after object bytes and canonical metadata are durable. Persistence or commit failure removes metadata/bytes and releases the reservation; retries use the same Artifact identity.
- Local desktop mode retains permissive in-process limits and does not require Oasis. Hosted CLI startup requires `WORKBENCH_CONTROL_PLANE_URL` and a distinct `WORKBENCH_RUNTIME_INGEST_SECRET` of at least 32 bytes.
