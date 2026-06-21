package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// findProjectRoot is the safety logic that narrows code indexing to the project
// (so it never walks the whole home dir). It must stop at the nearest .git /
// go.mod / package.json walking up from a nested start.
func TestFindProjectRoot(t *testing.T) {
	for _, marker := range []string{".git", "go.mod", "package.json"} {
		t.Run(marker, func(t *testing.T) {
			root := t.TempDir()
			if marker == ".git" {
				mustWrite(t, filepath.Join(root, marker, "HEAD"), "ref: refs/heads/main")
			} else {
				mustWrite(t, filepath.Join(root, marker), "x")
			}
			nested := filepath.Join(root, "a", "b", "c")
			mustWrite(t, filepath.Join(nested, "f.go"), "package c")

			if got := findProjectRoot(nested); got != root {
				t.Errorf("findProjectRoot(%q) = %q, want %q", nested, got, root)
			}
		})
	}
}

func TestFindProjectRootFallsBackToStart(t *testing.T) {
	// A bare dir with no marker up the chain falls back to the start dir.
	start := t.TempDir()
	if got := findProjectRoot(start); got != start {
		t.Errorf("with no marker, findProjectRoot should return the start %q, got %q", start, got)
	}
}

// These validation paths return before getIndex(), so they don't touch the
// global (sync.Once) index.
func TestCodeSearchQueryRequired(t *testing.T) {
	cs := &CodeSearchTool{}
	if _, err := cs.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Error("empty query should error")
	}
}

func TestCodeSearchInvalidArgs(t *testing.T) {
	cs := &CodeSearchTool{}
	_, err := cs.Execute(context.Background(), json.RawMessage(`{"query":`))
	if err == nil || !strings.Contains(err.Error(), "code_search") {
		t.Errorf("malformed json should error, got %v", err)
	}
}
