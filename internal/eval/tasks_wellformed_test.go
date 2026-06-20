package eval

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestEvalTasksWellFormed guards the committed eval task set so a malformed
// fixture can't silently break the benchmark. Structural checks only (no
// subprocess) so it stays fast and never contends with a concurrent `go test`
// on the build cache: every task must load with a non-empty prompt, a check
// command, and a workspace dir containing at least one source file.
//
// (That each task starts BROKEN — a no-op scores 0 — is verified when the task
// is authored and by a live `lumen eval` run; re-running every task's `go test`
// here would make the suite slow and flaky under parallel builds.)
func TestEvalTasksWellFormed(t *testing.T) {
	root := filepath.Join("..", "..", "evals", "tasks")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("eval tasks dir not found (%v) — skipping", err)
	}
	tasks, err := LoadTasks(root)
	if err != nil {
		t.Fatalf("LoadTasks(%s): %v", root, err)
	}
	if len(tasks) == 0 {
		t.Fatalf("no eval tasks found under %s", root)
	}
	for _, task := range tasks {
		t.Run(task.Name, func(t *testing.T) {
			if task.Prompt == "" {
				t.Error("empty prompt")
			}
			if len(task.Check) == 0 {
				t.Error("no check command")
			}
			info, err := os.Stat(task.Workspace)
			if err != nil || !info.IsDir() {
				t.Fatalf("workspace dir missing: %v", err)
			}
			files := 0
			_ = filepath.WalkDir(task.Workspace, func(_ string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					files++
				}
				return nil
			})
			if files == 0 {
				t.Errorf("workspace %s has no files", task.Workspace)
			}
		})
	}
}
