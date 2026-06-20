// Package eval is a minimal coding-quality harness: run a fixed set of real
// coding tasks end-to-end through the agent and score each by a deterministic
// check (compile + tests green). It turns "is Lumen good?" from an assertion
// into a number — pass-rate — and lets you compare models/providers and prove a
// change didn't regress quality. The agent-driving runner lives in cmd/lumen
// (it needs the controller); the loading, scoring, and aggregation here are pure
// and unit-tested without any model.
package eval

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Task is one eval: a prompt applied to a starting workspace, scored by Check
// (a command run in the post-edit workspace; exit 0 = pass).
type Task struct {
	Name      string
	Prompt    string
	Check     []string // default: go test ./...
	Workspace string   // dir holding the starting (broken) files
}

// Result is one task's outcome.
type Result struct {
	Task    string  `json:"task"`
	Passed  bool    `json:"passed"`
	Turns   int     `json:"turns"`
	CostUSD float64 `json:"cost_usd"`
	Seconds float64 `json:"seconds"`
	Err     string  `json:"err,omitempty"` // why it failed to run/score (not the same as a failed check)

	// Research instrumentation (failure-mode study). Zero values are fine for the
	// plain `lumen eval` path that doesn't populate them.
	FailureMode       FailureMode `json:"failure_mode,omitempty"`
	FirstPromptTokens int         `json:"first_prompt_tokens,omitempty"`
	ToolResultCount   int         `json:"tool_result_count"`
	FilesChanged      int         `json:"files_changed"`
	StopReason        string      `json:"stop_reason,omitempty"`
	Rho               float64     `json:"rho,omitempty"`

	// Self-describing cell coordinates (reproducibility — which model/config
	// produced this row). ServerContextWindow is the operator-asserted LM Studio
	// `-c` value (the harness can't read it from an OpenAI endpoint).
	Model               string `json:"model,omitempty"`
	ToolProfile         string `json:"tool_profile,omitempty"`
	ServerContextWindow int    `json:"server_context_window,omitempty"`
	Rep                 int    `json:"rep,omitempty"`
}

// ChangedNonTestFiles returns the non-test source files that differ between the
// original and post-run workspace — i.e. the files the agent actually edited.
// Used to separate F4 (wrong-edit: changed files but test still red) from F2
// (no-edit). Mirrors ProtectedTestsUnchanged but for non-_test.go files.
func ChangedNonTestFiles(origWorkspace, runWorkspace string) []string {
	var changed []string
	_ = filepath.WalkDir(runWorkspace, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, e := filepath.Rel(runWorkspace, path)
		if e != nil {
			return nil
		}
		runBytes, _ := os.ReadFile(path)
		origBytes, oerr := os.ReadFile(filepath.Join(origWorkspace, rel))
		if oerr != nil || !bytes.Equal(origBytes, runBytes) { // new file or modified
			changed = append(changed, rel)
		}
		return nil
	})
	sort.Strings(changed)
	return changed
}

// defaultCheck scores a task by building and testing the module.
var defaultCheck = []string{"go", "test", "./..."}

// LoadTasks reads every subdirectory of root that contains a prompt.txt. Each
// task's check command comes from check.txt (whitespace-split) or defaults to
// `go test ./...`; its starting files are the task's workspace/ subdir.
func LoadTasks(root string) ([]Task, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		promptBytes, err := os.ReadFile(filepath.Join(dir, "prompt.txt"))
		if err != nil {
			continue // not a task dir
		}
		check := defaultCheck
		if b, err := os.ReadFile(filepath.Join(dir, "check.txt")); err == nil {
			if f := strings.Fields(strings.TrimSpace(string(b))); len(f) > 0 {
				check = f
			}
		}
		tasks = append(tasks, Task{
			Name:      e.Name(),
			Prompt:    strings.TrimSpace(string(promptBytes)),
			Check:     check,
			Workspace: filepath.Join(dir, "workspace"),
		})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Name < tasks[j].Name })
	return tasks, nil
}

// Score runs check in workspace and reports whether it passed (exit 0) plus the
// trimmed output on failure (for the report).
func Score(ctx context.Context, workspace string, check []string) (bool, string) {
	if len(check) == 0 {
		return false, "no check command"
	}
	cmd := exec.CommandContext(ctx, check[0], check[1:]...)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(out))
	}
	return true, ""
}

// Summary aggregates a run.
type Summary struct {
	Total         int     `json:"total"`
	Passed        int     `json:"passed"`
	PassRate      float64 `json:"pass_rate"` // 0..1
	MedianTurns   int     `json:"median_turns"`
	MedianSeconds float64 `json:"median_seconds"` // wall-clock per task — the real metric for local models
	TotalCostUSD  float64 `json:"total_cost_usd"`
}

// Summarize computes the headline metrics over results.
func Summarize(rs []Result) Summary {
	s := Summary{Total: len(rs)}
	turns := make([]int, 0, len(rs))
	secs := make([]float64, 0, len(rs))
	for _, r := range rs {
		if r.Passed {
			s.Passed++
		}
		s.TotalCostUSD += r.CostUSD
		turns = append(turns, r.Turns)
		secs = append(secs, r.Seconds)
	}
	if s.Total > 0 {
		s.PassRate = float64(s.Passed) / float64(s.Total)
	}
	s.MedianTurns = median(turns)
	s.MedianSeconds = medianF(secs)
	return s
}

func medianF(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	return xs[len(xs)/2]
}

func median(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	sort.Ints(xs)
	return xs[len(xs)/2]
}

// ProtectedTestsUnchanged reports whether every *_test.go file present in the
// original task workspace still exists byte-for-byte identical in the post-run
// workspace. A coding task says "don't modify the tests"; this enforces it so a
// pass earned by editing or deleting a test assertion is caught and the task is
// failed instead of silently scored green. The returned slice names the offending
// files (relative paths), so the report can show why the run was rejected.
func ProtectedTestsUnchanged(origWorkspace, runWorkspace string) (bool, []string) {
	var changed []string
	_ = filepath.WalkDir(origWorkspace, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(origWorkspace, path)
		if err != nil {
			return nil
		}
		origBytes, _ := os.ReadFile(path)
		runBytes, rerr := os.ReadFile(filepath.Join(runWorkspace, rel))
		if rerr != nil || !bytes.Equal(origBytes, runBytes) {
			changed = append(changed, rel)
		}
		return nil
	})
	sort.Strings(changed)
	return len(changed) == 0, changed
}

// CopyDir recursively copies src into dst (used to give each task run a fresh,
// writable workspace so runs don't mutate the committed fixtures).
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
