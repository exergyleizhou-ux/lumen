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
	tool.RegisterBuiltin(&EditFileTool{})
}

// EditFileTool replaces an exact string in a file with another. It uses a
// uniqueness check so the old_string must match exactly once.
type EditFileTool struct{}

func (t *EditFileTool) Name() string   { return "edit_file" }
func (t *EditFileTool) ReadOnly() bool { return false }

func (t *EditFileTool) Description() string {
	return "Replace an exact string in a file with another. old_string must occur exactly once; add surrounding context to disambiguate. Use for targeted edits instead of rewriting the whole file."
}

func (t *EditFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"File path"},
  "old_string":{"type":"string","description":"Exact text to replace (must be unique in the file)"},
  "new_string":{"type":"string","description":"Replacement text (may be empty to delete)"}
},
"required":["path","old_string","new_string"]
}`)
}

func (t *EditFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
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
	if err := fileutil.ValidateReadSize(resolved); err != nil {
		return "", err
	}
	if binary, _ := fileutil.IsBinaryFile(resolved); binary {
		return "", fmt.Errorf("file appears to be binary")
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}
	content := string(data)

	newContent, err := applyReplace(content, p.OldString, p.NewString)
	if err != nil {
		return "", fmt.Errorf("%s: %w", p.Path, err)
	}
	if err := fileutil.SafeWriteFile(p.Path, wsRoot, []byte(newContent)); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	return fmt.Sprintf("Replaced 1 occurrence in %s", p.Path), nil
}

// ── Previewer implementation ──────────────────────────────

func (t *EditFileTool) Preview(ctx context.Context, args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, err
	}
	// Resolve the path the same way Execute does, so the diff isn't dropped and
	// the reported changed-file matches what Execute mutates (verify-after-edit).
	resolved, err := fileutil.ResolvePath(p.Path)
	if err != nil {
		return diff.Change{}, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return diff.Change{}, err
	}
	before := string(data)
	return diff.Change{
		Path:   resolved,
		Before: before,
		After:  strings.Replace(before, p.OldString, p.NewString, 1),
	}, nil
}
