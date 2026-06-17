package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNotebookEditPreservesEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nb.ipynb")
	orig := `{"cells":[{"cell_type":"code","source":"x=1","outputs":[],"execution_count":null,"metadata":{}}],"metadata":{"kernelspec":{"name":"python3"},"language_info":{"name":"python"}},"nbformat":4,"nbformat_minor":5}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := &NotebookEditTool{}
	args := fmt.Sprintf(`{"path":%q,"cell_number":0,"edit_mode":"replace","new_source":"x=2"}`, path)
	if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(path)
	var nb map[string]any
	if err := json.Unmarshal(out, &nb); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := nb["nbformat"]; !ok {
		t.Error("nbformat must be preserved (else Jupyter rejects the file)")
	}
	if _, ok := nb["nbformat_minor"]; !ok {
		t.Error("nbformat_minor must be preserved")
	}
	if _, ok := nb["metadata"]; !ok {
		t.Error("top-level metadata (kernelspec/language_info) must be preserved")
	}
	cells, ok := nb["cells"].([]any)
	if !ok || len(cells) != 1 {
		t.Fatalf("cells malformed: %v", nb["cells"])
	}
	if src := cells[0].(map[string]any)["source"]; src != "x=2" {
		t.Errorf("cell source not updated, got %v", src)
	}
}

func TestDeleteRangeRejectsAmbiguousAnchor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	const content = "X\nmid\nX\nY\nkeep\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := &DeleteRangeTool{}
	args := fmt.Sprintf(`{"path":%q,"start_anchor":"X","end_anchor":"Y"}`, path)
	if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err == nil {
		t.Fatal("ambiguous start_anchor (2 matches) must error, not silently over-delete")
	}
	out, _ := os.ReadFile(path)
	if string(out) != content {
		t.Errorf("file must be unchanged when the edit is rejected, got %q", out)
	}
}

func TestDeleteRangeUniqueAnchorsWork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nSTART\nb\nc\nEND\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := &DeleteRangeTool{}
	args := fmt.Sprintf(`{"path":%q,"start_anchor":"START","end_anchor":"END"}`, path)
	if _, err := tool.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatalf("unique anchors should succeed: %v", err)
	}
	out, _ := os.ReadFile(path)
	if string(out) != "a\nd\n" {
		t.Errorf("expected START..END (inclusive) removed, got %q", out)
	}
}
