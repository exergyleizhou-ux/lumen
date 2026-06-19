package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func withMissingGo(t *testing.T) {
	t.Helper()
	old := execLookPath
	execLookPath = func(file string) (string, error) {
		if file == "go" || file == "gopls" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + file, nil
	}
	t.Cleanup(func() { execLookPath = old })
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
}

// In a non-Go workspace (no go.mod), a missing Go toolchain is irrelevant and
// must not hard-fail the whole report — the user is in a Python/JS project.
func TestCheckGoToolchain_NonGoProjectDoesNotHardFail(t *testing.T) {
	withMissingGo(t)
	chdir(t, t.TempDir())

	r := &Report{AllOk: true}
	r.checkGoToolchain()
	if !r.AllOk {
		t.Error("missing go in a non-Go workspace must NOT set AllOk=false")
	}
}

// In a real Go project (go.mod present), a missing Go toolchain is a genuine
// failure and must hard-fail.
func TestCheckGoToolchain_GoProjectHardFailsWhenMissing(t *testing.T) {
	withMissingGo(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	r := &Report{AllOk: true}
	r.checkGoToolchain()
	if r.AllOk {
		t.Error("missing go in a Go project (go.mod present) must set AllOk=false")
	}
}
