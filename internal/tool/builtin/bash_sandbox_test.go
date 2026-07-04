package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"lumen/internal/sandbox"
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