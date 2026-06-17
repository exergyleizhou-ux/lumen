// Package sandbox provides Docker-based isolated code execution.
// It wraps `docker run` with safe defaults: read-only root, no network,
// memory/CPU limits, and a hard timeout. Uses the Docker CLI directly
// (no SDK dependency) for maximum portability.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runtime describes an available execution environment.
type Runtime struct {
	Name    string // "python3", "node", "go", "bash"
	Image   string // Docker image
	Ext     string // file extension for temp files
	Command string // command to run the file inside the container
}

// Built-in runtimes.
var Runtimes = []Runtime{
	{Name: "python3", Image: "python:3.12-slim", Ext: ".py", Command: "python /code/code.py"},
	{Name: "node", Image: "node:22-slim", Ext: ".js", Command: "node /code/code.js"},
	{Name: "bash", Image: "ubuntu:24.04", Ext: ".sh", Command: "bash /code/code.sh"},
	{Name: "go", Image: "golang:1.23-alpine", Ext: ".go", Command: "go run /code/code.go"},
}

// Config controls sandbox execution.
type Config struct {
	Runtime  string        // runtime name, e.g. "python3"
	Code     string        // source code to execute
	Timeout  time.Duration // max execution time (default 30s)
	MemoryMB int           // memory limit in MB (default 256)
	Network  bool          // allow network access (default false)
	Env      map[string]string
}

// Result is the output of a sandbox execution.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
	TimedOut bool   `json:"timed_out"`
}

// Exec runs code in an isolated Docker container.
func Exec(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MemoryMB <= 0 {
		cfg.MemoryMB = 256
	}

	rt, ok := lookupRuntime(cfg.Runtime)
	if !ok {
		return nil, fmt.Errorf("sandbox: unknown runtime %q (available: %s)",
			cfg.Runtime, availableRuntimes())
	}

	// Create a temp directory for the code
	dir, err := os.MkdirTemp("", "lumen-sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("sandbox: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	// Write code file
	codePath := filepath.Join(dir, "code"+rt.Ext)
	if err := os.WriteFile(codePath, []byte(cfg.Code), 0o644); err != nil {
		return nil, fmt.Errorf("sandbox: write code: %w", err)
	}

	// Pull image if not present (best-effort)
	pullCtx, pullCancel := context.WithTimeout(ctx, 30*time.Second)
	pullImage(pullCtx, rt.Image)
	pullCancel()

	// Build docker run command
	args := []string{
		"run",
		"--rm",
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=64M",
		"--memory", fmt.Sprintf("%dm", cfg.MemoryMB),
		"--cpus", "1",
		"--pids-limit", "50",
		"--security-opt", "no-new-privileges",
		"--cap-drop", "ALL",
		"-v", dir + ":/code:ro",
	}

	if !cfg.Network {
		args = append(args, "--network", "none")
	}

	// Environment variables
	for k, v := range cfg.Env {
		args = append(args, "-e", k+"="+v)
	}

	args = append(args, rt.Image)
	args = append(args, strings.Split(rt.Command, " ")...)

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(execCtx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	duration := time.Since(start)

	result := &Result{
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
		Duration: duration.Round(time.Millisecond).String(),
	}

	if execCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Stderr = "execution timed out after " + cfg.Timeout.String()
		return result, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Stderr = err.Error()
		}
	} else {
		result.ExitCode = 0
	}

	return result, nil
}

// Check verifies that Docker is available.
func Check() error {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("docker not available: %w (install Docker from https://docker.com)", err)
	}
	_ = out // Docker version confirmed
	return nil
}

// ── Helpers ──────────────────────────────────────────────────

func lookupRuntime(name string) (Runtime, bool) {
	for _, rt := range Runtimes {
		if rt.Name == name {
			return rt, true
		}
	}
	return Runtime{}, false
}

func availableRuntimes() string {
	names := make([]string, len(Runtimes))
	for i, rt := range Runtimes {
		names[i] = rt.Name
	}
	return strings.Join(names, ", ")
}

func pullImage(ctx context.Context, image string) {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	cmd.Stdout = nil // silent
	cmd.Stderr = nil
	cmd.Run() // best effort
}
