package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathUsesWorkspaceRoot(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "workspace")
	_ = os.MkdirAll(ws, 0o700)
	t.Setenv("LUMEN_WORKSPACE_ROOT", ws)
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	_ = os.Chdir(t.TempDir()) // cwd must differ from workspace

	resolved, err := ResolvePathAllowMissing("reports/out.md")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(ws, "reports", "out.md"))
	got, _ := filepath.EvalSymlinks(resolved)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}