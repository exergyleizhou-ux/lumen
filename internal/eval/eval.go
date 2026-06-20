// Package eval is a minimal coding-quality harness: run a fixed set of real
// coding tasks end-to-end through the agent and score each by a deterministic
// check (compile + tests green). It turns "is Lumen good?" from an assertion
// into a number — pass-rate — and lets you compare models/providers and prove a
// change didn't regress quality. The agent-driving runner lives in cmd/lumen
// (it needs the controller); the loading, scoring, and aggregation here are pure
// and unit-tested without any model.
package eval

import (
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
	Task    string
	Passed  bool
	Turns   int
	CostUSD float64
	Seconds float64
	Err     string // why it failed to run/score (not the same as a failed check)
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
	Total        int
	Passed       int
	PassRate     float64 // 0..1
	MedianTurns  int
	TotalCostUSD float64
}

// Summarize computes the headline metrics over results.
func Summarize(rs []Result) Summary {
	s := Summary{Total: len(rs)}
	turns := make([]int, 0, len(rs))
	for _, r := range rs {
		if r.Passed {
			s.Passed++
		}
		s.TotalCostUSD += r.CostUSD
		turns = append(turns, r.Turns)
	}
	if s.Total > 0 {
		s.PassRate = float64(s.Passed) / float64(s.Total)
	}
	s.MedianTurns = median(turns)
	return s
}

func median(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	sort.Ints(xs)
	return xs[len(xs)/2]
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
