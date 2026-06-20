package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/sandbox"
)

// TestBashSandboxOffByDefault: with LUMEN_BASH_SANDBOX unset, the bash tool runs
// directly (no behavior change) — a plain command succeeds.
func TestBashSandboxOffByDefault(t *testing.T) {
	t.Setenv(sandbox.EnvBashSandbox, "")
	bt := &BashTool{}
	out, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hello-direct"}`))
	if err != nil {
		t.Fatalf("plain echo failed: %v", err)
	}
	if !strings.Contains(out, "hello-direct") {
		t.Errorf("unexpected output: %q", out)
	}
}

// TestBashSandboxRequiredButUnavailable: when the sandbox is REQUIRED but no
// backend exists, the bash tool fails closed rather than silently running
// unsandboxed. Simulated by forcing required mode on a platform without a
// backend; skipped where a backend IS available (it would run instead).
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
// sandbox required, a write under $HOME is contained. Skipped where no backend.
func TestBashSandboxContainsWrites(t *testing.T) {
	if sandbox.SelectRunner() == nil {
		t.Skip("no sandbox backend available")
	}
	t.Setenv(sandbox.EnvBashSandbox, "required")
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	target := filepath.Join(home, ".lumen-bash-sbx-escape-test.txt")
	os.Remove(target)
	t.Cleanup(func() { os.Remove(target) })

	bt := &BashTool{}
	args, _ := json.Marshal(map[string]string{"command": "echo pwned > " + target})
	bt.Execute(context.Background(), json.RawMessage(args)) // error expected/ignored
	if _, err := os.Stat(target); err == nil {
		t.Error("bash sandbox failed to contain a write to $HOME")
	}
}
