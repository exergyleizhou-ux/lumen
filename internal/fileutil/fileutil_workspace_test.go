package fileutil

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	runworkspace "lumen/internal/workspace"
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

func TestContextFileOperationsIsolateParallelWorkspaces(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "note.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "note.txt"), []byte("beta"), 0o600); err != nil {
		t.Fatal(err)
	}
	wsA, err := runworkspace.NewLocal("a", rootA, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wsB, err := runworkspace.NewLocal("b", rootB, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctxA := runworkspace.WithContext(context.Background(), wsA)
	ctxB := runworkspace.WithContext(context.Background(), wsB)

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, marker := ctxA, "alpha"
			if i%2 == 1 {
				ctx, marker = ctxB, "beta"
			}
			content, _, _, err := SafeReadFileContext(ctx, "note.txt", 0, 10)
			if err != nil {
				errs <- err
				return
			}
			if !strings.Contains(content, marker) {
				errs <- &workspaceTestError{message: "read crossed roots: " + content}
				return
			}
			name := "generated-" + marker + ".txt"
			if err := SafeWriteFileContext(ctx, name, []byte(marker)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(rootA, "generated-beta.txt")); !os.IsNotExist(err) {
		t.Fatalf("beta write escaped into root A: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootB, "generated-alpha.txt")); !os.IsNotExist(err) {
		t.Fatalf("alpha write escaped into root B: %v", err)
	}
}

type workspaceTestError struct{ message string }

func (e *workspaceTestError) Error() string { return e.message }
