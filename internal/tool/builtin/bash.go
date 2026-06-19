package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"lumen/internal/jobs"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&BashTool{})
	tool.RegisterBuiltin(&BashOutputTool{})
	tool.RegisterBuiltin(&WaitTool{})
	tool.RegisterBuiltin(&KillShellTool{})
}

// ── bash ───────────────────────────────────────────────────

// BashTool executes shell commands synchronously, or asynchronously
// when run_in_background is set. Background jobs return a job ID that
// can be queried with bash_output, waited on with wait, and killed
// with kill_shell.
type BashTool struct{}

func (t *BashTool) Name() string     { return "bash" }
func (t *BashTool) ReadOnly() bool   { return false }

func (t *BashTool) Description() string {
	return "Execute a command in the shell and return combined stdout/stderr. Use for builds, tests, git, package managers, etc. To search/read/list/edit files, prefer the dedicated tools (grep, read_file, ls, glob, edit_file) over shell grep/cat/ls/find/sed — they behave identically on every OS. For symbol search, call graphs, or architecture questions, use codegraph tools instead of grep."
}

func (t *BashTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "command":{"type":"string","description":"Shell command to execute"},
  "run_in_background":{"type":"boolean","description":"Run detached: returns a job id immediately and keeps running across turns. Read new output with bash_output, wait for it with wait, stop it with kill_shell. Use for long-running commands like servers, watchers, or builds you don't need to block on."}
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

	// ── Background mode: hand off to jobs manager ──
	if p.RunInBackground {
		jm := jobs.FromContext(ctx)
		if jm == nil {
			return "", fmt.Errorf("run_in_background: no jobs manager available")
		}
		label := p.Command
		if len(label) > 60 {
			label = cutRunes(label, 57) + "..."
		}
		job := jm.Start("bash", label, func(bgCtx context.Context) (string, error) {
			cmd := exec.CommandContext(bgCtx, "sh", "-c", p.Command)
			out, err := cmd.CombinedOutput()
			return string(out), err
		})
		return fmt.Sprintf("started background job: %s (%s)", job.ID, job.Label), nil
	}

	// ── Synchronous mode ──
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

// ── bash_output ────────────────────────────────────────────

// BashOutputTool reads new output from a background bash job.
type BashOutputTool struct{}

func (t *BashOutputTool) Name() string     { return "bash_output" }
func (t *BashOutputTool) ReadOnly() bool   { return true }

func (t *BashOutputTool) Description() string {
	return "Read new output from a background job started with bash(run_in_background=true) or task(run_in_background=true). Returns the output produced since the last bash_output call for that job, plus its status (running/done/failed/killed). Does not block."
}

func (t *BashOutputTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "job_id":{"type":"string","description":"The background job id (e.g. \"bash-1\") returned when it was started."},
  "filter":{"type":"string","description":"Optional regular expression; only matching lines of the new output are returned."}
},
"required":["job_id"]
}`)
}

func (t *BashOutputTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		JobID  string `json:"job_id"`
		Filter string `json:"filter"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.JobID == "" {
		return "", fmt.Errorf("job_id is required")
	}

	jm := jobs.FromContext(ctx)
	if jm == nil {
		return "", fmt.Errorf("no jobs manager: run_in_background jobs not available")
	}

	job := jm.Get(p.JobID)
	if job == nil {
		return "", fmt.Errorf("job %q not found", p.JobID)
	}

	status, result, errStr := job.Snapshot()
	out := ""
	if status != jobs.StatusRunning {
		out = result
	}
	if out == "" && status == jobs.StatusDone {
		return fmt.Sprintf("[job %s done]", p.JobID), nil
	}
	if out == "" && status == jobs.StatusFailed {
		return fmt.Sprintf("[job %s failed: %s]", p.JobID, errStr), nil
	}
	if out == "" {
		return fmt.Sprintf("[job %s still running]", p.JobID), nil
	}

	if p.Filter != "" {
		// Simple filter — if not matching the regex-like pattern, exclude
		filtered := filterLines(out, p.Filter)
		if filtered == "" {
			return fmt.Sprintf("[no lines matched filter %q]", p.Filter), nil
		}
		return filtered, nil
	}
	return out, nil
}

func filterLines(output, filter string) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, filter) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// ── wait ───────────────────────────────────────────────────

// WaitTool blocks until a background job finishes.
type WaitTool struct{}

func (t *WaitTool) Name() string     { return "wait" }
func (t *WaitTool) ReadOnly() bool   { return true }

func (t *WaitTool) Description() string {
	return "Block until background jobs finish, then return each job's status and final output/answer. Use to collect the result of a task(run_in_background) or bash(run_in_background) before continuing. Omit job_ids to wait for every running job."
}

func (t *WaitTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "job_ids":{"type":"array","items":{"type":"string"},"description":"Background job ids to wait for. Omit to wait for every currently-running job."},
  "timeout_seconds":{"type":"integer","description":"Optional maximum seconds to block before returning current progress. Omit to wait until the jobs finish."}
}
}`)
}

func (t *WaitTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		JobIDs         []string `json:"job_ids"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	jm := jobs.FromContext(ctx)
	if jm == nil {
		return "", fmt.Errorf("no jobs manager: run_in_background jobs not available")
	}

	timeout := time.Duration(p.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var sb strings.Builder
	if len(p.JobIDs) == 0 {
		// Wait for all running jobs
		results := jm.WaitAll(timeout)
		for id, res := range results {
			sb.WriteString(fmt.Sprintf("%s: %s\n", id, res))
		}
	} else {
		for _, id := range p.JobIDs {
			out, err := jm.Wait(waitCtx, id)
			if err != nil {
				sb.WriteString(fmt.Sprintf("%s: %v\n", id, err))
			} else {
				sb.WriteString(fmt.Sprintf("%s: %s\n", id, out))
			}
		}
	}
	if sb.Len() == 0 {
		return "no jobs to wait for", nil
	}
	return sb.String(), nil
}

// ── kill_shell ─────────────────────────────────────────────

// KillShellTool terminates a running background job.
type KillShellTool struct{}

func (t *KillShellTool) Name() string     { return "kill_shell" }
func (t *KillShellTool) ReadOnly() bool   { return false }

func (t *KillShellTool) Description() string {
	return "Terminate a running background job (bash or task) started with run_in_background. A no-op if the job has already finished or the id is unknown."
}

func (t *KillShellTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "job_id":{"type":"string","description":"The background job id to terminate (e.g. \"bash-1\")."}
},
"required":["job_id"]
}`)
}

func (t *KillShellTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.JobID == "" {
		return "", fmt.Errorf("job_id is required")
	}

	jm := jobs.FromContext(ctx)
	if jm == nil {
		return "", fmt.Errorf("no jobs manager: run_in_background jobs not available")
	}

	job := jm.Get(p.JobID)
	if job == nil {
		return fmt.Sprintf("job %q not found", p.JobID), nil
	}
	jm.Kill(p.JobID)
	return fmt.Sprintf("killed: %s", p.JobID), nil
}
