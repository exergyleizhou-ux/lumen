package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/tool"
)

// requireGopls skips if gopls not available (matches lsp_real_test pattern).
func requireGopls(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found in PATH; skipping LSP tool integration test")
	}
}

// TestLSPDiagnosticTool_ShippedRegistryPath exercises the real registered
// lsp_diagnostics Tool (the one the agent uses) against a deliberately broken
// Go file in a fresh module. This drives: tool registration (via Builtins
// after package inits), the Tool.Execute path, openInGopls + getGopls, and
// GetDiagnostics or vet fallback. Asserts non-empty diagnostics mentioning
// the error. This is the durable committed test proving the shipped LSP
// tool path works on real gopls.
func TestLSPDiagnosticTool_ShippedRegistryPath(t *testing.T) {
	requireGopls(t)

	// Create isolated module with a compile error.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testlsp\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	badFile := filepath.Join(dir, "bad.go")
	badSrc := `package main

import "fmt"

func main() {
	fmt.Println("hi"
`
	if err := os.WriteFile(badFile, []byte(badSrc), 0644); err != nil {
		t.Fatalf("write bad.go: %v", err)
	}

	// Chdir so that lazy gopls start (in getGopls) uses this workspace.
	// This ensures the singleton starts against a workspace containing the error.
	oldWD, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(oldWD)

	// Confirm the tool IS registered in the shipped builtins (what the agent sees).
	all := tool.Builtins()
	hasLSPDiag := false
	for _, bt := range all {
		if bt.Name() == "lsp_diagnostics" {
			hasLSPDiag = true
			break
		}
	}
	if !hasLSPDiag {
		t.Fatal("lsp_diagnostics not found in tool.Builtins() — registration broken")
	}

	// Drive the *registered tool* exactly as the agent loop does:
	// NewRegistry + Add from Builtins, then Get + Execute.
	// This is the "real tool registry" + shipped Tool.Execute path.
	reg := tool.NewRegistry()
	for _, bt := range all {
		reg.Add(bt)
	}
	diag, ok := reg.Get("lsp_diagnostics")
	if !ok {
		t.Fatal("could not get lsp_diagnostics from registry")
	}

	args, _ := json.Marshal(map[string]any{"file": "bad.go"})
	// Call twice with delay: first primes gopls/open, second gives time for analysis + fallback vet.
	out1, err := diag.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute1: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	out, err := diag.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute2: %v", err)
	}
	t.Logf("first via registered Execute: %s", out1)
	if strings.Contains(out, "0 issues in bad.go — clean.") {
		t.Fatalf("expected non-empty diagnostics via registered Tool.Execute; got: %q", out)
	}
	if !strings.Contains(out, "missing") && !strings.Contains(out, "newline") && !strings.Contains(out, "issue") && !strings.Contains(out, "vet") {
		t.Fatalf("diagnostics output did not contain error text/location; got: %q", out)
	}
	t.Logf("SUCCESS: registered Tool.Execute (lsp_diagnostics) returned non-empty with error: %s", out)
}
