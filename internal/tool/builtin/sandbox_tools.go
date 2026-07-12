package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"lumen/internal/sandbox"
	"lumen/internal/tool"
)

func init() {
	// Only register if Docker is available — tool will return clear error if not
	tool.RegisterBuiltin(&SandboxExecTool{})
}

// ── sandbox_exec tool ────────────────────────────────────────

type SandboxExecTool struct{}

func (t *SandboxExecTool) Name() string        { return "sandbox_exec" }
func (t *SandboxExecTool) Description() string { return "Execute code in an isolated Docker sandbox. Safe: no network, read-only root, memory/cpu/timeout limits. Supports python3, node, go, bash. Use for running untrusted code, testing snippets, or executing user-provided scripts." }
func (t *SandboxExecTool) ReadOnly() bool      { return false }

func (t *SandboxExecTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"runtime": {
				"type": "string",
				"enum": ["python3", "node", "go", "bash"],
				"description": "Execution runtime."
			},
			"code": {
				"type": "string",
				"description": "Source code to execute."
			},
			"timeout": {
				"type": "integer",
				"description": "Timeout in seconds (default 30, max 120)."
			},
			"network": {
				"type": "boolean",
				"description": "Allow network access? (default false for safety)."
			}
		},
		"required": ["runtime", "code"]
	}`)
}

func (t *SandboxExecTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var req struct {
		Runtime string `json:"runtime"`
		Code    string `json:"code"`
		Timeout int    `json:"timeout"`
		Network bool   `json:"network"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("sandbox_exec: %w", err)
	}

	if req.Code == "" {
		return "", fmt.Errorf("sandbox_exec: code is required")
	}

	// Check Docker
	if err := sandbox.Check(); err != nil {
		return fmt.Sprintf("⚠ Docker not available: %v\n\nInstall Docker: https://docs.docker.com/get-docker/", err), nil
	}

	timeout := 30 * time.Second
	if req.Timeout > 0 {
		if req.Timeout > 120 {
			req.Timeout = 120
		}
		timeout = time.Duration(req.Timeout) * time.Second
	}

	result, err := sandbox.Exec(ctx, sandbox.Config{
		Runtime:  req.Runtime,
		Code:     req.Code,
		Timeout:  timeout,
		Network:  req.Network,
		MemoryMB: 256,
	})

	if err != nil {
		return "", fmt.Errorf("sandbox_exec: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("● sandbox [%s] · %s", req.Runtime, result.Duration))

	if result.TimedOut {
		sb.WriteString(" · ⏱ TIMED OUT")
	}
	if result.ExitCode != 0 {
		sb.WriteString(fmt.Sprintf(" · exit=%d", result.ExitCode))
	}
	sb.WriteString("\n")

	if result.Stdout != "" {
		sb.WriteString("\nstdout:\n")
		sb.WriteString(result.Stdout)
		sb.WriteString("\n")
	}
	if result.Stderr != "" {
		sb.WriteString("\nstderr:\n")
		sb.WriteString(result.Stderr)
		sb.WriteString("\n")
	}
	if result.Stdout == "" && result.Stderr == "" {
		sb.WriteString("(no output)\n")
	}

	return sb.String(), nil
}
