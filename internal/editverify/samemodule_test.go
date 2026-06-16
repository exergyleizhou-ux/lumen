package editverify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// setupModule builds a temp root module with a nested module and a normal nested
// package, returning the root dir.
func setupModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module testmod\n\ngo 1.21\n")
	// normal nested package (same module)
	os.MkdirAll(filepath.Join(root, "pkg", "sub"), 0o755)
	mustWrite(t, filepath.Join(root, "pkg", "sub", "s.go"), "package sub\n")
	// nested MODULE (own go.mod)
	os.MkdirAll(filepath.Join(root, "vendored", "lib"), 0o755)
	mustWrite(t, filepath.Join(root, "vendored", "go.mod"), "module other\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(root, "vendored", "lib", "l.go"), "package lib\n")
	return root
}

func TestSameModulePaths(t *testing.T) {
	root := setupModule(t)
	v := New(root, DefaultConfig())

	cases := []struct {
		name string
		in   []string
		keep bool
	}{
		{"same-module nested pkg", []string{"pkg/sub/s.go"}, true},
		{"root file", []string{"main.go"}, true}, // dir "." exists (root)
		{"nested module file", []string{"vendored/lib/l.go"}, false},
		{"nested module root file", []string{"vendored/x.go"}, false},
		{"deleted/nonexistent dir", []string{"gone/x.go"}, false},
		{"escapes root", []string{"../outside.go"}, false},
	}
	// "main.go" dir is "." → root exists; create it so the stat passes.
	mustWrite(t, filepath.Join(root, "main.go"), "package main\n")

	for _, c := range cases {
		got := v.sameModulePaths(c.in)
		kept := len(got) == 1
		if kept != c.keep {
			t.Errorf("%s: kept=%v want %v (got %v)", c.name, kept, c.keep, got)
		}
	}
}

func TestSameModulePaths_NonGoPassThrough(t *testing.T) {
	root := setupModule(t)
	v := New(root, DefaultConfig())
	got := v.sameModulePaths([]string{"README.md", "vendored/lib/l.go"})
	// README passes through (non-.go); nested-module .go is dropped.
	if len(got) != 1 || got[0] != "README.md" {
		t.Errorf("expected only README.md, got %v", got)
	}
}

// TestVerify_NestedModuleNoSpuriousFailure: changing a file only in a nested
// module must not yield a test step against it (which would fail spuriously);
// build+vet at root still run and pass.
func TestVerify_NestedModuleNoSpuriousFailure(t *testing.T) {
	root := setupModule(t)
	// root module must itself build cleanly
	mustWrite(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	var ran []string
	v := New(root, DefaultConfig())
	v.run = funcRunner(func(step Step) (string, bool) {
		ran = append(ran, step.Name+":"+lastArg(step))
		return "", true // everything passes
	})

	res := v.Verify(context.Background(), []string{"vendored/lib/l.go"})
	if !res.OK {
		t.Fatalf("nested-module change should not fail: %+v", res)
	}
	// No test step should target the nested module.
	for _, r := range ran {
		if r == "test:./vendored/lib" {
			t.Errorf("must not test nested module; ran %v", ran)
		}
	}
}

// helpers
type funcRunner func(Step) (string, bool)

func (f funcRunner) Run(_ context.Context, step Step) (string, bool) { return f(step) }

func lastArg(s Step) string {
	if len(s.Args) == 0 {
		return ""
	}
	return s.Args[len(s.Args)-1]
}
