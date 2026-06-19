package editverify

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestVerify_RealPythonToolchain is the end-to-end acceptance for the Python
// path: with real ruff installed, clean code passes and a lint violation fails
// Verify with the "lint" step. Proves the multi-language verify loop runs a real
// non-Go toolchain (not just Go's). Skips when ruff isn't installed.
func TestVerify_RealPythonToolchain(t *testing.T) {
	if _, err := exec.LookPath("ruff"); err != nil {
		t.Skip("ruff not installed")
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "pyproject.toml"), "[project]\nname = \"x\"\nversion = \"0\"\n")

	// Clean module → Verify passes.
	mustWrite(t, filepath.Join(root, "ok.py"), "x = 1\nprint(x)\n")
	v := New(root, DefaultConfig())
	if res := v.Verify(context.Background(), []string{"ok.py"}); !res.OK {
		t.Fatalf("clean python should pass ruff, got %+v", res)
	}

	// Unused import (ruff F401) → Verify fails at the lint step.
	mustWrite(t, filepath.Join(root, "bad.py"), "import os\nx = 1\nprint(x)\n")
	res := v.Verify(context.Background(), []string{"bad.py"})
	if res.OK {
		t.Fatal("python with an unused import should fail ruff")
	}
	if res.Failed == nil || res.Failed.Name != "lint" {
		t.Fatalf("expected failed step 'lint', got %+v", res.Failed)
	}
}
