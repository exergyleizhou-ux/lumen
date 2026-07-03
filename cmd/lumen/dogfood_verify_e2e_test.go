package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCLIVerifyAfterEditE2E is the canonical checked-in test for AC1.
// It creates an isolated Go module workspace (with go.mod so FindProjectRoot
// + SetVerifier + anyInWorkspace all fire), seeds a broken bug.go, writes a
// lumen.toml that activates the TEST_E2E_SUCCESS bypass (relative path only),
// chdirs into the module, execs the real built lumen binary "run" command,
// and asserts the shipped verify-after-edit observables are present:
//   - "verifying..." (from VerifyStarted in terminal sink)
//   - "✓ verified" (from success VerifyResult)
// It fails explicitly if only bare tool ✓ (from write_file result) appears
// without the verify lines. This drives the real CLI entry + controller +
// agent + editverify + terminal paths.
func TestCLIVerifyAfterEditE2E(t *testing.T) {
	// Build a fresh binary for this test from the module root (robust even if test cwd is package dir).
	// Compute module root from this test file location.
	_, thisFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	bin := filepath.Join(t.TempDir(), "lumen-testbin")
	buildCmd := exec.Command("go", "build", "-o", bin, filepath.Join(moduleRoot, "cmd", "lumen"))
	buildCmd.Dir = moduleRoot // ensure build runs from module root
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("go build lumen: %v", err)
	}

	// Isolated workspace = temp dir turned into a Go module.
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module e2edogfood\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Broken file: will fail to build (undefined identifier).
	badCode := `package main

func main() {
	println(undefinedVar) // deliberate error
}
`
	if err := os.WriteFile(filepath.Join(ws, "bug.go"), []byte(badCode), 0644); err != nil {
		t.Fatal(err)
	}

	// Config that activates the TEST bypass (relative path "bug.go" inside this module).
	toml := `default_model = "test-e2e"

[[providers]]
name = "test-e2e"
kind = "openai"
base_url = "https://api.openai.com/v1"
model = "dummy"
api_key = "TEST_E2E_SUCCESS"
`
	if err := os.WriteFile(filepath.Join(ws, "lumen.toml"), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	// Run from inside the module so cwd == workspace root, relative edit stays in-workspace.
	prompt := `fix the compile error in bug.go (undefinedVar). Make it a valid program that builds and runs cleanly.`
	cmd := exec.Command(bin, "run", prompt)
	cmd.Dir = ws
	out, err := cmd.CombinedOutput()
	output := string(out)
	t.Logf("lumen run output (dir=%s):\n%s", ws, output)
	if err != nil {
		// Non-zero is ok if it still performed the turn + verify; we care about observables.
		t.Logf("lumen run exit: %v (continuing to check output for verify strings)", err)
	}

	// Hard requirement: real verify-after-edit must have fired and produced the
	// shipped strings from terminal.go / agent events.
	if !strings.Contains(output, "verifying...") || !strings.Contains(output, "✓ verified") {
		t.Fatalf("AC1 verification failed: expected both 'verifying...' and '✓ verified' (from shipped VerifyStarted + success VerifyResult) in dogfood output.\nGot:\n%s\n\nNote: bare tool ✓ from write_file is NOT sufficient; verify-after-edit must have run.", output)
	}

	// Optional sanity: the file should have been edited by the tool.
	fixed, _ := os.ReadFile(filepath.Join(ws, "bug.go"))
	if !strings.Contains(string(fixed), "fixed by test turn") {
		t.Logf("warning: bug.go content after run: %s", fixed)
	}
}