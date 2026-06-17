package agent

import (
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/event"
)

func TestFaultRollbackCountsConsecutiveFailures(t *testing.T) {
	a := &Agent{
		faultRollback: map[string]int{},
	}
	changed := []string{"pkg/broken.go"}

	a.checkFaultRollback(changed)
	if a.faultRollback["pkg/broken.go"] != 1 {
		t.Errorf("first failure: count=%d want 1", a.faultRollback["pkg/broken.go"])
	}

	a.checkFaultRollback(changed)
	if a.faultRollback["pkg/broken.go"] != 0 {
		t.Errorf("after warning, count should reset: count=%d want 0", a.faultRollback["pkg/broken.go"])
	}
}

func TestFaultRollbackSkipsNonGoFiles(t *testing.T) {
	a := &Agent{
		faultRollback: map[string]int{},
	}
	changed := []string{"README.md", "lumen.toml"}
	a.checkFaultRollback(changed)
	if len(a.faultRollback) != 0 {
		t.Errorf("non-Go files skipped, got %v", a.faultRollback)
	}
}

// Repeated verify failure must NOT destroy the file's uncommitted contents — it
// only warns. (Previously it ran `git checkout`, silently discarding work.)
func TestFaultRollbackDoesNotTouchFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.go")
	const edited = "package pkg\nfunc Edited() {}\n"
	if err := os.WriteFile(filePath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	var warned bool
	a := &Agent{
		faultRollback: map[string]int{"test.go": 1},
	}
	a.SetSink(event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Level == event.LevelWarn {
			warned = true
		}
	}))
	a.checkFaultRollback([]string{"test.go"})

	got, _ := os.ReadFile(filePath)
	if string(got) != edited {
		t.Errorf("file contents must be untouched, got: %s", got)
	}
	if !warned {
		t.Error("expected a warning about the repeatedly-failing file")
	}
}
