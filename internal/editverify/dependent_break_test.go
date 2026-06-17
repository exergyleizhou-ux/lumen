package editverify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestVerifyCatchesDependentBreakage guards the regression where build/vet were
// scoped to the changed package: editing foo (a signature change) breaks its
// importer bar, which `go build ./foo` would NOT catch — only module-wide
// `go build ./...` does. Verify must report the module as broken.
func TestVerifyCatchesDependentBreakage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not in PATH")
	}
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module ex\n\ngo 1.23\n")
	// foo.Greet now REQUIRES an argument...
	write("foo/foo.go", "package foo\n\nfunc Greet(name string) string { return \"hi \" + name }\n")
	// ...but bar (an importer) still calls it with none — the module no longer compiles.
	write("bar/bar.go", "package bar\n\nimport \"ex/foo\"\n\nfunc X() string { return foo.Greet() }\n")

	v := New(root, DefaultConfig())
	res := v.Verify(context.Background(), []string{"foo/foo.go"}) // only foo was edited
	if res.OK {
		t.Fatal("Verify must catch that editing foo broke its importer bar (module-wide build)")
	}
}
