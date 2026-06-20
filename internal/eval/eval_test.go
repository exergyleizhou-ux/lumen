package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScore(t *testing.T) {
	ws := t.TempDir()
	pass, _ := Score(context.Background(), ws, []string{"sh", "-c", "exit 0"})
	if !pass {
		t.Error("exit 0 should score as pass")
	}
	fail, out := Score(context.Background(), ws, []string{"sh", "-c", "echo boom 1>&2; exit 1"})
	if fail {
		t.Error("exit 1 should score as fail")
	}
	if out == "" {
		t.Error("failure should capture output for the report")
	}
	if none, _ := Score(context.Background(), ws, nil); none {
		t.Error("no check command must not score as pass")
	}
}

func TestSummarize(t *testing.T) {
	s := Summarize([]Result{
		{Task: "a", Passed: true, Turns: 3, CostUSD: 0.01},
		{Task: "b", Passed: false, Turns: 9, CostUSD: 0.02},
		{Task: "c", Passed: true, Turns: 5, CostUSD: 0.03},
	})
	if s.Total != 3 || s.Passed != 2 {
		t.Errorf("counts wrong: %+v", s)
	}
	if s.PassRate < 0.66 || s.PassRate > 0.67 {
		t.Errorf("pass rate = %v, want ~0.667", s.PassRate)
	}
	if s.MedianTurns != 5 {
		t.Errorf("median turns = %d, want 5", s.MedianTurns)
	}
	if s.TotalCostUSD < 0.059 || s.TotalCostUSD > 0.061 {
		t.Errorf("total cost = %v, want ~0.06", s.TotalCostUSD)
	}
}

func TestLoadTasks(t *testing.T) {
	root := t.TempDir()
	// a well-formed task
	td := filepath.Join(root, "fix-bug")
	os.MkdirAll(filepath.Join(td, "workspace"), 0o755)
	os.WriteFile(filepath.Join(td, "prompt.txt"), []byte("  fix the bug  \n"), 0o644)
	os.WriteFile(filepath.Join(td, "check.txt"), []byte("go vet ./...\n"), 0o644)
	// a non-task dir (no prompt.txt) is ignored
	os.MkdirAll(filepath.Join(root, "not-a-task"), 0o755)

	tasks, err := LoadTasks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	if got.Name != "fix-bug" || got.Prompt != "fix the bug" {
		t.Errorf("task fields wrong: %+v", got)
	}
	if len(got.Check) != 3 || got.Check[0] != "go" || got.Check[1] != "vet" {
		t.Errorf("check not parsed from check.txt: %v", got.Check)
	}
}

func TestLoadTasks_DefaultCheck(t *testing.T) {
	root := t.TempDir()
	td := filepath.Join(root, "t")
	os.MkdirAll(td, 0o755)
	os.WriteFile(filepath.Join(td, "prompt.txt"), []byte("do it"), 0o644)
	tasks, _ := LoadTasks(root)
	if len(tasks) != 1 || tasks[0].Check[0] != "go" || tasks[0].Check[2] != "./..." {
		t.Errorf("missing check.txt should default to `go test ./...`, got %v", tasks)
	}
}

func TestCopyDir(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("hi"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("yo"), 0o644)
	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(b) != "yo" {
		t.Errorf("nested file not copied, got %q", b)
	}
}
