package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/event"
)

func TestFaultRollbackCountsConsecutiveFailures(t *testing.T) {
	a := &Agent{
		faultRollback: map[string]int{},
		sink:          event.Discard,
	}
	changed := []string{"pkg/broken.go"}
	
	a.checkFaultRollback(changed)
	if a.faultRollback["pkg/broken.go"] != 1 {
		t.Errorf("first failure: count=%d want 1", a.faultRollback["pkg/broken.go"])
	}
	
	a.checkFaultRollback(changed)
	if a.faultRollback["pkg/broken.go"] != 0 {
		t.Errorf("after rollback count=%d want 0", a.faultRollback["pkg/broken.go"])
	}
}

func TestFaultRollbackSkipsNonGoFiles(t *testing.T) {
	a := &Agent{
		faultRollback: map[string]int{},
		sink:          event.Discard,
	}
	changed := []string{"README.md", "lumen.toml"}
	a.checkFaultRollback(changed)
	if len(a.faultRollback) != 0 {
		t.Errorf("non-Go files skipped, got %v", a.faultRollback)
	}
}

func TestFaultRollbackRealGitCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	exec.Command("git", "init", dir).Run()
	
	filePath := filepath.Join(dir, "test.go")
	os.WriteFile(filePath, []byte("package pkg\nfunc Original() {}\n"), 0644)
	exec.Command("git", "-C", dir, "add", "test.go").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init").Run()
	
	os.WriteFile(filePath, []byte("package pkg\nfunc Broken() {!!!}\n"), 0644)
	
	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)
	
	a := &Agent{
		faultRollback: map[string]int{"test.go": 1},
		sink:          event.Discard,
	}
	a.checkFaultRollback([]string{"test.go"})
	
	content, _ := os.ReadFile(filePath)
	if !strings.Contains(string(content), "func Original") {
		t.Errorf("expected Original after rollback, got: %s", string(content))
	}
}
