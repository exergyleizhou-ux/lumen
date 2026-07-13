package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"lumen/internal/jobs"
	"lumen/internal/sandbox"
	runworkspace "lumen/internal/workspace"
)

// TestBashSandboxAutoByDefault: unset env uses auto — direct path when no backend.
func TestBashSandboxAutoByDefault(t *testing.T) {
	t.Setenv(sandbox.EnvBashSandbox, "")
	if sandbox.BashMode() != sandbox.ModeAuto {
		t.Fatalf("default mode want auto, got %v", sandbox.BashMode())
	}
	bt := &BashTool{}
	out, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hello-auto"}`))
	if err != nil {
		t.Fatalf("auto echo failed: %v", err)
	}
	if !strings.Contains(out, "hello-auto") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestBashUsesWorkspaceRootAndEnvironment(t *testing.T) {
	t.Setenv(sandbox.EnvBashSandbox, "off")
	type run struct {
		root   string
		marker string
	}
	runs := []run{{t.TempDir(), "alpha"}, {t.TempDir(), "beta"}}
	var wg sync.WaitGroup
	for _, tc := range runs {
		tc := tc
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws, err := runworkspace.NewLocal(tc.marker, tc.root, "", map[string]string{"RUN_MARKER": tc.marker})
			if err != nil {
				t.Errorf("workspace: %v", err)
				return
			}
			ctx := runworkspace.WithContext(context.Background(), ws)
			out, err := (&BashTool{}).Execute(ctx, json.RawMessage(`{"command":"pwd; printf ':%s' \"$RUN_MARKER\""}`))
			if err != nil {
				t.Errorf("bash %s: %v", tc.marker, err)
				return
			}
			if out != ws.Root+"\n:"+tc.marker {
				t.Errorf("bash %s crossed workspace: %q", tc.marker, out)
			}
		}()
	}
	wg.Wait()
}

func TestBackgroundBashPreservesWorkspaceContext(t *testing.T) {
	t.Setenv(sandbox.EnvBashSandbox, "off")
	ws, err := runworkspace.NewLocal("background", t.TempDir(), "", map[string]string{"RUN_MARKER": "background"})
	if err != nil {
		t.Fatal(err)
	}
	manager := jobs.NewManager()
	ctx := jobs.WithManager(runworkspace.WithContext(context.Background(), ws), manager)
	started, err := (&BashTool{}).Execute(ctx, json.RawMessage(`{"command":"pwd; printf ':%s' \"$RUN_MARKER\"","run_in_background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(started)
	if len(fields) < 4 {
		t.Fatalf("unexpected start response: %q", started)
	}
	waitArgs, _ := json.Marshal(map[string]any{"job_ids": []string{fields[3]}, "timeout_seconds": 2})
	out, err := (&WaitTool{}).Execute(ctx, waitArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ws.Root+"\n:background") {
		t.Fatalf("background bash lost workspace: %q", out)
	}
}

// TestBashSandboxExplicitOff runs directly even when a backend exists.
func TestBashSandboxExplicitOff(t *testing.T) {
	t.Setenv(sandbox.EnvBashSandbox, "off")
	if sandbox.BashMode() != sandbox.ModeOff {
		t.Fatal("off mode expected")
	}
	bt := &BashTool{}
	out, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hello-off"}`))
	if err != nil || !strings.Contains(out, "hello-off") {
		t.Fatalf("off mode failed: out=%q err=%v", out, err)
	}
}

// TestBashSandboxAutoUsesRunnerWhenAvailable verifies auto selects sandbox-exec/bwrap path.
func TestBashSandboxAutoUsesRunnerWhenAvailable(t *testing.T) {
	if sandbox.SelectRunner() == nil {
		t.Skip("no sandbox backend available")
	}
	t.Setenv(sandbox.EnvBashSandbox, "auto")
	cmd, err := bashCmd(context.Background(), "echo routed-via-sandbox")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := exec.LookPath(cmd.Path)
	if path == "" {
		path = cmd.Path
	}
	base := cmd.Args[0]
	if base == "sh" {
		t.Fatalf("auto mode should route through sandbox runner, got direct sh: %v", cmd.Args)
	}
	if !strings.Contains(base, "sandbox-exec") && !strings.Contains(path, "bwrap") && cmd.Path != "bwrap" {
		t.Fatalf("unexpected sandbox command: path=%q args=%v", cmd.Path, cmd.Args)
	}
}

// TestBashSandboxRequiredButUnavailable: when the sandbox is REQUIRED but no
// backend exists, the bash tool fails closed rather than silently running
// unsandboxed.
func TestBashSandboxRequiredButUnavailable(t *testing.T) {
	if sandbox.SelectRunner() != nil {
		t.Skip("a sandbox backend is available; fail-closed path not exercised here")
	}
	t.Setenv(sandbox.EnvBashSandbox, "required")
	bt := &BashTool{}
	_, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hi"}`))
	if err == nil {
		t.Error("required sandbox with no backend must fail closed, got nil error")
	}
}

// TestBashSandboxContainsWrites: end-to-end through the bash tool — with the
// sandbox required, a write under $HOME is contained.
func TestBashSandboxContainsWrites(t *testing.T) {
	if sandbox.SelectRunner() == nil {
		t.Skip("no sandbox backend available")
	}
	t.Setenv(sandbox.EnvBashSandbox, "required")
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	target := home + "/.lumen-bash-sbx-escape-test.txt"
	os.Remove(target)
	t.Cleanup(func() { os.Remove(target) })

	bt := &BashTool{}
	args, _ := json.Marshal(map[string]string{"command": "echo pwned > " + target})
	bt.Execute(context.Background(), json.RawMessage(args))
	if _, err := os.Stat(target); err == nil {
		t.Error("bash sandbox failed to contain a write to $HOME")
	}
}
