package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireGopls skips the test if gopls is not found in PATH.
func requireGopls(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found in PATH; skipping LSP integration test")
	}
}

// setupGoModule creates a temporary directory with a go.mod and a minimal Go
// file. Returns the workspace root directory path and the URI of the Go file.
func setupGoModule(t *testing.T) (workspaceRoot string, fileURI string, filePath string) {
	t.Helper()

	dir := t.TempDir()

	// Write go.mod.
	goMod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(goMod, []byte("module testmod\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// Write a small Go file with a function we can inspect.
	mainGo := filepath.Join(dir, "main.go")
	content := `package main

import "fmt"

// greet returns a friendly greeting for the given name.
func greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func main() {
	msg := greet("world")
	fmt.Println(msg)
}
`
	if err := os.WriteFile(mainGo, []byte(content), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	uri := "file://" + mainGo
	return dir, uri, mainGo
}

// TestStartGopls verifies that we can launch gopls and initialize.
func TestStartGopls(t *testing.T) {
	requireGopls(t)
	workspaceRoot, _, _ := setupGoModule(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := StartGopls(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("StartGopls: %v", err)
	}
	defer client.Shutdown()

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// TestOpenDocumentAndDiagnostics opens a Go file and fetches diagnostics.
func TestOpenDocumentAndDiagnostics(t *testing.T) {
	requireGopls(t)
	workspaceRoot, uri, _ := setupGoModule(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := StartGopls(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("StartGopls: %v", err)
	}
	defer client.Shutdown()

	// Read the file content so we can open it.
	content, err := os.ReadFile(strings.TrimPrefix(uri, "file://"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if err := client.OpenDocument(uri, string(content)); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	// Give gopls a moment to process.
	time.Sleep(500 * time.Millisecond)

	diags, err := client.GetDiagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("GetDiagnostics: %v", err)
	}

	// A valid Go file should produce zero diagnostics.
	if len(diags) != 0 {
		for _, d := range diags {
			t.Logf("unexpected diagnostic: %s:%d:%d: %s",
				uri, d.Range.Start.Line, d.Range.Start.Character, d.Message)
		}
		t.Fatalf("expected 0 diagnostics, got %d", len(diags))
	}
}

// TestCompletion requests completion at a known position and verifies we get
// results.
func TestCompletion(t *testing.T) {
	requireGopls(t)
	workspaceRoot, uri, _ := setupGoModule(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := StartGopls(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("StartGopls: %v", err)
	}
	defer client.Shutdown()

	content, err := os.ReadFile(strings.TrimPrefix(uri, "file://"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if err := client.OpenDocument(uri, string(content)); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	// Give gopls time to load the package.
	time.Sleep(1 * time.Second)

	// Request completion at "fmt.Prin" — line 12 (0-based), character after "fmt.Prin"
	// The file content has: fmt.Println(msg) at line 12 (0-based line 11).
	// Let's try a simpler position: inside main() after "fmt." — line 11, col 7 would be after "fmt."
	// Actually, let's pick a safe position: inside the greet function, line 6, col 10
	// which is inside "fmt.Sprintf".
	// Line 6 (0-based): return fmt.Sprintf("Hello, %s!", name)
	// Let's try completion at line 6, char 16 which is inside Sprintf.
	completions, err := client.GetCompletion(ctx, uri, 5, 16)
	if err != nil {
		t.Fatalf("GetCompletion: %v", err)
	}

	if len(completions) == 0 {
		t.Fatal("expected at least one completion item")
	}

	t.Logf("got %d completions", len(completions))
	for i, c := range completions {
		if i >= 5 {
			break
		}
		t.Logf("  completion[%d]: label=%q kind=%d detail=%q", i, c.Label, c.Kind, c.Detail)
	}
}

// TestHover requests hover information at a known symbol.
func TestHover(t *testing.T) {
	requireGopls(t)
	workspaceRoot, uri, _ := setupGoModule(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := StartGopls(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("StartGopls: %v", err)
	}
	defer client.Shutdown()

	content, err := os.ReadFile(strings.TrimPrefix(uri, "file://"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if err := client.OpenDocument(uri, string(content)); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Hover over "greet" at line 11 (0-based: 10), character after "greet".
	hover, err := client.GetHover(ctx, uri, 10, 6)
	if err != nil {
		t.Fatalf("GetHover: %v", err)
	}

	if hover == nil {
		t.Fatal("expected non-nil hover")
	}
	t.Logf("hover contents: %s", hover.Contents)
}

// TestDefinition requests the definition of a symbol.
func TestDefinition(t *testing.T) {
	requireGopls(t)
	workspaceRoot, uri, _ := setupGoModule(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := StartGopls(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("StartGopls: %v", err)
	}
	defer client.Shutdown()

	content, err := os.ReadFile(strings.TrimPrefix(uri, "file://"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if err := client.OpenDocument(uri, string(content)); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Definition of "greet" in `msg := greet("world")` — line 10 (0-based),
	// where the identifier begins at column 8.
	locs, err := client.GetDefinition(ctx, uri, 10, 10)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}

	if len(locs) == 0 {
		t.Fatal("expected at least one definition location")
	}

	t.Logf("definition: uri=%s line=%d char=%d",
		locs[0].URI, locs[0].Range.Start.Line, locs[0].Range.Start.Character)
}

// TestReferences requests references to a symbol.
func TestReferences(t *testing.T) {
	requireGopls(t)
	workspaceRoot, uri, _ := setupGoModule(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := StartGopls(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("StartGopls: %v", err)
	}
	defer client.Shutdown()

	content, err := os.ReadFile(strings.TrimPrefix(uri, "file://"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if err := client.OpenDocument(uri, string(content)); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	time.Sleep(1 * time.Second)

	// References of "greet" at its declaration `func greet(...)` — line 5
	// (0-based), where the identifier begins at column 5.
	locs, err := client.GetReferences(ctx, uri, 5, 7, true)
	if err != nil {
		t.Fatalf("GetReferences: %v", err)
	}

	if len(locs) == 0 {
		t.Fatal("expected at least one reference")
	}

	t.Logf("found %d reference(s)", len(locs))
	for _, loc := range locs {
		t.Logf("  ref: uri=%s line=%d char=%d",
			loc.URI, loc.Range.Start.Line, loc.Range.Start.Character)
	}
}

// TestCloseDocument verifies didClose does not error.
func TestCloseDocument(t *testing.T) {
	requireGopls(t)
	workspaceRoot, uri, _ := setupGoModule(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := StartGopls(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("StartGopls: %v", err)
	}
	defer client.Shutdown()

	content, err := os.ReadFile(strings.TrimPrefix(uri, "file://"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if err := client.OpenDocument(uri, string(content)); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	if err := client.CloseDocument(uri); err != nil {
		t.Fatalf("CloseDocument: %v", err)
	}
}

// TestShutdown verifies that calling Shutdown twice is safe.
func TestShutdown(t *testing.T) {
	requireGopls(t)
	workspaceRoot, _, _ := setupGoModule(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := StartGopls(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("StartGopls: %v", err)
	}

	if err := client.Shutdown(); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	// Second call must be safe.
	if err := client.Shutdown(); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}
