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
