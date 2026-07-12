package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"lumen/internal/diff"
	"lumen/internal/fileutil"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&NotebookEditTool{})
	tool.RegisterBuiltin(&DeleteRangeTool{})
}

// ── notebook_edit ──────────────────────────────────────────

// NotebookEditTool edits one cell of a Jupyter notebook (.ipynb).
type NotebookEditTool struct{}

func (t *NotebookEditTool) Name() string   { return "notebook_edit" }
func (t *NotebookEditTool) ReadOnly() bool { return false }

func (t *NotebookEditTool) Description() string {
	return "Edit one cell of a Jupyter notebook (.ipynb). Target a cell by 0-based cell_number (or cell_id). edit_mode: \"replace\" (default) swaps the cell's source; \"insert\" adds a new cell after cell_number (use -1 to prepend at the top), taking cell_type and new_source; \"delete\" removes the cell. Editing a code cell clears its outputs."
}

func (t *NotebookEditTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"Path to the .ipynb notebook."},
  "cell_number":{"type":"integer","description":"0-based index of the target cell."},
  "cell_id":{"type":"string","description":"Target the cell by its id instead of cell_number."},
  "edit_mode":{"type":"string","enum":["replace","insert","delete"],"description":"replace (default), insert, or delete."},
  "cell_type":{"type":"string","enum":["code","markdown"],"description":"Cell type for insert."},
  "new_source":{"type":"string","description":"The cell's new source text."}
},
"required":["path"]
}`)
}

func (t *NotebookEditTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path       string `json:"path"`
		CellNumber int    `json:"cell_number"`
		CellID     string `json:"cell_id"`
		EditMode   string `json:"edit_mode"`
		CellType   string `json:"cell_type"`
		NewSource  string `json:"new_source"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if p.EditMode == "" {
		p.EditMode = "replace"
	}

	resolved, err := fileutil.ResolvePath(p.Path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p.Path, err)
	}
	if wsRoot := fileutil.WorkspaceRoot(); wsRoot != "" {
		if err := fileutil.ValidateWorkspaceBoundary(resolved, wsRoot); err != nil {
			return "", err
		}
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}

	// Unmarshal the WHOLE notebook so the top-level envelope (nbformat,
	// nbformat_minor, metadata/kernelspec) survives the round-trip — operating on
	// only the cells array would write a spec-invalid file Jupyter can't open.
	var nb map[string]any
	if err := json.Unmarshal(data, &nb); err != nil {
		return "", fmt.Errorf("invalid notebook JSON: %w", err)
	}
	rawCells, _ := nb["cells"].([]any)
	cells := make([]map[string]any, 0, len(rawCells))
	for _, rc := range rawCells {
		if m, ok := rc.(map[string]any); ok {
			cells = append(cells, m)
		}
	}

	switch p.EditMode {
	case "replace":
		idx := p.CellNumber
		if p.CellID != "" {
			for i, c := range cells {
				if id, _ := c["id"].(string); id == p.CellID {
					idx = i
					break
				}
			}
		}
		if idx < 0 || idx >= len(cells) {
			return "", fmt.Errorf("cell %d out of range (0..%d)", idx, len(cells)-1)
		}
		cells[idx]["source"] = p.NewSource
		// Clear outputs for code cells
		if ct, _ := cells[idx]["cell_type"].(string); ct == "code" {
			cells[idx]["outputs"] = []any{}
			cells[idx]["execution_count"] = nil
		}
		return writeNotebook(resolved, nb, cells, "replaced cell", idx)

	case "insert":
		newCell := map[string]any{
			"cell_type": p.CellType,
			"source":    p.NewSource,
			"metadata":  map[string]any{},
		}
		if p.CellType == "code" {
			newCell["outputs"] = []any{}
			newCell["execution_count"] = nil
		}
		idx := p.CellNumber + 1 // insert after
		if p.CellNumber < 0 {
			cells = append([]map[string]any{newCell}, cells...)
			return writeNotebook(resolved, nb, cells, "prepended cell", 0)
		}
		if idx >= len(cells) {
			cells = append(cells, newCell)
		} else {
			cells = append(cells[:idx], append([]map[string]any{newCell}, cells[idx:]...)...)
		}
		return writeNotebook(resolved, nb, cells, "inserted cell", idx)

	case "delete":
		idx := p.CellNumber
		if p.CellID != "" {
			for i, c := range cells {
				if id, _ := c["id"].(string); id == p.CellID {
					idx = i
					break
				}
			}
		}
		if idx < 0 || idx >= len(cells) {
			return "", fmt.Errorf("cell %d out of range", idx)
		}
		cells = append(cells[:idx], cells[idx+1:]...)
		return writeNotebook(resolved, nb, cells, "deleted cell", idx)
	}

	return "", fmt.Errorf("unknown edit_mode: %s", p.EditMode)
}

func (t *NotebookEditTool) Preview(ctx context.Context, args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path string `json:"path"`
	}
	json.Unmarshal(args, &p)
	data, _ := os.ReadFile(p.Path)
	return diff.Change{Path: p.Path, Before: string(data)}, nil
}

func writeNotebook(path string, nb map[string]any, cells []map[string]any, action string, idx int) (string, error) {
	nb["cells"] = cells
	data, err := json.MarshalIndent(nb, "", " ")
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	// Atomic + workspace-boundary-checked write (temp file + rename under SafeWriteFile).
	if err := fileutil.SafeWriteFile(path, fileutil.WorkspaceRoot(), data); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return fmt.Sprintf("%s at index %d", action, idx), nil
}

// ── delete_range ───────────────────────────────────────────

// DeleteRangeTool deletes a contiguous text range from a file using exact
// start/end text anchors.
type DeleteRangeTool struct{}

func (t *DeleteRangeTool) Name() string   { return "delete_range" }
func (t *DeleteRangeTool) ReadOnly() bool { return false }

func (t *DeleteRangeTool) Description() string {
	return "Delete a contiguous text range from a file using exact start/end text anchors. Each anchor must match exactly one line. Returns unified diff on success."
}

func (t *DeleteRangeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"File path"},
  "start_anchor":{"type":"string","description":"Exact text of the first line to delete (must be unique in the file)"},
  "end_anchor":{"type":"string","description":"Exact text of the last line to delete (must be unique in the file)"},
  "inclusive":{"type":"boolean","description":"Whether to include the anchor lines in the deletion (default true)"}
},
"required":["path","start_anchor","end_anchor"]
}`)
}

func (t *DeleteRangeTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path        string `json:"path"`
		StartAnchor string `json:"start_anchor"`
		EndAnchor   string `json:"end_anchor"`
		Inclusive   *bool  `json:"inclusive"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	wsRoot := fileutil.WorkspaceRoot()
	resolved, err := fileutil.ResolvePath(p.Path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p.Path, err)
	}
	if wsRoot != "" {
		if err := fileutil.ValidateWorkspaceBoundary(resolved, wsRoot); err != nil {
			return "", err
		}
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	// Anchors must be unique (per the schema contract) — otherwise we'd silently
	// re-anchor to the first occurrence and over-delete. Fail closed on ambiguity,
	// exactly like edit_file/multi_edit's applyReplace.
	startCount, endCount := 0, 0
	startLine, endLine := -1, -1
	for i, line := range lines {
		if line == p.StartAnchor {
			startCount++
			if startLine < 0 {
				startLine = i
			}
		}
		if line == p.EndAnchor {
			endCount++
			endLine = i
		}
	}
	if startCount == 0 {
		return "", fmt.Errorf("start_anchor not found")
	}
	if startCount > 1 {
		return "", fmt.Errorf("start_anchor matches %d lines (must be unique — add more surrounding context)", startCount)
	}
	if endCount == 0 {
		return "", fmt.Errorf("end_anchor not found")
	}
	if endCount > 1 {
		return "", fmt.Errorf("end_anchor matches %d lines (must be unique — add more surrounding context)", endCount)
	}
	if endLine < startLine {
		return "", fmt.Errorf("end_anchor appears before start_anchor")
	}

	inclusive := true
	if p.Inclusive != nil {
		inclusive = *p.Inclusive
	}

	delStart := startLine
	delEnd := endLine + 1
	if !inclusive {
		// Exclude BOTH anchor lines — delete only the lines between them.
		delStart = startLine + 1
		delEnd = endLine
		if delStart >= delEnd {
			return "", fmt.Errorf("nothing to delete between the anchors (with inclusive=false)")
		}
	}

	newLines := append(lines[:delStart], lines[delEnd:]...)
	newContent := strings.Join(newLines, "\n")

	if err := fileutil.SafeWriteFile(resolved, wsRoot, []byte(newContent)); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}

	deleted := delEnd - delStart
	return fmt.Sprintf("Deleted %d line(s) from %s (lines %d-%d)", deleted, p.Path, startLine+1, endLine+1), nil
}

func (t *DeleteRangeTool) Preview(ctx context.Context, args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path string `json:"path"`
	}
	json.Unmarshal(args, &p)
	data, _ := os.ReadFile(p.Path)
	return diff.Change{Path: p.Path, Before: string(data)}, nil
}
