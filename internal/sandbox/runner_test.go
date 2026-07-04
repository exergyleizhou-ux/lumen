package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBashMode(t *testing.T) {
	cases := map[string]Mode{
		"":         ModeAuto,
		"auto":     ModeAuto,
		"0":        ModeOff,
		"false":    ModeOff,
		"off":      ModeOff,
		"1":        ModeRequired,
		"true":     ModeRequired,
		"on":       ModeRequired,
		"yes":      ModeRequired,
		"required": ModeRequired,
		"AUTO":     ModeAuto,
		"  on  ":   ModeRequired,
	}
	for in, want := range cases {
		t.Setenv(EnvBashSandbox, in)
		if got := BashMode(); got != want {
			t.Errorf("BashMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestForBash(t *testing.T) {
	// off → nil, not required
	t.Setenv(EnvBashSandbox, "off")
	if r, required := ForBash(); r != nil || required {
		t.Errorf("off mode: got (%v, %v), want (nil, false)", r, required)
	}
	// default (unset) → auto semantics
	t.Setenv(EnvBashSandbox, "")
	if _, required := ForBash(); required {
		t.Error("default auto mode should not be required")
	}
	// required → required=true regardless of availability
	t.Setenv(EnvBashSandbox, "1")
	if _, required := ForBash(); !required {
		t.Error("required mode should report required=true")
	}
	// auto → required=false
	t.Setenv(EnvBashSandbox, "auto")
	if _, required := ForBash(); required {
		t.Error("auto mode should report required=false")
	}
}

func TestBashNetwork(t *testing.T) {
	t.Setenv(EnvBashNetwork, "")
	if BashNetwork() {
		t.Error("network should default to false (deny) when the sandbox is on")
	}
	t.Setenv(EnvBashNetwork, "1")
	if !BashNetwork() {
		t.Error("LUMEN_BASH_SANDBOX_NET=1 should allow network")
	}
}

func TestSeatbeltProfile(t *testing.T) {
	// Network denied by default.
	p := seatbeltProfile(RunOptions{Workdir: "/work/dir"})
	if !strings.Contains(p, "(deny network*)") {
		t.Error("default profile must deny network")
	}
	if !strings.Contains(p, "(deny file-write*)") {
		t.Error("profile must deny writes by default")
	}
	if !strings.Contains(p, `(subpath "/work/dir")`) {
		t.Error("profile must re-allow writes to the workdir")
	}
	if !strings.Contains(p, `(subpath "/tmp")`) || !strings.Contains(p, `(subpath "/private/tmp")`) {
		t.Error("profile must allow temp writes")
	}
	// Network allowed when requested.
	p2 := seatbeltProfile(RunOptions{Network: true})
	if strings.Contains(p2, "(deny network*)") {
		t.Error("network-allowed profile must not deny network")
	}
	// Extra writable paths are included.
	p3 := seatbeltProfile(RunOptions{WritablePaths: []string{"/extra/path"}})
	if !strings.Contains(p3, `(subpath "/extra/path")`) {
		t.Error("extra writable paths must be included")
	}
}

func TestBwrapArgs(t *testing.T) {
	args := bwrapArgs("echo hi", RunOptions{Workdir: "/w", Network: false})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--ro-bind / /") {
		t.Error("bwrap must read-only-bind the root fs")
	}
	if !strings.Contains(joined, "--bind /w /w") || !strings.Contains(joined, "--chdir /w") {
		t.Error("bwrap must bind+chdir the workdir writable")
	}
	if !strings.Contains(joined, "--unshare-net") {
		t.Error("bwrap must unshare the network when network is denied")
	}
	if args[len(args)-3] != "sh" || args[len(args)-2] != "-c" || args[len(args)-1] != "echo hi" {
		t.Errorf("bwrap must end with sh -c <command>, got %v", args[len(args)-3:])
	}
	// Network allowed → no --unshare-net
	args2 := bwrapArgs("echo hi", RunOptions{Network: true})
	if strings.Contains(strings.Join(args2, " "), "--unshare-net") {
		t.Error("network-allowed bwrap must not unshare the network")
	}
}

func TestSelectRunnerMatchesPlatform(t *testing.T) {
	r := SelectRunner()
	if r == nil {
		t.Skip("no sandbox backend available on this platform")
	}
	switch runtime.GOOS {
	case "darwin":
		if r.Name() != "seatbelt" {
			t.Errorf("darwin runner = %q, want seatbelt", r.Name())
		}
	case "linux":
		if r.Name() != "bwrap" {
			t.Errorf("linux runner = %q, want bwrap", r.Name())
		}
	}
}

// TestSeatbeltContainment is a REAL integration test: it runs commands through
// sandbox-exec and verifies the filesystem confinement actually holds. Skipped
// where seatbelt is unavailable (non-mac / CI without it).
func TestSeatbeltContainment(t *testing.T) {
	r := &SeatbeltRunner{}
	if !r.Available() {
		t.Skip("sandbox-exec not available")
	}
	ctx := context.Background()

	// Real workdir, symlinks resolved (mac /tmp -> /private/tmp).
	work := t.TempDir()
	work, _ = filepath.EvalSymlinks(work)

	run := func(command string, opts RunOptions) (string, error) {
		cmd, err := r.Command(ctx, command, opts)
		if err != nil {
			return "", err
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// 1. A plain command works inside the sandbox.
	if out, err := run("echo sandbox-ok", RunOptions{Workdir: work}); err != nil || !strings.Contains(out, "sandbox-ok") {
		t.Fatalf("echo in sandbox failed: out=%q err=%v", out, err)
	}

	// 2. Writing inside the workdir is allowed.
	if _, err := run("echo data > "+filepath.Join(work, "inside.txt"), RunOptions{Workdir: work}); err != nil {
		t.Errorf("write inside workdir should be allowed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "inside.txt")); err != nil {
		t.Errorf("file inside workdir was not created: %v", err)
	}

	// 3. Writing OUTSIDE the confined set (workdir + temp) is denied. Use a path
	// directly under $HOME — the canonical persistence target (think ~/.ssh,
	// dotfiles) that the sandbox must keep read-only. t.TempDir() can't be used
	// here: it lives under the system temp root, which is intentionally writable.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory to test escape against")
	}
	target := filepath.Join(home, ".lumen-sandbox-escape-test.txt")
	os.Remove(target) // ensure clean slate
	t.Cleanup(func() { os.Remove(target) })
	if _, err := run("echo pwned > "+target, RunOptions{Workdir: work}); err == nil {
		t.Error("write under $HOME should be denied by the sandbox")
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("sandbox failed to contain the write — file escaped to $HOME")
	}
}
