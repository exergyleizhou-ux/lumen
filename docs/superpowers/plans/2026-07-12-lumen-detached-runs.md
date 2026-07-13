# Lumen Detached Runs and Browser Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a Lumen Run continue when its SSE connection disappears, support explicit cancellation by Run ID, and restore progress after a browser reload.

**Architecture:** Keep the current serialized Controller for now, but detach the Agent execution context from the HTTP request cancellation signal and register an explicit per-Run cancel function in Server. Persisted run events remain the source of truth; the browser restores the sessionStorage Run ID, replays events, polls only while the Run is non-terminal, and uses the cancel endpoint for the Stop button.

**Tech Stack:** Go 1.23 HTTP/SSE, `context.WithoutCancel`, existing runstate Manager/event replay, browser JavaScript, Node syntax checks.

---

### Task 1: Add explicit active-Run lifecycle to Server

**Files:**
- Modify: `internal/server/server.go`
- Create: `internal/server/server_active_runs_test.go`

- [ ] **Step 1: Write failing lifecycle tests**

Create a request context, derive a Run execution context, cancel the request and assert the Run remains active. Call Server cancellation and assert the Run context ends with `context.Canceled`. Assert cleanup removes the active entry and repeated cancel returns false.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/server -run 'ActiveRun|DetachedRun' -count=1 -v`

- [ ] **Step 3: Implement lifecycle registry**

Add `activeRuns sync.Map` to Server and:

```go
func (s *Server) beginActiveRun(parent context.Context, runID string, timeout time.Duration) (context.Context, func())
func (s *Server) cancelActiveRun(runID string) bool
```

Use `context.WithTimeout(context.WithoutCancel(parent), timeout)`. Store its cancel function by Run ID. Cleanup cancels and deletes exactly once.

- [ ] **Step 4: Verify GREEN under race**

Run: `go test -race ./internal/server -run 'ActiveRun|DetachedRun' -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_active_runs_test.go
git commit -m "feat(server): detach run execution from SSE connections"
```

### Task 2: Add cancel API and wire chat execution

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_api.go`
- Modify: `internal/server/server_runs_test.go`

- [ ] **Step 1: Write failing API tests**

Assert `POST /v1/runs/{id}/cancel` returns 202 for an active Run, cancels its context, returns 409 for a known terminal/non-active Run, and 404 for a missing Run. GET routes remain unchanged.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/server -run 'RunCancelAPI' -count=1 -v`

- [ ] **Step 3: Implement exact cancel routing**

Allow POST only for exactly `/{id}/cancel`. Validate the Run exists, call `cancelActiveRun`, and return `{ok:true,run_id}` with 202. Never accept extra segments or encoded traversal.

- [ ] **Step 4: Detach handleChat after Run creation**

Replace the request-bound timeout context with `beginActiveRun(r.Context(), run.ID, 5*time.Minute)` and defer cleanup. Explicit cancel now produces `context.Canceled`, which existing runstate classification persists as `canceled`. Network disconnect alone does not stop execution.

- [ ] **Step 5: Verify GREEN**

Run: `go test -race ./internal/server -run 'RunCancelAPI|RunLifecycle|RunAPI' -count=1`

- [ ] **Step 6: Commit**

```bash
git add internal/server
git commit -m "feat(server): cancel active runs by id"
```

### Task 3: Make Stop explicit and restore browser Runs

**Files:**
- Modify: `internal/server/static/assets/app.js`
- Modify: `internal/server/static_assets_test.go`

- [ ] **Step 1: Add failing static contract assertions**

Require `/cancel`, `restoreStoredRun`, `sessionStorage.getItem("lumen_active_run")`, and bounded one-second polling literals in app.js.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/server -run '^TestEmbeddedAppJS' -count=1 -v`

- [ ] **Step 3: Wire explicit Stop**

Make `stopGeneration` async. If `currentRunId` is known, POST its cancel endpoint before aborting the local fetch. If no Run ID arrived yet, abort only the local request.

- [ ] **Step 4: Restore a saved Run on init**

`restoreStoredRun` loads `{runId,lastSeq}`, validates the Run, creates one assistant message, replays all persisted events through a small renderer, then polls `GET events?after=` and `GET run` once per second only while status is non-terminal. It updates the same sessionStorage cursor, exposes the Stop button during restoration, and clears the active record after a terminal state is rendered.

- [ ] **Step 5: Verify syntax and server tests**

```bash
node --check internal/server/static/assets/app.js
go test ./internal/server -count=1
```

- [ ] **Step 6: Commit**

```bash
git add internal/server/static/assets/app.js internal/server/static_assets_test.go
git commit -m "feat(web): restore and cancel detached runs"
```

### Task 4: Detached Run completion gate

- [ ] **Step 1: Focused race and repetition**

```bash
go test -race ./internal/server -count=1
go test ./internal/server -run 'ActiveRun|RunCancelAPI|RunLifecycle' -count=10
```

- [ ] **Step 2: Repository gates**

```bash
go vet ./...
go build ./...
go test ./... -count=1
make test-integration
```

- [ ] **Step 3: Audit**

```bash
git status --short
git diff --check main...HEAD
```

Expected: a dropped SSE connection no longer cancels a Run; explicit Stop produces a canceled terminal Run; reload resumes from persisted sequence without duplicating events.
