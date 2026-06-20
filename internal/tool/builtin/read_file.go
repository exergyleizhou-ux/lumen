package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"lumen/internal/diff"
	"lumen/internal/fileutil"
	"lumen/internal/tool"
	"lumen/internal/untrusted"
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

	wsRoot := fileutil.WorkspaceRoot()
	content, _, _, err := fileutil.SafeReadFile(p.Path, wsRoot, p.Offset, p.Limit)
	if err == nil && untrusted.ReadsWrapped() {
		// Opt-in (LUMEN_UNTRUSTED_READS): treat file contents as untrusted when
		// working in a repo whose contents may carry injection payloads. Off by
		// default because wrapping interferes with exact-string edit workflows.
		content = untrusted.Wrap("file: "+p.Path, content)
	}
	return content, err
}

// ── Previewer implementation ──────────────────────────────

func (t *ReadFileTool) Preview(args json.RawMessage) (diff.Change, bool) {
	return diff.Change{}, false // read-only
}
