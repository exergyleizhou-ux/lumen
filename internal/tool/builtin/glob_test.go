package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// glob's ** recursive branch is the complex, previously-untested path.
func TestGlobRecursive(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "top.go"), "")
	mustWrite(t, filepath.Join(dir, "sub", "deep", "nested.go"), "")
	mustWrite(t, filepath.Join(dir, "sub", "note.txt"), "")

	gt := &GlobTool{}
	out, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"`+dir+`/**/*.go"}`))
	if err != nil {
		t.Fatalf("glob **: %v", err)
	}
	if !strings.Contains(out, "nested.go") {
		t.Errorf("recursive glob should descend into sub/deep, got %q", out)
	}
	if strings.Contains(out, "note.txt") {
		t.Errorf("*.go suffix must not match note.txt, got %q", out)
	}
}

// The recursive walk must skip hidden dirs and node_modules.
func TestGlobRecursiveSkipsHiddenAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "keep.go"), "")
	mustWrite(t, filepath.Join(dir, ".hidden", "secret.go"), "")
	mustWrite(t, filepath.Join(dir, "node_modules", "dep.go"), "")

	gt := &GlobTool{}
	out, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"`+dir+`/**/*.go"}`))
	if err != nil {
		t.Fatalf("glob **: %v", err)
	}
	if !strings.Contains(out, "keep.go") {
		t.Errorf("should find keep.go, got %q", out)
	}
	if strings.Contains(out, "secret.go") {
		t.Errorf("must skip hidden dirs, got %q", out)
	}
	if strings.Contains(out, "dep.go") {
		t.Errorf("must skip node_modules, got %q", out)
	}
}

func TestGlobEmptyPattern(t *testing.T) {
	gt := &GlobTool{}
	if _, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":""}`)); err == nil {
		t.Error("empty pattern should error")
	}
}

func TestGlobInvalidArgs(t *testing.T) {
	gt := &GlobTool{}
	if _, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":`)); err == nil {
		t.Error("malformed json should error")
	}
}

// ls recursive is its own previously-untested branch.
func TestLsRecursive(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "hi")
	mustWrite(t, filepath.Join(dir, "pkg", "b.go"), "x")
	mustWrite(t, filepath.Join(dir, ".git", "config"), "secret")

	lt := &LsTool{}
	out, err := lt.Execute(context.Background(), json.RawMessage(`{"path":"`+dir+`","recursive":true}`))
	if err != nil {
		t.Fatalf("ls recursive: %v", err)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, filepath.Join("pkg", "b.go")) {
		t.Errorf("recursive ls should list nested files, got %q", out)
	}
	if strings.Contains(out, "config") {
		t.Errorf("recursive ls must skip hidden dirs like .git, got %q", out)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
