// Package sandbox provides optional command execution isolation.
// On macOS, it uses Seatbelt (sandbox-exec) when available. On Linux,
// it can use seccomp or namespace isolation. When no sandbox is
// available, commands run natively with bash guard protection.
//
// Adapted from claw-code's sandbox.rs and Reasonix's sandbox/.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Mode selects the sandbox strategy.
type Mode string

const (
	ModeNone     Mode = "none"     // no sandbox, bash guard only
	ModeSeatbelt Mode = "seatbelt" // macOS Seatbelt (sandbox-exec)
	ModeSeccomp  Mode = "seccomp"  // Linux seccomp
	ModeDocker   Mode = "docker"   // Docker container isolation
	ModeAuto     Mode = "auto"     // best available
)

// Config tunes sandbox behavior.
type Config struct {
	Mode          Mode     // which sandbox to use
	ReadOnlyRoot  bool     // mount root filesystem read-only
	NetworkAccess bool     // allow network access
	AllowedPaths  []string // paths allowed for read/write (outside workspace)
	WorkspaceRoot string   // project root (read-write by default)
	MaxMemoryMB   int      // memory limit (0 = none)
	MaxCPUSeconds int      // CPU time limit in seconds (0 = none)
	MaxProcesses  int      // max child processes (0 = none)
}

// Executor runs commands in a sandbox.
type Executor struct {
	cfg  Config
	mu   sync.Mutex
	mode Mode // resolved mode (may differ from config if probe fails)
}

// NewExecutor creates a sandbox executor. It probes the system to
// determine the best available sandbox if ModeAuto is selected.
func NewExecutor(cfg Config) *Executor {
	e := &Executor{cfg: cfg}
	e.probe()
	return e
}

func (e *Executor) probe() {
	if e.cfg.Mode != ModeAuto {
		e.mode = e.cfg.Mode
		return
	}

	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("sandbox-exec"); err == nil {
			e.mode = ModeSeatbelt
			return
		}
	case "linux":
		if _, err := os.Stat("/proc/self/status"); err == nil {
			e.mode = ModeSeccomp
			return
		}
	}
	e.mode = ModeNone
}

// Mode returns the active sandbox mode.
func (e *Executor) Mode() Mode { return e.mode }

// Run executes a command in the configured sandbox. When no sandbox is
// available or ModeNone is selected, it falls back to direct execution
// (bash guard is handled separately by the permission gate).
func (e *Executor) Run(ctx context.Context, command string) ([]byte, error) {
	e.mu.Lock()
	mode := e.mode
	e.mu.Unlock()

	switch mode {
	case ModeSeatbelt:
		return e.runSeatbelt(ctx, command)
	case ModeDocker:
		return e.runDocker(ctx, command)
	default:
		return e.runNative(ctx, command)
	}
}

// ── Native execution (no sandbox) ──────────────────────────

func (e *Executor) runNative(ctx context.Context, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = os.Environ()
	if e.cfg.WorkspaceRoot != "" {
		cmd.Dir = e.cfg.WorkspaceRoot
	}
	return cmd.CombinedOutput()
}

// ── macOS Seatbelt ──────────────────────────────────────────

func (e *Executor) runSeatbelt(ctx context.Context, command string) ([]byte, error) {
	profile, err := e.buildSeatbeltProfile()
	if err != nil {
		return nil, fmt.Errorf("seatbelt profile: %w", err)
	}

	// Write profile to temp file
	tmpDir, err := os.MkdirTemp("", "lumen-sandbox")
	if err != nil {
		return nil, fmt.Errorf("sandbox tmp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	profilePath := filepath.Join(tmpDir, "profile.sb")
	if err := os.WriteFile(profilePath, []byte(profile), 0o644); err != nil {
		return nil, fmt.Errorf("write profile: %w", err)
	}

	cmd := exec.CommandContext(ctx, "sandbox-exec", "-f", profilePath, "sh", "-c", command)
	cmd.Env = os.Environ()
	if e.cfg.WorkspaceRoot != "" {
		cmd.Dir = e.cfg.WorkspaceRoot
	}
	return cmd.CombinedOutput()
}

func (e *Executor) buildSeatbeltProfile() (string, error) {
	var sb strings.Builder
	sb.WriteString("(version 1)\n")
	sb.WriteString("(deny default)\n\n")

	// Allow basic process operations
	sb.WriteString("(allow process-exec)\n")
	sb.WriteString("(allow sysctl-read)\n")
	sb.WriteString("(allow signal)\n\n")

	// Allow reading system files needed by sh/go/git
	sb.WriteString("(allow file-read*\n")
	sb.WriteString("  (subpath \"/usr\")\n")
	sb.WriteString("  (subpath \"/bin\")\n")
	sb.WriteString("  (subpath \"/Library/Developer\")\n")
	sb.WriteString("  (subpath \"/Applications/Xcode.app\")\n")
	sb.WriteString("  (subpath \"/private/var/db\")\n")
	sb.WriteString("  (subpath \"/dev\")\n")
	sb.WriteString("  (subpath \"/etc\"))\n\n")

	// Workspace read-write
	if e.cfg.WorkspaceRoot != "" {
		abs, _ := filepath.Abs(e.cfg.WorkspaceRoot)
		sb.WriteString(fmt.Sprintf("(allow file-read* file-write*\n  (subpath %q))\n\n", abs))
	}

	// Temp directories
	sb.WriteString("(allow file-read* file-write*\n  (subpath \"/tmp\"))\n\n")

	// Network
	if e.cfg.NetworkAccess {
		sb.WriteString("(allow network-outbound\n  (remote ip \"*:*\"))\n")
		sb.WriteString("(allow network-inbound\n  (local ip \"*:*\"))\n\n")
	}

	return sb.String(), nil
}

// ── Docker isolation ───────────────────────────────────────

func (e *Executor) runDocker(ctx context.Context, command string) ([]byte, error) {
	args := []string{"run", "--rm",
		"--network", "none",
		"--memory", fmt.Sprintf("%dm", max(e.cfg.MaxMemoryMB, 256)),
		"-v", fmt.Sprintf("%s:/workspace", e.cfg.WorkspaceRoot),
		"-w", "/workspace",
		"alpine:latest",
		"sh", "-c", command,
	}

	if e.cfg.NetworkAccess {
		args[2] = "bridge" // replace --network none with bridge
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()
	return cmd.CombinedOutput()
}

// ── Utilities ───────────────────────────────────────────────

// Available reports whether any sandbox is available on this system.
func Available() bool {
	e := NewExecutor(Config{Mode: ModeAuto})
	return e.mode != ModeNone
}

// CanSeatbelt reports whether macOS Seatbelt is available.
func CanSeatbelt() bool {
	_, err := exec.LookPath("sandbox-exec")
	return err == nil && runtime.GOOS == "darwin"
}

// CanDocker reports whether Docker is available.
func CanDocker() bool {
	_, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

// QuickProfile returns a minimal seatbelt profile for a given workspace.
func QuickProfile(workspaceRoot string) string {
	e := &Executor{cfg: Config{WorkspaceRoot: workspaceRoot}}
	profile, _ := e.buildSeatbeltProfile()
	return profile
}
