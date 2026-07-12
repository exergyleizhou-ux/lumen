# Lumen Runtime Correctness Gates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate false-success run endings and mode leakage, preserve completion reasons through Web/SSE, and split deterministic tests from integration/live probes so the shared runtime starts from an honest green baseline.

**Architecture:** Keep the existing Agent and Controller APIs, but introduce a typed exhaustion error and scope plan mode to one call. Preserve the existing event stream while making terminal status explicit. Treat subprocess-backed MCP tests as controlled integration tests and real network checks as live smoke tests.

**Tech Stack:** Go 1.23, existing `internal/agent`, `internal/control`, `internal/server`, native MCP subprocesses, Go build tags, GitHub Actions.

---

## File map

- Create `internal/agent/errors.go`: typed terminal errors and `errors.Is` sentinel.
- Modify `internal/agent/agent.go`: return exhaustion as an error.
- Modify `internal/agent/integration_test.go`: prove max-step exhaustion cannot be mistaken for success.
- Modify `internal/control/controller.go`: scope plan mode and reset it for execution.
- Create `internal/control/controller_mode_test.go`: regression tests for plan-to-run and run-to-plan transitions.
- Modify `internal/server/server.go`: emit full event fields and a final `{ok,error}` frame.
- Modify `internal/server/server_chat_test.go`: verify SSE preserves stop reason and failure state.
- Modify `internal/server/static/assets/app.js`: render incomplete/failed terminal states honestly.
- Modify `internal/science/native/token_gated_test.go`: mark subprocess fleet checks as integration tests.
- Modify `internal/science/native/auth_test.go`: keep auth policy assertions in the default unit suite.
- Modify `internal/science/native/chembl_live_test.go`: mark real ChEMBL access as a live test.
- Modify `internal/science/native/manager.go`: add context-controlled connection setup.
- Create `internal/science/native/manager_context_test.go`: deterministic timeout/cancel coverage.
- Modify `Makefile`: explicit unit, integration, and live targets.
- Modify `.github/workflows/ci.yml`: run deterministic race and controlled integration gates separately.

### Task 1: Make max-step exhaustion a typed failure

**Files:**
- Create: `internal/agent/errors.go`
- Modify: `internal/agent/agent.go:889-901`
- Test: `internal/agent/integration_test.go:250-272`

- [ ] **Step 1: Change the existing test to require a typed error**

Replace the final assertion in `TestMaxStepsEnforcement` with:

```go
err := ag.Run(context.Background(), "do forever")
if !errors.Is(err, ErrMaxStepsExhausted) {
    t.Fatalf("expected ErrMaxStepsExhausted, got %v", err)
}
var exhausted *MaxStepsError
if !errors.As(err, &exhausted) || exhausted.Limit != 2 {
    t.Fatalf("expected MaxStepsError{Limit:2}, got %#v", err)
}
```

Add `errors` to the test imports.

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
go test ./internal/agent -run '^TestMaxStepsEnforcement$' -count=1 -v
```

Expected: FAIL because the current implementation returns `nil`.

- [ ] **Step 3: Add the typed error**

Create `internal/agent/errors.go`:

```go
package agent

import (
    "errors"
    "fmt"
)

var ErrMaxStepsExhausted = errors.New("agent max steps exhausted")

type MaxStepsError struct {
    Limit int
}

func (e *MaxStepsError) Error() string {
    return fmt.Sprintf("%v: limit=%d", ErrMaxStepsExhausted, e.Limit)
}

func (e *MaxStepsError) Unwrap() error { return ErrMaxStepsExhausted }
```

- [ ] **Step 4: Return the error after emitting the terminal event**

Replace the `return nil` at the max-step exit in `Agent.Run` with:

```go
a.Sink().Emit(event.Event{
    Kind:       event.TurnDone,
    StopReason: "max_steps",
    Timestamp:  time.Now(),
})
return &MaxStepsError{Limit: a.maxSteps}
```

Keep the warning notice. Do not drop the session: the incomplete run is useful for diagnosis and retry.

- [ ] **Step 5: Verify focused and package tests**

Run:

```bash
go test ./internal/agent -run 'TestMaxStepsEnforcement|TestStormBreakerIntegration' -count=1 -v
go test ./internal/agent -count=1
```

Expected: PASS; max-step test observes the typed error.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/errors.go internal/agent/agent.go internal/agent/integration_test.go
git commit -m "fix(agent): report max-step exhaustion as failure"
```

### Task 2: Scope plan mode to one operation

**Files:**
- Modify: `internal/control/controller.go:380-430`
- Create: `internal/control/controller_mode_test.go`

- [ ] **Step 1: Write a mode-recording provider test**

Create `internal/control/controller_mode_test.go` with a provider that returns a final text chunk and a test-only agent. The two regression tests must assert:

```go
func TestPlanModeDoesNotLeakIntoRun(t *testing.T) {
    c := newModeTestController(t)
    if err := c.Plan(context.Background(), "inspect"); err != nil {
        t.Fatal(err)
    }
    if c.ag.IsPlanMode() {
        t.Fatal("plan mode must be restored after Plan returns")
    }
    if err := c.Run(context.Background(), "execute"); err != nil {
        t.Fatal(err)
    }
    if c.ag.IsPlanMode() {
        t.Fatal("Run must execute with plan mode disabled")
    }
}

func TestRunClearsPreviouslySetPlanMode(t *testing.T) {
    c := newModeTestController(t)
    c.ag.SetPlanMode(true)
    if err := c.Run(context.Background(), "execute"); err != nil {
        t.Fatal(err)
    }
    if c.ag.IsPlanMode() {
        t.Fatal("Run left plan mode enabled")
    }
}
```

The helper must initialize `Controller{prov: p, ag: agent.New(...)}` and call `storeSink(event.Discard)` so `Run` can emit safely.

- [ ] **Step 2: Run tests and verify the leak is reproduced**

Run:

```bash
go test ./internal/control -run 'TestPlanModeDoesNotLeakIntoRun|TestRunClearsPreviouslySetPlanMode' -count=1 -v
```

Expected: FAIL because `Plan` never restores and `Run` never clears `planMode`.

- [ ] **Step 3: Make Plan scoped and Run explicit**

At the start of `Controller.Run`, add:

```go
c.ag.SetPlanMode(false)
```

Change `Controller.Plan` to:

```go
func (c *Controller) Plan(ctx context.Context, prompt string) error {
    c.ag.SetPlanMode(true)
    defer c.ag.SetPlanMode(false)
    c.sink().Emit(event.Event{Kind: event.Phase, Text: c.prov.Name() + " · planning (read-only)"})
    err := c.ag.Run(ctx, prompt)
    if err != nil {
        c.emitError(err)
    }
    return err
}
```

- [ ] **Step 4: Verify control and Lab mode paths**

Run:

```bash
go test ./internal/control -count=1
go test ./internal/science/lab -run 'Approval|ControllerPool|TurnPool' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/control/controller.go internal/control/controller_mode_test.go
git commit -m "fix(control): isolate plan mode per operation"
```

### Task 3: Preserve honest completion through SSE and Web UI

**Files:**
- Modify: `internal/server/server.go:121-235`
- Modify: `internal/server/server_chat_test.go`
- Modify: `internal/server/static/assets/app.js:430-530`

- [ ] **Step 1: Add an SSE failure regression test**

Add a test controller/provider that exhausts one step, send a non-demo `/v1/chat` request, and assert the response contains:

```go
if !strings.Contains(out, `"stop_reason":"max_steps"`) {
    t.Fatalf("missing max_steps terminal reason:\n%s", out)
}
if !strings.Contains(out, `"ok":false`) {
    t.Fatalf("terminal frame must report failure:\n%s", out)
}
```

Also extend the demo test to assert `"ok":true`.

- [ ] **Step 2: Run the server tests and verify they fail**

Run:

```bash
go test ./internal/server -run 'TestHandleChat.*Completion|TestHandleChatDemo' -count=1 -v
```

Expected: FAIL because `sseSink.Emit` drops `stop_reason` and the final frame is always `{}`.

- [ ] **Step 3: Serialize the full typed event**

Replace the hand-selected map in `sseSink.Emit` with:

```go
func (s sseSink) Emit(e event.Event) {
    data, err := json.Marshal(e)
    if err != nil {
        s.emit("error", "encode event: "+err.Error())
        return
    }
    fmt.Fprintf(s.w, "data: %s\n\n", data)
    s.flusher.Flush()
}
```

This preserves `level`, `diff`, `perf`, and `stop_reason` without adding another protocol.

- [ ] **Step 4: Emit one authoritative terminal frame**

Track `runErr` in `handleChat`. Add one helper and call it exactly once on every exit path:

```go
func (s sseSink) done(err error) {
    terminal := map[string]any{"kind": "stream_done", "ok": err == nil}
    if err != nil {
        terminal["error"] = err.Error()
    }
    data, _ := json.Marshal(terminal)
    fmt.Fprintf(s.w, "event: done\ndata: %s\n\n", data)
    s.flusher.Flush()
}
```

Demo mode calls `sink.done(nil)`; configure and run errors pass the real error. Remove the three hand-written `event: done` frames so the browser receives exactly one authoritative terminal record.

- [ ] **Step 5: Make the browser show incomplete terminal state**

In `app.js`, keep `let terminalOK = null; let terminalError = "";`. Handle:

```js
case "stream_done":
  terminalOK = ev.ok === true;
  terminalError = ev.error || "";
  break;
```

After the read loop, if `terminalOK === false`, append `.msg-error` with `terminalError || "任务未完成"`. Do not replace it with `（无文本输出）` and do not mark the run visually complete.

- [ ] **Step 6: Verify server and static regression tests**

Run:

```bash
go test ./internal/server -count=1
go test ./internal/server -run 'TestHandleChat' -count=3
```

Expected: PASS with explicit successful and failed terminal frames.

- [ ] **Step 7: Commit**

```bash
git add internal/server/server.go internal/server/server_chat_test.go internal/server/static/assets/app.js
git commit -m "fix(server): expose truthful run completion over SSE"
```

### Task 4: Separate unit, subprocess integration, and live network tests

**Files:**
- Modify: `internal/science/native/token_gated_test.go`
- Modify: `internal/science/native/auth_test.go`
- Modify: `internal/science/native/chembl_live_test.go`

- [ ] **Step 1: Preserve auth checks in the unit suite**

Move `TestCheckAuthTokenGated` from `token_gated_test.go` into `auth_test.go`. Keep its assertions byte-for-byte so adding the integration build tag does not reduce policy coverage.

- [ ] **Step 2: Mark controlled native fleet tests as integration**

Add these first lines to `token_gated_test.go`:

```go
//go:build integration

package native
```

The file continues to build local MCP binaries and use `httptest`; it performs no real network access.

- [ ] **Step 3: Mark ChEMBL access as live**

Add these first lines to `chembl_live_test.go`:

```go
//go:build live

package native
```

Remove the `testing.Short()` branch because the build tag is now the explicit opt-in.

- [ ] **Step 4: Verify default tests contain no live ChEMBL test**

Run:

```bash
go test ./internal/science/native -list .
```

Expected: output includes `TestCheckAuthTokenGated` and excludes `TestChemblLiveOutput`, `TestC2DListAlgorithmsFleetWithToken`, and `TestOasisPreviewSchemaFleetWithToken`.

- [ ] **Step 5: Verify controlled integration tests**

Run:

```bash
go test -tags=integration -p=2 ./internal/science/native -run 'TestC2DListAlgorithmsFleetWithToken|TestOasisPreviewSchemaFleetWithToken' -count=3 -v
```

Expected: PASS without external network access.

- [ ] **Step 6: Verify live tests are opt-in**

Run:

```bash
go test -tags=live ./internal/science/native -list 'Live'
```

Expected: lists `TestChemblLiveOutput`. Do not require the real request to pass in the offline PR gate.

- [ ] **Step 7: Commit**

```bash
git add internal/science/native/auth_test.go internal/science/native/token_gated_test.go internal/science/native/chembl_live_test.go
git commit -m "test(science): separate native integration and live probes"
```

### Task 5: Make native MCP connection cancellation caller-controlled

**Files:**
- Modify: `internal/science/native/manager.go`
- Create: `internal/science/native/manager_context_test.go`
- Modify: `internal/science/native/token_gated_test.go`

- [ ] **Step 1: Write a canceled-connect test**

Create a test-only `FleetMember` using a helper process that never answers `initialize`, then assert:

```go
ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()
started := time.Now()
err := mgr.connectOne(ctx, member)
if !errors.Is(err, context.DeadlineExceeded) {
    t.Fatalf("expected deadline error, got %v", err)
}
if time.Since(started) > time.Second {
    t.Fatalf("connect ignored caller deadline")
}
```

The helper process must be launched from the test binary, following the existing `internal/mcplife/mock_stdio_server.go` re-exec pattern.

- [ ] **Step 2: Run the focused test and verify the current signature fails to compile**

Run:

```bash
go test ./internal/science/native -run '^TestConnectHonorsCallerDeadline$' -count=1 -v
```

Expected: FAIL because `connectOne` does not accept a context.

- [ ] **Step 3: Add context-aware methods**

Change the manager methods to:

```go
func (m *Manager) Connect(id string) error {
    ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
    defer cancel()
    return m.ConnectContext(ctx, id)
}

func (m *Manager) ConnectContext(ctx context.Context, id string) error {
    for _, mem := range ShippedFleet() {
        if mem.ID == id {
            return m.connectOne(ctx, mem)
        }
    }
    return fmt.Errorf("unknown or unshipped fleet member %q", id)
}
```

Update `ConnectAll` to create one timeout context per member. Change `connectOne` to select on `ctx.Done()` instead of `time.After`. On cancellation, close the client and return:

```go
return fmt.Errorf("connect %s: %w", mem.ID, ctx.Err())
```

- [ ] **Step 4: Make integration tests pass their existing 20-second context to ConnectContext**

Create the context before connection and call `mgr.ConnectContext(ctx, "c2d")` / `mgr.ConnectContext(ctx, "oasis")`. Use a fresh context for the subsequent tool call so initialization does not consume its entire deadline.

- [ ] **Step 5: Verify timeout and integration behavior**

Run:

```bash
go test ./internal/science/native -run '^TestConnectHonorsCallerDeadline$' -count=3
go test -tags=integration -p=2 ./internal/science/native -count=3
```

Expected: PASS; canceled initialization returns within one second and controlled fleet tests remain stable.

- [ ] **Step 6: Commit**

```bash
git add internal/science/native/manager.go internal/science/native/manager_context_test.go internal/science/native/token_gated_test.go
git commit -m "fix(science): make native fleet connection context-aware"
```

### Task 6: Encode the test taxonomy in Make and CI

**Files:**
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `scripts/test-science-all.sh`

- [ ] **Step 1: Add explicit Make targets**

Add to `.PHONY` and define:

```make
test-unit:
	go test ./...

test-integration:
	go test -tags=integration -p=2 -count=1 -timeout=180s ./internal/science/native

test-live:
	go test -tags=live -count=1 -timeout=120s ./internal/science/native -run 'Live'
```

Make `test` depend on `test-unit`. Keep `test-live` out of `check`.

- [ ] **Step 2: Make science-all run the controlled integration gate**

After `science-check`, add:

```bash
echo "▶ science-all: controlled native integration"
go test -tags=integration -p=2 -count=1 -timeout 180s ./internal/science/native
```

- [ ] **Step 3: Split CI steps**

Keep the race test as the deterministic unit gate and add:

```yaml
      - name: Test controlled native integration
        run: GOTOOLCHAIN=local go test -tags=integration -p=2 -count=1 -timeout=180s ./internal/science/native
```

Do not enable `live` in pull requests.

- [ ] **Step 4: Verify Make and shell syntax**

Run:

```bash
bash -n scripts/test-science-all.sh
make test-unit
make test-integration
```

Expected: both suites pass; unit suite makes no ChEMBL request.

- [ ] **Step 5: Commit**

```bash
git add Makefile .github/workflows/ci.yml scripts/test-science-all.sh
git commit -m "ci: split deterministic integration and live gates"
```

### Task 7: Final correctness gate

**Files:**
- Modify only if a verified failure requires a scoped correction.

- [ ] **Step 1: Format and diff validation**

Run:

```bash
gofmt -w internal/agent/errors.go internal/agent/agent.go internal/agent/integration_test.go \
  internal/control/controller.go internal/control/controller_mode_test.go \
  internal/server/server.go internal/server/server_chat_test.go \
  internal/science/native/auth_test.go internal/science/native/token_gated_test.go \
  internal/science/native/chembl_live_test.go internal/science/native/manager.go \
  internal/science/native/manager_context_test.go
git diff --check
```

Expected: no output from `git diff --check`.

- [ ] **Step 2: Run package gates**

Run:

```bash
go test ./internal/agent ./internal/control ./internal/server ./internal/science/native -count=1
go test -tags=integration -p=2 ./internal/science/native -count=3
go vet ./internal/agent ./internal/control ./internal/server ./internal/science/native
```

Expected: PASS.

- [ ] **Step 3: Run repository gates**

Run:

```bash
go build ./...
go test ./... -count=1
go test -race ./internal/agent ./internal/control ./internal/server ./internal/science/native -count=1
```

Expected: PASS with no real ChEMBL request in the default suite.

- [ ] **Step 4: Confirm terminal invariants from tests**

Run:

```bash
go test ./internal/agent -run '^TestMaxStepsEnforcement$' -count=10
go test ./internal/control -run 'PlanMode' -count=10
go test ./internal/server -run 'Completion' -count=10
```

Expected: all repetitions pass; max-step, mode transition, and SSE terminal state are deterministic.

- [ ] **Step 5: Review the final diff and commit any verification-only correction**

Run:

```bash
git status --short
git diff --stat main...HEAD
git log --oneline main..HEAD
```

Expected: only Phase 0 correctness/test-gate files plus the approved design/plan are present.
