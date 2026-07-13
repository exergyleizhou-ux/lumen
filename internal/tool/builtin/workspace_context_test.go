package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	runworkspace "lumen/internal/workspace"
)

func TestCoreFileToolsUseWorkspaceContext(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "shared.txt"), []byte("alpha old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "shared.txt"), []byte("beta old"), 0o600); err != nil {
		t.Fatal(err)
	}
	wsA, _ := runworkspace.NewLocal("a", rootA, "", nil)
	wsB, _ := runworkspace.NewLocal("b", rootB, "", nil)
	ctxA := runworkspace.WithContext(context.Background(), wsA)
	ctxB := runworkspace.WithContext(context.Background(), wsB)

	var wg sync.WaitGroup
	for _, tc := range []struct {
		ctx    context.Context
		marker string
	}{
		{ctxA, "alpha"},
		{ctxB, "beta"},
	} {
		tc := tc
		wg.Add(1)
		go func() {
			defer wg.Done()
			readArgs, _ := json.Marshal(map[string]any{"path": "shared.txt", "limit": 10})
			out, err := (&ReadFileTool{}).Execute(tc.ctx, readArgs)
			if err != nil || !strings.Contains(out, tc.marker+" old") {
				t.Errorf("read %s: out=%q err=%v", tc.marker, out, err)
				return
			}

			editArgs, _ := json.Marshal(map[string]string{"path": "shared.txt", "old_string": tc.marker + " old", "new_string": tc.marker + " edited"})
			preview, err := (&EditFileTool{}).Preview(tc.ctx, editArgs)
			if err != nil {
				t.Errorf("preview %s: %v", tc.marker, err)
				return
			}
			ws, _ := runworkspace.FromContext(tc.ctx)
			if preview.Path != filepath.Join(ws.Root, "shared.txt") {
				t.Errorf("preview %s path=%q", tc.marker, preview.Path)
				return
			}
			if _, err := (&EditFileTool{}).Execute(tc.ctx, editArgs); err != nil {
				t.Errorf("edit %s: %v", tc.marker, err)
				return
			}

			writeArgs, _ := json.Marshal(map[string]string{"path": "generated.txt", "content": tc.marker})
			if _, err := (&WriteFileTool{}).Execute(tc.ctx, writeArgs); err != nil {
				t.Errorf("write %s: %v", tc.marker, err)
			}
		}()
	}
	wg.Wait()

	for root, marker := range map[string]string{rootA: "alpha", rootB: "beta"} {
		shared, _ := os.ReadFile(filepath.Join(root, "shared.txt"))
		generated, _ := os.ReadFile(filepath.Join(root, "generated.txt"))
		if string(shared) != marker+" edited" || string(generated) != marker {
			t.Fatalf("workspace %s crossed: shared=%q generated=%q", marker, shared, generated)
		}
	}
}

func TestMultiEditAndNotebookUseWorkspaceContext(t *testing.T) {
	root := t.TempDir()
	ws, err := runworkspace.NewLocal("science", root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := runworkspace.WithContext(context.Background(), ws)
	if err := os.WriteFile(filepath.Join(root, "multi.txt"), []byte("one two"), 0o600); err != nil {
		t.Fatal(err)
	}
	notebook := `{"cells":[{"cell_type":"code","source":"x=1","outputs":[],"execution_count":null,"metadata":{}}],"metadata":{},"nbformat":4,"nbformat_minor":5}`
	if err := os.WriteFile(filepath.Join(root, "nb.ipynb"), []byte(notebook), 0o600); err != nil {
		t.Fatal(err)
	}

	multiArgs := json.RawMessage(`{"path":"multi.txt","edits":[{"old_string":"one","new_string":"1"},{"old_string":"two","new_string":"2"}]}`)
	if _, err := (&MultiEditTool{}).Execute(ctx, multiArgs); err != nil {
		t.Fatal(err)
	}
	nbArgs := json.RawMessage(`{"path":"nb.ipynb","cell_number":0,"edit_mode":"replace","new_source":"x=2"}`)
	if _, err := (&NotebookEditTool{}).Execute(ctx, nbArgs); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(filepath.Join(root, "multi.txt")); string(got) != "1 2" {
		t.Fatalf("multi edit=%q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "nb.ipynb")); !strings.Contains(string(got), "x=2") {
		t.Fatalf("notebook was not updated: %s", got)
	}
}
