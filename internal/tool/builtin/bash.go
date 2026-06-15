package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&BashTool{})
}

// BashTool executes shell commands with a configurable timeout.
type BashTool struct{}

func (t *BashTool) Name() string   { return "bash" }
func (t *BashTool) ReadOnly() bool { return false }

func (t *BashTool) Description() string {
	return "Execute a command in the shell and return combined stdout/stderr. Use for builds, tests, git, package managers, etc. To search/read/list/edit files, prefer the dedicated tools (grep, read_file, ls, glob, edit_file) over shell grep/cat/ls/find/sed — they behave identically on every OS. For symbol search, call graphs, or architecture questions, use codegraph tools instead of grep."
}

func (t *BashTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "command":{"type":"string","description":"Shell command to execute"},
  "run_in_background":{"type":"boolean","description":"Run detached: returns a job id immediately and keeps running across turns. Read new output with bash_output, wait for it with wait, stop it with kill_shell."}
},
"required":["command"]
}`)
}

func (t *BashTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command         string `json:"command"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)
	out, err := cmd.CombinedOutput()

	if ctx.Err() != nil {
		if len(out) > 0 {
			return fmt.Sprintf("%s\n\n[command timed out after %v]", string(out), timeout), nil
		}
		return "", fmt.Errorf("command timed out after %v", timeout)
	}

	if err != nil {
		return fmt.Sprintf("%s\n\nexit code: %v", string(out), err), nil
	}
	return strings.TrimRight(string(out), "\n"), nil
}
