# Lumen Science Lab Shared Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every Lab chat a durable shared-Runtime Run with ordered replayable events, explicit cancellation, detached execution, and provenance records linked to the generating Run.

**Architecture:** Inject the existing `runstate.Manager` into Lab API, configure the project controller first, then start a science-profile Run and replace the controller sink with a provenance-preserving stamped sink. Reuse the detached-context/cancel pattern from Lumen Web. Extend provenance records with `run_id`; Lab routes expose Run state/events/cancel without inventing a second state machine.

**Tech Stack:** Go 1.23, existing `runstate`, SQLite adapter, Lab SSE/controller pool, provenance JSONL, browser JavaScript.

---

### Task 1: Link provenance to Run IDs

**Files:**
- Modify: `internal/science/lab/provenance/writer.go`
- Modify: `internal/science/lab/provenance/recorder.go`
- Modify: `internal/science/lab/provenance/recorder_test.go`
- Modify: `internal/science/lab/ctrl.go`

- [ ] **Step 1: Write failing provenance tests**

Set a Recorder Run ID, record an MCP call and an artifact, then decode both JSONL records and assert the same `run_id` is present. Change the Run ID and assert the next record uses the new value without mutating prior records.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/science/lab/provenance -run 'RunID' -count=1 -v`

- [ ] **Step 3: Implement Run binding**

Add `RunID string` to Record and `SetRunID(string)` to Recorder. Read session/model/run fields under the existing mutex before building each record. Extend `RecordWrite` to accept Run ID. Add Lab Controller methods `BindRun(runID string, sink event.Sink)` and `Workspace/Session` accessors; BindRun sets Recorder Run ID, wraps the new sink with provenance, and delegates to shared Controller.SetSink.

- [ ] **Step 4: Verify GREEN under race**

Run: `go test -race ./internal/science/lab/provenance ./internal/science/lab -run 'RunID|Provenance' -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/science/lab/provenance internal/science/lab/ctrl.go
git commit -m "feat(lab): link provenance records to runtime runs"
```

### Task 2: Give Lab API a shared Run manager

**Files:**
- Modify: `internal/science/lab/server.go`
- Modify: `internal/science/lab/api.go`
- Create: `internal/science/lab/run_api_test.go`

- [ ] **Step 1: Write failing API contract tests**

Inject an in-memory Manager, create a science Run with three events, and assert:

```text
GET  /api/lab/runs/{id}                 -> Run
GET  /api/lab/runs/{id}/events?after=2  -> seq 3
POST /api/lab/runs/{id}/cancel          -> 202 only while active
GET  missing                            -> 404
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/science/lab -run 'LabRunAPI' -count=1 -v`

- [ ] **Step 3: Inject Manager and add exact routes**

Add `Runs *runstate.Manager` to Lab Config. New API defaults to a Manager over the shared lumenstore SQLite adapter and accepts injected in-memory managers in tests. Register `/api/lab/runs/`; implement only exact Run/events/cancel paths with the same status semantics as Lumen Server.

- [ ] **Step 4: Add detached active-Run registry**

Add `activeRuns sync.Map`, `beginActiveRun`, and `cancelActiveRun` to API. Use `context.WithoutCancel`; explicit cancel owns termination.

- [ ] **Step 5: Verify GREEN**

Run: `go test -race ./internal/science/lab -run 'LabRunAPI|LabActiveRun' -count=1`

- [ ] **Step 6: Commit**

```bash
git add internal/science/lab/server.go internal/science/lab/api.go internal/science/lab/run_api_test.go
git commit -m "feat(lab): expose shared runtime run APIs"
```

### Task 3: Wrap every Lab chat in a durable Run

**Files:**
- Modify: `internal/science/lab/api.go`
- Modify: `internal/science/lab/history_sink.go`
- Modify: `internal/science/lab/productivity_test.go`
- Create: `internal/science/lab/chat_run_test.go`

- [ ] **Step 1: Write failing chat lifecycle tests**

Use a controlled provider to assert first typed event has `run_id/seq`, success/failure finishes the Manager, terminal SSE contains the same Run ID, request cancellation does not cancel execution, and explicit cancel produces canceled status. Configure failure creates no Run.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/science/lab -run 'LabChatRun|LabConfigureGhost' -count=1 -v`

- [ ] **Step 3: Integrate after Configure**

After Configure succeeds, call `runs.Start(sessionID,"science",title,"")`, create a detached active context, wrap `historySink` with `runs.WrapSink`, and call `ctrl.BindRun`. Finish exactly once with the returned error. Emit `stream_done` with `{ok,error,run_id}` while retaining the legacy `event: done` frame during migration.

- [ ] **Step 4: Preserve controller pool safety**

Active Run cleanup occurs before releasing the project controller. A second turn for the same project remains conflict/queued per existing pool rules; different projects remain concurrent and isolated.

- [ ] **Step 5: Verify GREEN and repetition**

Run: `go test -race ./internal/science/lab -run 'LabChatRun|LabConfigureGhost|ProcessWorkspaceOrPath' -count=10`

- [ ] **Step 6: Commit**

```bash
git add internal/science/lab
git commit -m "feat(lab): execute chats as durable science runs"
```

### Task 4: Restore/cancel Lab Runs in the UI

**Files:**
- Modify: `internal/science/lab/static/app.js`
- Modify: `internal/science/lab/static_helpers_test.go`
- Modify: `internal/science/lab/static/labui_test.mjs`

- [ ] **Step 1: Add failing UI contracts**

Require storage of `{runId,lastSeq}`, `/api/lab/runs/.../events?after=`, explicit `/cancel`, terminal cleanup, and reload restoration. Add a DOM test that replay does not duplicate an already-rendered sequence.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/science/lab -run 'Static|RunReplay' -count=1 -v`

- [ ] **Step 3: Implement shared behavior**

Capture run metadata from SSE, restore on init, poll while non-terminal, and cancel by Run ID before aborting the local stream. Render failed/exhausted/canceled science Runs with provenance and retry actions.

- [ ] **Step 4: Verify GREEN**

```bash
node --check internal/science/lab/static/app.js
go test ./internal/science/lab -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/science/lab/static internal/science/lab/static_helpers_test.go
git commit -m "feat(lab-ui): restore durable science runs"
```

### Task 5: Science Run/provenance E2E gate

- [ ] **Step 1: Add controlled artifact E2E**

Drive `model -> write_file -> artifact provenance -> verification/final` in a temporary Lab project. Assert Run status, ordered events, artifact hash, session ID and Run ID all agree. Add a failure case that cannot become succeeded.

- [ ] **Step 2: Focused race gates**

```bash
go test -race ./internal/science/lab ./internal/science/lab/provenance ./internal/runstate -count=1
go test ./internal/science/lab -run 'ScienceRunE2E|LabChatRun|LabRunAPI' -count=10
```

- [ ] **Step 3: Repository gates**

```bash
go vet ./...
go build ./...
go test ./... -count=1
make test-integration
```

- [ ] **Step 4: Audit**

```bash
git status --short
git diff --check main...HEAD
```

Expected: Lab and Lumen use the same authoritative Run/event state machine; every recorded science artifact identifies its Run; disconnect/cancel/replay are deterministic.
