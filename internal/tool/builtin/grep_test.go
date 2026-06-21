package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Grepping a single file (not a dir) exercises the grepFile path and the
// path:line:text result format.
func TestGrepSingleFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.go")
	mustWrite(t, f, "package main\nfunc main() {}\n// trailing\n")

	gt := &GrepTool{}
	out, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"func","path":"`+f+`"}`))
	if err != nil {
		t.Fatalf("grep single file: %v", err)
	}
	want := f + ":2:func main() {}"
	if out != want {
		t.Errorf("grep result = %q, want %q", out, want)
	}
}

func TestGrepInvalidRegex(t *testing.T) {
	gt := &GrepTool{}
	_, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"[unclosed","path":"."}`))
	if err == nil || !strings.Contains(err.Error(), "regex") {
		t.Errorf("invalid regex should error with a regex message, got %v", err)
	}
}

func TestGrepPatternRequired(t *testing.T) {
	gt := &GrepTool{}
	if _, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":""}`)); err == nil {
		t.Error("empty pattern should error")
	}
}

func TestGrepMissingPath(t *testing.T) {
	gt := &GrepTool{}
	_, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"x","path":"`+filepath.Join(t.TempDir(), "nope")+`"}`))
	if err == nil || !strings.Contains(err.Error(), "stat") {
		t.Errorf("missing path should error with a stat message, got %v", err)
	}
}

// Directory search must skip hidden files/dirs and node_modules.
func TestGrepSkipsHiddenAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "keep.go"), "needle here\n")
	mustWrite(t, filepath.Join(dir, ".secret.env"), "needle here\n")
	mustWrite(t, filepath.Join(dir, ".git", "config"), "needle here\n")
	mustWrite(t, filepath.Join(dir, "node_modules", "dep.js"), "needle here\n")

	gt := &GrepTool{}
	out, err := gt.Execute(context.Background(), json.RawMessage(`{"pattern":"needle","path":"`+dir+`"}`))
	if err != nil {
		t.Fatalf("grep dir: %v", err)
	}
	if !strings.Contains(out, "keep.go") {
		t.Errorf("should match the visible file, got %q", out)
	}
	if strings.Contains(out, ".secret.env") || strings.Contains(out, "config") || strings.Contains(out, "dep.js") {
		t.Errorf("must skip hidden files and node_modules, got %q", out)
	}
}
