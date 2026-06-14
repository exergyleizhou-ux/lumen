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
	tool.RegisterBuiltin(&ReadFileTool{})
}

// ReadFileTool reads a file from the workspace.
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string   { return "read_file" }
func (t *ReadFileTool) ReadOnly() bool { return true }

func (t *ReadFileTool) Description() string {
	return "Read a text file with optional line offset/limit. Output prefixes each line with its 1-based number so subsequent edit_file calls can target exact lines."
}

func (t *ReadFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"File path"},
  "offset":{"type":"integer","description":"0-based line offset to start reading from (default 0)"},
  "limit":{"type":"integer","description":"Maximum lines to return (default 2000)"}
},
"required":["path"]
}`)
}

func (t *ReadFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
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

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if p.Limit <= 0 {
		p.Limit = 2000
	}
	start := p.Offset
	if start < 0 {
		start = 0
	}
	end := start + p.Limit
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&sb, "%d→%s\n", i+1, lines[i])
	}
	return sb.String(), nil
}

// ── Previewer implementation ──────────────────────────────

func (t *ReadFileTool) Preview(args json.RawMessage) (diff.Change, bool) {
	return diff.Change{}, false // read-only
}
