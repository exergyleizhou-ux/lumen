package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvalBaseline6WellFormed(t *testing.T) {
	root := filepath.Join("..", "..", "evals", "baseline6")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("baseline6 dir not found (%v)", err)
	}
	tasks, err := LoadTasks(root)
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 6 {
		t.Fatalf("want 6 baseline tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		t.Run(task.Name, func(t *testing.T) {
			if task.Prompt == "" {
				t.Error("empty prompt")
			}
			if len(task.Check) == 0 {
				t.Error("no check command")
			}
		})
	}
}