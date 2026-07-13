# Lumen Per-Run Workspace and Typed Tool Effects Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every Lumen and Lab turn execute against an immutable per-run workspace and classify tool side effects precisely so concurrent projects cannot leak paths or environment and only file writes trigger edit verification.

**Architecture:** Add a shared `internal/workspace` context and local backend, then pass it through `context.Context` from `control.Controller` to every tool call. Preserve legacy entry points for CLI compatibility, but remove Lab's process-global workspace/PATH mutations. Add an optional typed-effects contract with a conservative compatibility fallback, and migrate the core engineering and notebook tools in this phase.

**Tech Stack:** Go 1.23, `context.Context`, `os/exec`, existing `fileutil`, `tool.Registry`, Agent verification loop, Go race detector.

---

## File map

- Create `internal/workspace/context.go`: immutable run workspace, context helpers, environment overlay, local path backend.
- Create `internal/workspace/context_test.go`: isolation, defensive-copy, traversal and symlink tests.
- Modify `internal/fileutil/fileutil.go`: context-aware resolve/read/write helpers while keeping legacy wrappers.
- Modify `internal/fileutil/fileutil_workspace_test.go`: prove two contexts resolve the same relative name into different roots.
- Modify `internal/tool/tool.go`: typed `Effects`, optional provider, compatibility classification, context-aware previews.
- Modify `internal/tool/tool_test.go`: effect classification and preview context tests.
- Modify `internal/agent/agent.go`: use `WritesFiles` rather than `!ReadOnly()` for preview and verification.
- Modify `internal/agent/agent_test.go`: command-only tools must not enter the edit verification loop.
- Modify `internal/tool/builtin/read_file.go`: shared workspace context for reads and explicit effects.
- Modify `internal/tool/builtin/write_file.go`: shared workspace context for writes/previews and explicit effects.
- Modify `internal/tool/builtin/edit_file.go`: shared workspace context for writes/previews and explicit effects.
- Modify `internal/tool/builtin/multi_edit.go`: shared workspace context for writes/previews and explicit effects.
- Modify `internal/tool/builtin/notebook_edit.go`: shared workspace context for notebook writes/previews and explicit effects.
- Modify `internal/tool/builtin/bash.go`: per-run root/environment for foreground and background commands and explicit effects.
- Modify `internal/tool/builtin/bash_sandbox_test.go`: parallel workspace/Env isolation tests.
- Modify `internal/jobs/manager.go`: `StartContext` that preserves run-scoped values while retaining cancellation.
- Modify `internal/jobs/manager_test.go`: background job context propagation test.
- Modify `internal/control/controller.go`: `ConfigureOptions`, workspace ownership and context injection for Run/Plan/Chat.
- Create `internal/control/controller_workspace_test.go`: controller run/plan context propagation tests.
- Modify `internal/science/lab/ctrl.go`: configure a shared workspace context instead of calling `os.Setenv`.
- Modify `internal/science/lab/runtime/conda.go`: return a PATH overlay instead of mutating process PATH.
- Modify `internal/science/lab/runtime/python_test.go`: PATH overlay behavior test.
- Modify `internal/science/lab/productivity_test.go`: two-project concurrent workspace isolation regression.

### Task 1: Define immutable per-run workspace context

**Files:**
- Create: `internal/workspace/context.go`
- Create: `internal/workspace/context_test.go`

- [ ] **Step 1: Write failing context isolation tests**

Test two temporary roots with the same relative file name, defensive copying of `Env`, rejection of `../escape`, and rejection of a symlink that resolves outside the root. The public contract is:

```go
type Backend interface {
    Root() string
    Resolve(path string, allowMissing bool) (string, error)
}

type Context struct {
    WorkspaceID string
    Root        string
    UserID      string
    Env         map[string]string
    Backend     Backend
}

func NewLocal(workspaceID, root, userID string, env map[string]string) (Context, error)
func WithContext(parent context.Context, ws Context) context.Context
func FromContext(ctx context.Context) (Context, bool)
func (ws Context) Environment(base []string) []string
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/workspace -count=1 -v`

Expected: build failure because the package does not exist.

- [ ] **Step 3: Implement context and local backend**

`NewLocal` canonicalizes an existing root with `filepath.EvalSymlinks`, rejects files, copies `Env`, and installs a backend. `Resolve` joins relative paths to the immutable root, resolves the longest existing ancestor for writes, and validates the final path with `filepath.Rel`; string-prefix checks are not sufficient. `WithContext` stores a defensive copy, and `FromContext` returns another copy.

- [ ] **Step 4: Verify GREEN under race**

Run: `go test -race ./internal/workspace -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/workspace
git commit -m "feat(runtime): add immutable per-run workspace context"
```

### Task 2: Add context-aware file operations

**Files:**
- Modify: `internal/fileutil/fileutil.go`
- Modify: `internal/fileutil/fileutil_workspace_test.go`

- [ ] **Step 1: Write failing parallel-root tests**

Create two `workspace.Context` values containing different `note.txt` files and run 32 goroutines alternating between them. Assert `SafeReadFileContext` always reads the matching root and `SafeWriteFileContext` never creates a file in the other root.

- [ ] **Step 2: Verify RED**

Run: `go test -race ./internal/fileutil -run 'Context|ParallelWorkspace' -count=1 -v`

Expected: build failure because the context-aware helpers do not exist.

- [ ] **Step 3: Add context-aware helpers**

Add:

```go
func ResolvePathContext(ctx context.Context, path string, allowMissing bool) (string, error)
func SafeReadFileContext(ctx context.Context, path string, offset, limit int) (string, int, int, error)
func SafeWriteFileContext(ctx context.Context, path string, content []byte) error
```

When a shared workspace exists, resolution must go through `ws.Backend.Resolve`. Without one, delegate to the existing legacy functions so current CLI/tests remain compatible. Factor resolved-path read/write internals so checks and atomic writes are not duplicated.

- [ ] **Step 4: Verify GREEN and legacy compatibility**

Run: `go test -race ./internal/fileutil -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/fileutil
git commit -m "feat(fileutil): resolve file operations from run workspace"
```

### Task 3: Introduce typed tool effects and correct Agent verification

**Files:**
- Modify: `internal/tool/tool.go`
- Modify: `internal/tool/tool_test.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/agent_test.go`

- [ ] **Step 1: Write failing effect and verification tests**

Define a test tool with `Effects() tool.Effects { return tool.Effects{RunsCommands: true} }` and assert a successful call does not set `toolOutcome.wroteFile` and never invokes a fake edit verifier. Add table tests for read, write and command effects.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/tool ./internal/agent -run 'Effects|CommandDoesNotVerify' -count=1 -v`

Expected: build failure because `Effects` and `EffectsOf` do not exist.

- [ ] **Step 3: Implement the typed contract**

Add the exact design fields:

```go
type Effects struct {
    ReadsFiles      bool
    WritesFiles     bool
    RunsCommands    bool
    UsesNetwork     bool
    SendsRemoteData bool
    StartsCompute   bool
    Publishes       bool
    MayCharge       bool
}

type EffectProvider interface { Effects() Effects }
```

`EffectsOf` uses explicit effects when available. The temporary compatibility fallback is `WritesFiles: !t.ReadOnly()` to preserve safety for unmigrated tools. Change Agent pre-edit preview, `wroteFile`, changed-path collection and repeat-write wording to use `effects.WritesFiles`; plan-mode and current permission-gate behavior remain compatible in this phase.

- [ ] **Step 4: Make previews context-aware**

Change `Previewer` to:

```go
type Previewer interface {
    Preview(ctx context.Context, args json.RawMessage) (diff.Change, error)
}
```

Change `PreviewChange` and the Agent call site to pass the same tool-call context used by `Execute`.

- [ ] **Step 5: Verify GREEN**

Run: `go test -race ./internal/tool ./internal/agent -count=1`

Expected: PASS and the command-only regression records zero edit verifications.

- [ ] **Step 6: Commit**

```bash
git add internal/tool internal/agent
git commit -m "feat(runtime): classify tool effects for verification"
```

### Task 4: Migrate core file, edit and notebook tools

**Files:**
- Modify: `internal/tool/builtin/read_file.go`
- Modify: `internal/tool/builtin/write_file.go`
- Modify: `internal/tool/builtin/edit_file.go`
- Modify: `internal/tool/builtin/multi_edit.go`
- Modify: `internal/tool/builtin/notebook_edit.go`
- Modify: corresponding tests in `internal/tool/builtin/*_test.go`

- [ ] **Step 1: Write failing cross-workspace tool tests**

Execute read/write/edit/multi-edit/notebook tools concurrently with two workspace contexts and identical relative paths. Assert each output and mutation stays under its context root. Preview each writer and assert `Change.Path` is the canonical path inside that root.

- [ ] **Step 2: Verify RED**

Run: `go test -race ./internal/tool/builtin -run 'WorkspaceContext|PreviewContext' -count=1 -v`

Expected: FAIL because tools still read `LUMEN_WORKSPACE_ROOT`/CWD and previews lack context.

- [ ] **Step 3: Migrate reads/writes/previews**

Use the context-aware fileutil functions in every Execute/Preview path. `writeNotebook` must accept `ctx`; it may not call `WorkspaceRoot()`. Declare explicit effects:

```go
func (*ReadFileTool) Effects() tool.Effects { return tool.Effects{ReadsFiles: true} }
func (*WriteFileTool) Effects() tool.Effects { return tool.Effects{WritesFiles: true} }
```

Edit, multi-edit, notebook-edit and delete-range declare both `ReadsFiles` and `WritesFiles`.

- [ ] **Step 4: Verify GREEN**

Run: `go test -race ./internal/tool/builtin -run 'WorkspaceContext|PreviewContext|Edit|Notebook|MultiEdit' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tool/builtin
git commit -m "feat(tools): isolate file operations by run workspace"
```

### Task 5: Isolate foreground and background Bash

**Files:**
- Modify: `internal/tool/builtin/bash.go`
- Modify: `internal/tool/builtin/bash_sandbox_test.go`
- Modify: `internal/jobs/manager.go`
- Modify: `internal/jobs/manager_test.go`

- [ ] **Step 1: Write failing command isolation tests**

Run `pwd` and `printf "$RUN_MARKER"` concurrently in two workspace contexts. Assert each command sees its own root and marker. Repeat with `run_in_background=true`, then `wait`, proving the detached job retained the originating context.

- [ ] **Step 2: Verify RED**

Run: `go test -race ./internal/tool/builtin ./internal/jobs -run 'Workspace|StartContext' -count=1 -v`

Expected: FAIL because Bash uses process CWD/environment and jobs start from `context.Background()`.

- [ ] **Step 3: Preserve run context in jobs**

Add:

```go
func (m *Manager) StartContext(parent context.Context, jobType, label string, fn func(context.Context) (string, error)) *Job
```

It derives a cancelable context from `context.WithoutCancel(parent)` so immutable run values survive after the foreground request while `Kill` still cancels the job. Keep `Start` as a compatibility wrapper over `context.Background()`.

- [ ] **Step 4: Apply workspace to Bash**

When a workspace is present, set `cmd.Dir`/sandbox `Workdir` to its root and build `cmd.Env` from `ws.Environment(os.Environ())` before secret scrubbing. Declare `Effects{RunsCommands:true}` for Bash and `Effects{RunsCommands:true}` for kill; output/wait remain read-only operational tools. Background Bash must call `StartContext(ctx, ...)`.

- [ ] **Step 5: Verify GREEN**

Run: `go test -race ./internal/tool/builtin ./internal/jobs -run 'Workspace|StartContext|Bash' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tool/builtin/bash.go internal/tool/builtin/bash_sandbox_test.go internal/jobs
git commit -m "feat(shell): bind commands to the run workspace"
```

### Task 6: Pass workspace through Controller and remove Lab globals

**Files:**
- Modify: `internal/control/controller.go`
- Create: `internal/control/controller_workspace_test.go`
- Modify: `internal/science/lab/ctrl.go`
- Modify: `internal/science/lab/runtime/conda.go`
- Modify: `internal/science/lab/runtime/python_test.go`
- Modify: `internal/science/lab/productivity_test.go`

- [ ] **Step 1: Write failing Controller and Lab tests**

Add two configured controllers with distinct roots and PATH markers. The provider test double invokes read/write/bash calls; assert every call observes its controller workspace. In Lab, configure two project controllers concurrently and assert `os.Getenv("LUMEN_WORKSPACE_ROOT")` and process `PATH` remain unchanged.

- [ ] **Step 2: Verify RED**

Run: `go test -race ./internal/control ./internal/science/lab ./internal/science/lab/runtime -run 'Workspace|ProcessEnvironment' -count=1 -v`

Expected: FAIL because Configure mutates process environment and Controller does not inject workspace context.

- [ ] **Step 3: Add ConfigureOptions**

Add:

```go
type ConfigureOptions struct {
    Workspace    workspace.Context
    ToolsProfile string
}

func (c *Controller) ConfigureWithOptions(sink event.Sink, asker agent.Asker, cfgPath string, opts ConfigureOptions) error
```

Existing `Configure` creates a local context from CWD and delegates for CLI compatibility. `ConfigureWithOptions` selects `opts.ToolsProfile` without reading `LUMEN_TOOLS_PROFILE`, builds skills/verifier from `opts.Workspace.Root`, stores the immutable context, and Run/Plan/Chat attach it before calling Agent.

- [ ] **Step 4: Replace Lab environment mutation**

Lab builds `workspace.NewLocal(slug, ws, "", env)` where `env["PATH"]` is returned by a new pure `LabPath(sciDir, basePATH string) string`. Remove `os.Setenv("LUMEN_WORKSPACE_ROOT", ...)`, `os.Setenv("LUMEN_TOOLS_PROFILE", ...)`, and `InjectLabPath` from Configure. Call `ConfigureWithOptions(..., ToolsProfile: defaultToolProfile)`.

- [ ] **Step 5: Verify GREEN under concurrent projects**

Run: `go test -race ./internal/control ./internal/science/lab ./internal/science/lab/runtime -run 'Workspace|ProcessEnvironment|Productivity' -count=1`

Expected: PASS with no process environment changes.

- [ ] **Step 6: Commit**

```bash
git add internal/control internal/science/lab
git commit -m "feat(lab): isolate project workspaces per controller"
```

### Task 7: Workspace/effects completion gate

**Files:**
- Modify only for a scoped, test-reproduced failure.

- [ ] **Step 1: Format and static checks**

```bash
gofmt -w internal/workspace internal/fileutil internal/tool internal/agent internal/jobs internal/control internal/science/lab
git diff --check
```

- [ ] **Step 2: Focused race gates**

```bash
go test -race ./internal/workspace ./internal/fileutil ./internal/tool ./internal/tool/builtin ./internal/agent ./internal/jobs ./internal/control ./internal/science/lab ./internal/science/lab/runtime -count=1
go test -race ./internal/tool/builtin ./internal/science/lab -run 'Workspace|ProcessEnvironment' -count=10
```

- [ ] **Step 3: Repository gates**

```bash
go vet ./...
go build ./...
go test ./... -count=1
make test-integration
```

- [ ] **Step 4: Inspect final state**

```bash
git status --short
git diff --check main...HEAD
git log --oneline --decorate main..HEAD
```

Expected: clean feature worktree; all workspace/effect tests and existing gates pass; Lab no longer writes workspace root, tool profile or PATH into process globals.
