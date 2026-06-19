package editverify

import (
	"os"
	"path/filepath"
	"testing"
)

// Running from a monorepo subdirectory (go.mod up a level) must still activate
// verify — FindProjectRoot walks up to the nearest marker.
func TestFindProjectRoot_FromSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindProjectRoot(sub); got != root {
		t.Errorf("FindProjectRoot from subdir = %q, want %q", got, root)
	}
	// the root dir itself resolves to itself
	if got := FindProjectRoot(root); got != root {
		t.Errorf("FindProjectRoot at root = %q, want %q", got, root)
	}
	// a dir with no markers anywhere up returns "" (a bare temp dir)
	if got := FindProjectRoot(t.TempDir()); got != "" {
		t.Errorf("FindProjectRoot with no project = %q, want empty", got)
	}
}
