package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"lumen/internal/diff"
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

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}
	content := string(data)

	count := strings.Count(content, p.OldString)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", p.Path)
	}
	if count > 1 {
		return "", fmt.Errorf("old_string matches %d times in %s (must be unique — add surrounding context)", count, p.Path)
	}

	newContent := strings.Replace(content, p.OldString, p.NewString, 1)
	if err := os.WriteFile(p.Path, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	return fmt.Sprintf("Replaced 1 occurrence in %s", p.Path), nil
}

// ── Previewer implementation ──────────────────────────────

func (t *EditFileTool) Preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, err
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return diff.Change{}, err
	}
	before := string(data)
	return diff.Change{
		Path:   p.Path,
		Before: before,
		After:  strings.Replace(before, p.OldString, p.NewString, 1),
	}, nil
}
