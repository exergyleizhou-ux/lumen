package sandbox

// This file adds a command-sandbox Runner — a thin, capability-bounding wrapper
// around an arbitrary shell command, distinct from the Docker code-runner in
// sandbox.go. It lets the `bash` tool optionally run under OS-level isolation
// (mac seatbelt / Linux bubblewrap) so a denylist miss (docs/threat-model.md §6)
// is contained rather than catastrophic.
//
// Default OFF: nothing here runs unless the user opts in via LUMEN_BASH_SANDBOX,
// so existing behavior is unchanged. See docs/sandbox.md.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// RunOptions configures one sandboxed command execution.
type RunOptions struct {
	// Workdir is the working directory; it is made writable inside the sandbox
	// while the rest of the filesystem is read-only.
	Workdir string
	// Network allows network access. Default false (deny) — blocking the network
	// is the single most valuable containment (it defeats exfiltration and
	// download-and-execute), so callers must opt in.
	Network bool
	// WritablePaths are additional absolute paths allowed to be written, beyond
	// Workdir and the system temp dirs.
	WritablePaths []string
}

// Runner wraps a shell command into an isolated *exec.Cmd. Returning a *exec.Cmd
// (rather than running it) keeps output/timeout handling with the caller (the
// bash tool), so isolation is purely additive to the existing execution path.
type Runner interface {
	Name() string
	Available() bool
	Command(ctx context.Context, command string, opts RunOptions) (*exec.Cmd, error)
}

// SelectRunner returns the best available command-sandbox runner for this OS, or
// nil when none is available.
func SelectRunner() Runner {
	switch runtime.GOOS {
	case "darwin":
		if r := (&SeatbeltRunner{}); r.Available() {
			return r
		}
	case "linux":
		if r := (&BwrapRunner{}); r.Available() {
			return r
		}
	}
	return nil
}

// ── Env-driven enablement (avoids touching the typed config, which S2 owns) ──

const (
	// EnvBashSandbox controls whether the bash tool runs under a sandbox.
	//   unset / 0 / false / off → off (current behavior, no isolation)
	//   1 / true / on / yes / required → REQUIRED (fail closed if no backend)
	//   auto → use a sandbox if one is available, else run directly
	EnvBashSandbox = "LUMEN_BASH_SANDBOX"
	// EnvBashNetwork allows network access inside the bash sandbox. When the
	// sandbox is on, network is DENIED by default; set this to 1/true to allow.
	EnvBashNetwork = "LUMEN_BASH_SANDBOX_NET"
)

// Mode is the bash-sandbox enablement mode parsed from EnvBashSandbox.
type Mode int

const (
	ModeOff Mode = iota
	ModeAuto
	ModeRequired
)

// BashMode reports the configured bash-sandbox mode.
func BashMode() Mode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBashSandbox))) {
	case "1", "true", "on", "yes", "required":
		return ModeRequired
	case "auto":
		return ModeAuto
	default:
		return ModeOff
	}
}

// BashNetwork reports whether network is allowed inside the bash sandbox.
func BashNetwork() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBashNetwork))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// ForBash returns the runner the bash tool should use and whether a sandbox is
// required. In ModeRequired the caller MUST fail when the returned runner is nil
// (fail closed); in ModeAuto a nil runner means "run directly".
func ForBash() (r Runner, required bool) {
	switch BashMode() {
	case ModeRequired:
		return SelectRunner(), true
	case ModeAuto:
		return SelectRunner(), false
	default:
		return nil, false
	}
}

// ── macOS: sandbox-exec (Seatbelt) ──────────────────────────────────────────

// SeatbeltRunner isolates a command using macOS's sandbox-exec with a generated
// Seatbelt profile. Reads stay allowed (the guard already blocks sensitive
// reads; reads don't mutate); writes are confined to the workdir + temp, and the
// network is denied unless opted in.
type SeatbeltRunner struct{}

func (*SeatbeltRunner) Name() string { return "seatbelt" }

func (*SeatbeltRunner) Available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

// seatbeltProfile builds a Seatbelt policy. Seatbelt is last-match-wins, so the
// pattern is: allow-by-default (so sh, dyld, libs all load), then deny the
// high-harm capabilities, then re-allow the narrow writable set.
func seatbeltProfile(opts RunOptions) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")
	if !opts.Network {
		b.WriteString("(deny network*)\n")
	}
	b.WriteString("(deny file-write*)\n")
	writable := []string{
		"/tmp", "/private/tmp", "/private/var/folders", "/dev",
	}
	if opts.Workdir != "" {
		writable = append(writable, opts.Workdir)
	}
	writable = append(writable, opts.WritablePaths...)
	for _, p := range writable {
		fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n", p)
	}
	return b.String()
}

func (r *SeatbeltRunner) Command(ctx context.Context, command string, opts RunOptions) (*exec.Cmd, error) {
	if !r.Available() {
		return nil, fmt.Errorf("sandbox: sandbox-exec not available on this system")
	}
	// Resolve symlinks so the workdir path in the profile matches the real path
	// Seatbelt sees (mac /tmp -> /private/tmp, etc.). Best-effort.
	if opts.Workdir != "" {
		if real, err := filepath.EvalSymlinks(opts.Workdir); err == nil {
			opts.Workdir = real
		}
	}
	profile := seatbeltProfile(opts)
	cmd := exec.CommandContext(ctx, "sandbox-exec", "-p", profile, "sh", "-c", command)
	if opts.Workdir != "" {
		cmd.Dir = opts.Workdir
	}
	return cmd, nil
}

// ── Linux: bubblewrap (bwrap) ───────────────────────────────────────────────

// BwrapRunner isolates a command using bubblewrap: a read-only bind of the root
// filesystem, a writable bind of the workdir, a fresh tmpfs /tmp, and an
// unshared network namespace unless network is opted in.
type BwrapRunner struct{}

func (*BwrapRunner) Name() string { return "bwrap" }

func (*BwrapRunner) Available() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := exec.LookPath("bwrap")
	return err == nil
}

func bwrapArgs(command string, opts RunOptions) []string {
	args := []string{
		"--die-with-parent",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	}
	if opts.Workdir != "" {
		args = append(args, "--bind", opts.Workdir, opts.Workdir, "--chdir", opts.Workdir)
	}
	for _, p := range opts.WritablePaths {
		args = append(args, "--bind", p, p)
	}
	if !opts.Network {
		args = append(args, "--unshare-net")
	}
	args = append(args, "sh", "-c", command)
	return args
}

func (r *BwrapRunner) Command(ctx context.Context, command string, opts RunOptions) (*exec.Cmd, error) {
	if !r.Available() {
		return nil, fmt.Errorf("sandbox: bwrap not available on this system")
	}
	if opts.Workdir != "" {
		if real, err := filepath.EvalSymlinks(opts.Workdir); err == nil {
			opts.Workdir = real
		}
	}
	cmd := exec.CommandContext(ctx, "bwrap", bwrapArgs(command, opts)...)
	if opts.Workdir != "" {
		cmd.Dir = opts.Workdir
	}
	return cmd, nil
}
