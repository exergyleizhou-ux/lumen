# Lumen Run State and Versioned Events Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every Lumen task a durable run identity, validated terminal state, ordered versioned events, and replay APIs so CLI/Web/Lab can observe and recover work without interpreting chat text.

**Architecture:** Add an `internal/runstate` domain package above `event` and `lumenstore`. A Manager owns the in-process state machine, a stamping Sink assigns run IDs and monotonically increasing sequence numbers, and SQLite stores run snapshots plus event JSON. The HTTP server starts/finishes runs and exposes query/replay endpoints; existing Agent behavior remains intact.

**Tech Stack:** Go 1.23, `database/sql`, modernc SQLite, existing `event.Sink`, HTTP/SSE, TDD.

---

## File map

- Create `internal/runstate/types.go`: Run, Status, terminal classification and transition rules.
- Create `internal/runstate/types_test.go`: state machine and error classification tests.
- Create `internal/runstate/manager.go`: lifecycle, event stamping and replay orchestration.
- Create `internal/runstate/manager_test.go`: monotonic sequence, isolation and finish tests.
- Modify `internal/event/event.go`: protocol metadata fields.
- Create `internal/lumenstore/run.go`: run/event SQLite CRUD using storage-only record types.
- Create `internal/lumenstore/run_test.go`: migration, optimistic transition and replay tests.
- Modify `internal/lumenstore/store.go`: run table migrations.
- Modify `internal/server/server.go`: Manager ownership and chat lifecycle integration.
- Modify `internal/server/server_api.go`: run query/event replay routes.
- Create `internal/server/server_runs_test.go`: HTTP contract tests.
- Modify `internal/server/static/assets/app.js`: capture `run_id`, preserve terminal state, and provide one reconnect replay path.

### Task 1: Define the Run state machine

**Files:**
- Create: `internal/runstate/types.go`
- Create: `internal/runstate/types_test.go`

- [ ] **Step 1: Write transition and terminal-classification tests**

Create table-driven tests covering:

```go
func TestCanTransition(t *testing.T) {
    cases := []struct {
        from, to Status
        want     bool
    }{
        {StatusQueued, StatusRunning, true},
        {StatusRunning, StatusWaitingApproval, true},
        {StatusWaitingApproval, StatusRunning, true},
        {StatusRunning, StatusVerifying, true},
        {StatusVerifying, StatusRunning, true},
        {StatusRunning, StatusSucceeded, true},
        {StatusRunning, StatusFailed, true},
        {StatusRunning, StatusCanceled, true},
        {StatusRunning, StatusTimedOut, true},
        {StatusRunning, StatusExhausted, true},
        {StatusSucceeded, StatusRunning, false},
        {StatusFailed, StatusSucceeded, false},
    }
    for _, tc := range cases {
        if got := CanTransition(tc.from, tc.to); got != tc.want {
            t.Errorf("CanTransition(%s,%s)=%v want %v", tc.from, tc.to, got, tc.want)
        }
    }
}
```

Add tests that `ClassifyTerminal(nil)` is succeeded, `agent.ErrMaxStepsExhausted` is exhausted, `context.Canceled` is canceled, `context.DeadlineExceeded` is timed out, and an ordinary error is failed.

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./internal/runstate -count=1 -v
```

Expected: build failure because the package and types do not exist.

- [ ] **Step 3: Implement domain types**

Define:

```go
type Status string

const (
    StatusQueued          Status = "queued"
    StatusRunning         Status = "running"
    StatusWaitingApproval Status = "waiting_approval"
    StatusVerifying       Status = "verifying"
    StatusSucceeded       Status = "succeeded"
    StatusFailed          Status = "failed"
    StatusCanceled        Status = "canceled"
    StatusTimedOut        Status = "timed_out"
    StatusExhausted       Status = "exhausted"
)

type Run struct {
    ID         string     `json:"id"`
    SessionID  string     `json:"session_id,omitempty"`
    ParentID   string     `json:"parent_run_id,omitempty"`
    Profile    string     `json:"profile"`
    Title      string     `json:"title"`
    Status     Status     `json:"status"`
    StopReason string     `json:"stop_reason,omitempty"`
    Error      string     `json:"error,omitempty"`
    CreatedAt  time.Time  `json:"created_at"`
    UpdatedAt  time.Time  `json:"updated_at"`
    StartedAt  *time.Time `json:"started_at,omitempty"`
    FinishedAt *time.Time `json:"finished_at,omitempty"`
    Version    uint64     `json:"version"`
}
```

`CanTransition` must reject every transition out of a terminal status. `ClassifyTerminal` must use `errors.Is` and return `(status, stopReason)`.

- [ ] **Step 4: Verify GREEN**

```bash
go test ./internal/runstate -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runstate/types.go internal/runstate/types_test.go
git commit -m "feat(runtime): define durable run state machine"
```

### Task 2: Add version metadata to Agent events

**Files:**
- Modify: `internal/event/event.go`
- Create: `internal/runstate/manager.go`
- Create: `internal/runstate/manager_test.go`

- [ ] **Step 1: Write stamping sink tests**

Use a collecting inner sink and assert three emitted events receive:

```go
if got[0].SchemaVersion != 1 || got[0].RunID != run.ID || got[0].Seq != 1 {
    t.Fatalf("first event not stamped: %#v", got[0])
}
if got[1].Seq != 2 || got[2].Seq != 3 {
    t.Fatalf("non-monotonic sequence: %#v", got)
}
if got[1].EventID == got[2].EventID {
    t.Fatal("event ids must be unique")
}
```

Add a concurrent test with 32 goroutines and assert unique contiguous sequences `1..32`. Add a two-run test proving counters are independent.

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./internal/runstate -run 'Stamp|Concurrent|Independent' -count=1 -race
```

Expected: build failure because Event metadata and Manager do not exist.

- [ ] **Step 3: Extend `event.Event`**

Add non-breaking JSON fields:

```go
SchemaVersion int    `json:"schema_version,omitempty"`
Seq           uint64 `json:"seq,omitempty"`
EventID       string `json:"event_id,omitempty"`
RunID         string `json:"run_id,omitempty"`
StepID        string `json:"step_id,omitempty"`
ToolCallID    string `json:"tool_call_id,omitempty"`
```

Keep existing fields unchanged. The stamping sink sets `ToolCallID = Tool.ID` when the field is empty.

- [ ] **Step 4: Implement Manager and stamping Sink in memory**

`NewManager(nil)` must work without SQLite. `Start` generates a cryptographically random `run_` ID, creates a queued run, immediately transitions it to running, and returns a copy. `WrapSink` returns a sink that locks only that run's sequence counter, stamps a copy of the event, stores it, then forwards it.

Define the persistence boundary in `manager.go` now so later tasks do not change the Manager API:

```go
type Store interface {
    CreateRun(run Run) error
    UpdateRun(run Run, expectedVersion uint64) error
    GetRun(id string) (Run, error)
    AppendEvent(event.Event) error
    Events(runID string, afterSeq uint64) ([]event.Event, error)
}
```

Required public methods:

```go
func NewManager(store Store) *Manager
func (m *Manager) Start(sessionID, profile, title, parentID string) (Run, error)
func (m *Manager) WrapSink(runID string, inner event.Sink) event.Sink
func (m *Manager) Finish(runID string, runErr error) (Run, error)
func (m *Manager) Get(runID string) (Run, error)
func (m *Manager) Events(runID string, afterSeq uint64) ([]event.Event, error)
```

Define `ErrRunNotFound` and `ErrInvalidTransition`. Return copies so callers cannot mutate Manager state.

- [ ] **Step 5: Verify GREEN under race**

```bash
go test -race ./internal/runstate -count=1
```

Expected: PASS with contiguous per-run sequences.

- [ ] **Step 6: Commit**

```bash
git add internal/event/event.go internal/runstate/manager.go internal/runstate/manager_test.go
git commit -m "feat(runtime): stamp ordered versioned run events"
```

### Task 3: Persist runs and events in lumenstore

**Files:**
- Modify: `internal/lumenstore/store.go`
- Create: `internal/lumenstore/run.go`
- Create: `internal/lumenstore/run_test.go`

- [ ] **Step 1: Write migration and CRUD tests**

Open a temporary DB and verify:

1. Create a `RunRecord` at version 1.
2. Update running -> succeeded with expected version 1.
3. A second update with expected version 1 returns `ErrVersionConflict`.
4. Append events with sequences 1 and 2.
5. `LoadRunEvents(id, 1)` returns only sequence 2.
6. Duplicate `(run_id, seq)` returns `ErrEventConflict` and does not overwrite payload.

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./internal/lumenstore -run 'Run|Event' -count=1 -v
```

Expected: build failure because run storage types/methods do not exist.

- [ ] **Step 3: Add tables**

Add migrations:

```sql
CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  session_id TEXT,
  parent_run_id TEXT,
  profile TEXT NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  stop_reason TEXT,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  version INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_runs_session_updated ON runs(session_id, updated_at DESC);
CREATE TABLE IF NOT EXISTS run_events (
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  created_at TEXT NOT NULL,
  payload TEXT NOT NULL,
  PRIMARY KEY(run_id, seq),
  UNIQUE(event_id)
);
```

- [ ] **Step 4: Implement storage-only records and optimistic update**

Define `RunRecord` using strings for timestamps/status to keep `lumenstore` independent from `runstate`. Implement:

```go
func (s *Store) CreateRun(rec RunRecord) error
func (s *Store) UpdateRun(rec RunRecord, expectedVersion uint64) error
func (s *Store) GetRun(id string) (RunRecord, error)
func (s *Store) AppendRunEvent(rec RunEventRecord) error
func (s *Store) LoadRunEvents(runID string, afterSeq uint64) ([]RunEventRecord, error)
```

`UpdateRun` uses `WHERE id=? AND version=?`; zero affected rows returns `ErrVersionConflict`.

- [ ] **Step 5: Verify GREEN and concurrent safety**

```bash
go test -race ./internal/lumenstore -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/lumenstore/store.go internal/lumenstore/run.go internal/lumenstore/run_test.go
git commit -m "feat(store): persist runs and ordered events"
```

### Task 4: Connect Manager to SQLite and replay after restart

**Files:**
- Modify: `internal/runstate/manager.go`
- Create: `internal/runstate/store.go`
- Modify: `internal/runstate/manager_test.go`

- [ ] **Step 1: Write restart/replay test**

Use a temporary `lumenstore.Store`, create Manager A, start/emit/finish a run, then create Manager B over the same store. Assert `Get` and `Events(afterSeq)` return the persisted run and events without relying on Manager A's maps.

- [ ] **Step 2: Run and verify RED**

```bash
go test ./internal/runstate -run 'Restart|Replay' -count=1 -v
```

Expected: FAIL because Manager only reads in-memory state.

- [ ] **Step 3: Add the storage adapter**

Implement `SQLiteStore` in `internal/runstate/store.go` against the Store interface created in Task 2 by converting between domain structs and `lumenstore.RunRecord`/`RunEventRecord`. JSON marshal/unmarshal the complete event payload.

- [ ] **Step 4: Make Manager write-through and read-through**

- Start: persist before exposing the Run.
- Event: append to persistence before forwarding to the UI; on persistence failure, forward one non-persisted LevelErr notice directly to the inner sink (never back through the stamping sink) and keep the original event in memory.
- Finish: optimistic update using the current version.
- Get/Events: use memory first, then store; merge without duplicating sequence numbers.

- [ ] **Step 5: Verify GREEN**

```bash
go test -race ./internal/runstate ./internal/lumenstore -count=1
```

Expected: PASS including restart replay.

- [ ] **Step 6: Commit**

```bash
git add internal/runstate/manager.go internal/runstate/store.go internal/runstate/manager_test.go
git commit -m "feat(runtime): persist and replay run lifecycle"
```

### Task 5: Integrate runs into HTTP chat

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_chat_test.go`
- Create: `internal/server/server_runs_test.go`

- [ ] **Step 1: Write chat lifecycle tests**

Inject a test Manager through `server.Config`. Assert:

- the first streamed typed event has a non-empty `run_id` and `seq:1`;
- success finishes the run as succeeded;
- max-step error finishes as exhausted with `stop_reason=max_steps`;
- configure failure before Agent start does not create a ghost running run;
- the final `stream_done` payload contains the same `run_id`.

- [ ] **Step 2: Run and verify RED**

```bash
go test ./internal/server -run 'RunLifecycle|RunID' -count=1 -v
```

Expected: FAIL because Server has no Run Manager.

- [ ] **Step 3: Add Manager ownership**

Extend Config/Server:

```go
type Config struct {
    // existing fields...
    Runs *runstate.Manager
}

type Server struct {
    // existing fields...
    runs *runstate.Manager
}
```

When Config.Runs is nil, build a Manager using `runstate.NewSQLiteStore(lumenstore.Default())`; a nil lumenstore produces an in-memory Manager.

- [ ] **Step 4: Wrap each chat turn**

Call `Controller.Configure` with the base `sseSink`. After Configure succeeds, derive the session ID from `Controller.Session().Path`, call `Start(sessionID, "code", promptSummary, "")`, wrap the base sink with `runs.WrapSink`, then call `Controller.SetSink(wrapped)` before `Plan` or `Run`. Use a 120-rune title cap. Always call `Finish` exactly once with the returned error. Configure failures occur before Start and therefore cannot leave a ghost run.

Change `sseSink.done` to accept `runID` and include it in `stream_done`.

- [ ] **Step 5: Verify chat lifecycle GREEN**

```bash
go test -race ./internal/server -run 'RunLifecycle|RunID|HandleChat' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/server_chat_test.go internal/server/server_runs_test.go
git commit -m "feat(server): track every chat as a durable run"
```

### Task 6: Add run query and event replay APIs

**Files:**
- Modify: `internal/server/server_api.go`
- Modify: `internal/server/server_runs_test.go`

- [ ] **Step 1: Write API contract tests**

Create a run with three events and assert:

```text
GET /v1/runs/{id}                  -> 200 {run:{...}}
GET /v1/runs/{id}/events           -> seq 1,2,3
GET /v1/runs/{id}/events?after=2   -> seq 3 only
GET /v1/runs/missing               -> 404
GET /v1/runs/{id}/events?after=x   -> 400
```

- [ ] **Step 2: Run and verify RED**

```bash
go test ./internal/server -run '^TestRunAPI' -count=1 -v
```

Expected: FAIL with 404 because routes do not exist.

- [ ] **Step 3: Implement exact-path routing**

Register `/v1/runs/` and parse only:

- one segment after `runs` for Run;
- exactly `/{id}/events` for events;
- non-negative base-10 `after`.

Reject empty IDs, `..`, encoded slashes and extra segments. Return `runstate.ErrRunNotFound` as 404 and other errors as 500.

- [ ] **Step 4: Verify GREEN and traversal rejection**

```bash
go test ./internal/server -run '^TestRunAPI' -count=3
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/server_api.go internal/server/server_runs_test.go
git commit -m "feat(server): expose run state and event replay"
```

### Task 7: Teach the Web UI to retain run identity and replay events

**Files:**
- Modify: `internal/server/static/assets/app.js`
- Modify: `internal/server/static_assets_test.go`

- [ ] **Step 1: Add static contract assertions**

Extend the static test to read `app.js` and require the literals `/v1/runs/`, `run_id`, and `after=`. Keep the Node syntax check.

- [ ] **Step 2: Run and verify RED**

```bash
go test ./internal/server -run '^TestEmbeddedAppJS' -count=1 -v
```

Expected: FAIL because replay code is absent.

- [ ] **Step 3: Capture run identity and last sequence**

During stream parsing:

```js
if (ev.run_id) currentRunId = ev.run_id;
if (Number.isInteger(ev.seq) && ev.seq > currentRunSeq) currentRunSeq = ev.seq;
```

Persist only `{runId,lastSeq}` in sessionStorage. Never persist API keys in this record.

- [ ] **Step 4: Add one bounded replay request**

On a network interruption with a known run ID, request:

```js
fetch(`${API_BASE}/v1/runs/${encodeURIComponent(currentRunId)}/events?after=${currentRunSeq}`)
```

Feed returned events through the same `applyRunEvent(ev)` function used by live SSE. Retry replay once; if it fails, show a next action instead of looping.

- [ ] **Step 5: Verify syntax and server package**

```bash
node --check internal/server/static/assets/app.js
go test ./internal/server -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/static/assets/app.js internal/server/static_assets_test.go
git commit -m "feat(web): retain run identity and replay missed events"
```

### Task 8: Run-state completion gate

**Files:**
- Modify only for a scoped, test-reproduced failure.

- [ ] **Step 1: Format and static checks**

```bash
gofmt -w internal/runstate internal/lumenstore/run.go internal/lumenstore/run_test.go \
  internal/event/event.go internal/server/server.go internal/server/server_api.go \
  internal/server/server_chat_test.go internal/server/server_runs_test.go internal/server/static_assets_test.go
node --check internal/server/static/assets/app.js
git diff --check
```

- [ ] **Step 2: Focused race and restart tests**

```bash
go test -race ./internal/runstate ./internal/lumenstore ./internal/server -count=1
go test ./internal/runstate -run 'Concurrent|Restart|Replay' -count=10
go test ./internal/server -run 'RunLifecycle|RunAPI|RunID' -count=10
```

Expected: PASS.

- [ ] **Step 3: Repository gates**

```bash
go vet ./...
go build ./...
go test ./... -count=1
make test-integration
```

Expected: PASS; no live service requests in default/integration gates.

- [ ] **Step 4: Inspect state and commits**

```bash
git status --short
git diff --stat main...HEAD
git log --oneline --decorate main..HEAD
```

Expected: clean worktree and only approved runtime/server/store changes.
