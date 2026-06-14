package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"lumen/internal/diff"
	"lumen/internal/fileutil"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&WriteFileTool{})
}

// WriteFileTool writes content to a file, overwriting existing content.
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string   { return "write_file" }
func (t *WriteFileTool) ReadOnly() bool { return false }

func (t *WriteFileTool) Description() string {
	return "Write content to a file at the given path (overwriting existing content). Creates parent directories as needed."
}

func (t *WriteFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"File path"},
  "content":{"type":"string","description":"Full content to write"}
},
"required":["path","content"]
}`)
}

func (t *WriteFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	wsRoot := fileutil.WorkspaceRoot()
	if err := fileutil.SafeWriteFile(p.Path, wsRoot, []byte(p.Content)); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}

// ── Previewer implementation ──────────────────────────────

func (t *WriteFileTool) Preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, err
	}
	var before string
	if data, err := os.ReadFile(p.Path); err == nil {
		before = string(data)
	}
	return diff.Change{
		Path:   p.Path,
		Before: before,
		After:  p.Content,
		New:    before == "",
	}, nil
}
