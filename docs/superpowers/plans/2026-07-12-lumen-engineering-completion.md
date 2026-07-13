# Lumen Engineering Completion Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent Lumen Code from reporting a modified engineering task as successful unless its latest file changes were actually verified in the correct run workspace.

**Architecture:** Record a small per-turn completion state inside Agent, update it from typed tool effects and verify-after-edit results, and gate final success only when a recognized project verifier is active. Keep non-project writing tasks compatible, but persist honest verification events and classify incomplete/failed verification as explicit non-success stop reasons. Tighten `complete_step` evidence to use typed effects instead of `ReadOnly` guesses.

**Tech Stack:** Go 1.23, existing Agent loop, `editverify`, typed `tool.Effects`, versioned events, runstate terminal classification, TDD/race tests.

---

## File map

- Modify `internal/evidence/evidence.go`: receipt effect fields and exact file/command evidence checks.
- Modify `internal/evidence/evidence_test.go`: Bash cannot satisfy file-write evidence; file tools can.
- Modify `internal/agent/agent.go`: per-turn completion state, workspace-aware verification, final completion gate.
- Modify `internal/agent/errors.go`: typed verification-incomplete and verification-failed errors.
- Create `internal/agent/completion_policy_test.go`: pass/skip/fail/no-project completion cases.
- Modify `internal/runstate/types.go`: classify verification terminal errors.
- Modify `internal/runstate/types_test.go`: stop-reason/status coverage.
- Modify `internal/server/server_chat_test.go`: SSE and durable Run expose verification failure honestly.
- Modify `internal/server/static/assets/app.js`: actionable labels for verification stop reasons.

### Task 1: Make evidence receipts effect-aware

**Files:**
- Modify: `internal/evidence/evidence.go`
- Modify: `internal/evidence/evidence_test.go`
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Write failing receipt tests**

Record a successful Bash receipt with `RunsCommands=true` and assert a `files` evidence item is rejected. Record a successful write receipt with `WritesFiles=true` and assert the same item is accepted. Verification evidence must still require an exact successful Bash command.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/evidence -run 'Effects|WriterEvidence' -count=1 -v`

Expected: FAIL because Receipt has no typed effect fields and still treats all non-read-only calls as writers.

- [ ] **Step 3: Add effect fields**

Add `WritesFiles`, `RunsCommands`, `UsesNetwork`, and `StartsCompute` to Receipt. Change `ReceiptFromToolCall` to receive `tool.Effects`, and make `VerifyEvidence` require `WritesFiles` for file/diff evidence. Agent passes the already-computed effects from `executeOne`.

- [ ] **Step 4: Verify GREEN**

Run: `go test -race ./internal/evidence ./internal/agent -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/evidence internal/agent/agent.go
git commit -m "fix(evidence): validate completion with typed tool effects"
```

### Task 2: Track engineering verification state per turn

**Files:**
- Modify: `internal/agent/agent.go`
- Create: `internal/agent/completion_policy_test.go`

- [ ] **Step 1: Write failing state tests**

Use a fake verifier to cover: a write followed by `Result{OK:true,Ran:1}` is verified; `OK:true,Ran:0` is skipped; a failed result is failed; a second write invalidates an earlier pass. Assert verification uses the root from `workspace.Context`, not process CWD.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/agent -run 'EngineeringCompletion|VerificationUsesWorkspace' -count=1 -v`

- [ ] **Step 3: Implement turn state**

Add an internal state with `wroteFiles`, `verificationRequired`, `verificationRan`, `verificationPassed`, `verificationFailed`, and changed paths. Reset it at the start of every Agent Run. File writes invalidate the prior pass. `verifyAfterEdits` resolves membership from `workspace.FromContext(ctx).Root`; only legacy calls fall back to CWD.

- [ ] **Step 4: Update state for every verify outcome**

`Ran>0 && OK` marks passed; `Ran==0`, timeout, missing verifier or disabled verification marks incomplete; a failing result marks failed. Existing VerifyStarted/VerifyResult events remain the durable evidence stream.

- [ ] **Step 5: Verify GREEN**

Run: `go test -race ./internal/agent -run 'EngineeringCompletion|VerificationUsesWorkspace' -count=1`

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agent.go internal/agent/completion_policy_test.go
git commit -m "feat(agent): track engineering verification completion"
```

### Task 3: Gate final success in recognized engineering projects

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/errors.go`
- Modify: `internal/agent/completion_policy_test.go`

- [ ] **Step 1: Write failing end-to-end Agent tests**

Drive the mock provider through write tool -> final answer. With a verifier that ran and passed, Run returns nil. With Ran=0, Run returns `ErrVerificationIncomplete`; with a failed/exhausted verifier, Run returns `ErrVerificationFailed`. A controller without an installed verifier remains compatible for plain non-project writing.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/agent -run 'FinalRequiresVerification' -count=1 -v`

- [ ] **Step 3: Add typed errors and final gate**

Define sentinels and typed errors supporting `errors.Is`. Immediately before emitting `TurnDone(finished)`, call `engineeringCompletionError`. Only enforce when an edit verifier is installed and at least one file write succeeded. Emit `TurnDone` with `verification_incomplete` or `verification_failed` and return the typed error; never append a successful final assistant message for that attempt.

- [ ] **Step 4: Verify GREEN and max-step compatibility**

Run: `go test -race ./internal/agent -run 'FinalRequiresVerification|MaxSteps|PlanMode' -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/agent
git commit -m "feat(agent): require verified engineering changes for success"
```

### Task 4: Preserve verification terminal reasons through Run/SSE

**Files:**
- Modify: `internal/runstate/types.go`
- Modify: `internal/runstate/types_test.go`
- Modify: `internal/server/server_chat_test.go`
- Modify: `internal/server/static/assets/app.js`

- [ ] **Step 1: Write failing classification and SSE tests**

Assert `ErrVerificationIncomplete` and `ErrVerificationFailed` classify as `failed` with distinct stop reasons. A server chat using the failing test controller must finish the durable Run as failed and emit `stream_done {ok:false,error,run_id}`.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/runstate ./internal/server -run 'VerificationTerminal|VerificationFailure' -count=1 -v`

- [ ] **Step 3: Implement terminal propagation**

Extend `ClassifyTerminal` using `errors.Is`. Keep status `failed`; use `verification_incomplete` and `verification_failed` as machine-readable stop reasons. The Web UI renders both as “修改未通过工程验证” and retains the Run ID/retry action.

- [ ] **Step 4: Verify GREEN**

Run: `go test -race ./internal/runstate ./internal/server -run 'VerificationTerminal|VerificationFailure|RunLifecycle' -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/runstate internal/server
git commit -m "feat(runtime): expose engineering verification failures"
```

### Task 5: Engineering completion gate

- [ ] **Step 1: Focused race tests**

```bash
go test -race ./internal/evidence ./internal/agent ./internal/runstate ./internal/server -count=1
go test ./internal/agent ./internal/server -run 'FinalRequiresVerification|VerificationFailure' -count=10
```

- [ ] **Step 2: Repository gates**

```bash
go vet ./...
go build ./...
go test ./... -count=1
make test-integration
```

- [ ] **Step 3: Final audit**

```bash
git status --short
git diff --check main...HEAD
```

Expected: clean worktree; a recognized engineering project cannot finish successfully after unverified or failing file changes; non-project writing remains compatible; verification reasons are durable and visible.
