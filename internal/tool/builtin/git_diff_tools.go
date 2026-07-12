// git_diff_tools.go — Git diff/log/blame tools powered by diffengine.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"lumen/internal/diffengine"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&GitDiffTool{})
	tool.RegisterBuiltin(&GitLogTool{})
	tool.RegisterBuiltin(&GitDiffFilesTool{})
}

type GitDiffTool struct{}

func (t *GitDiffTool) Name() string   { return "git_diff" }
func (t *GitDiffTool) ReadOnly() bool { return true }
func (t *GitDiffTool) Description() string {
	return "Show all working tree changes (staged + unstaged) as a unified diff. Like 'git diff HEAD'."
}
func (t *GitDiffTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *GitDiffTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "diff", "HEAD").CombinedOutput()
	if err != nil && len(out) == 0 {
		out2, _ := exec.CommandContext(ctx, "git", "diff").CombinedOutput()
		if len(out2) > 0 {
			return string(out2), nil
		}
		return "No changes to show.", nil
	}
	s := string(out)
	if strings.TrimSpace(s) == "" {
		return "Working tree clean — no changes.", nil
	}
	if len(s) > 8192 {
		s = cutRunes(s, 8192) + "\n… (truncated)"
	}
	return s, nil
}

type GitLogTool struct{}

func (t *GitLogTool) Name() string   { return "git_log" }
func (t *GitLogTool) ReadOnly() bool { return true }
func (t *GitLogTool) Description() string {
	return "Show commit history as compact one-liners: hash, author, subject."
}
func (t *GitLogTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","default":20}}}`)
}
func (t *GitLogTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Count int }
	json.Unmarshal(args, &p)
	if p.Count <= 0 {
		p.Count = 20
	}
	out, err := exec.CommandContext(ctx, "git", "log", fmt.Sprintf("-%d", p.Count), "--oneline").CombinedOutput()
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "No commits yet.", nil
	}
	return string(out), nil
}

type GitDiffFilesTool struct{}

func (t *GitDiffFilesTool) Name() string   { return "git_diff_files" }
func (t *GitDiffFilesTool) ReadOnly() bool { return true }
func (t *GitDiffFilesTool) Description() string {
	return "Semantic line diff between two arbitrary files using LCS diff engine. Shows additions, removals, and unchanged lines in unified format."
}
func (t *GitDiffFilesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"old":{"type":"string"},"new":{"type":"string"}},"required":["old","new"]}`)
}
func (t *GitDiffFilesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Old, New string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	oldData, err := os.ReadFile(p.Old)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Old, err)
	}
	newData, err := os.ReadFile(p.New)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.New, err)
	}
	eng := diffengine.NewEngine()
	result := eng.LineDiff(string(oldData), string(newData))
	result.OldFile = p.Old
	result.NewFile = p.New
	return diffengine.FormatDiff(result), nil
}
