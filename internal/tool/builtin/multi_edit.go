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
	tool.RegisterBuiltin(&MultiEditTool{})
}

// MultiEditTool applies multiple old_string→new_string replacements to one
// file, in order. Each edit must match exactly once at the moment it is applied.
type MultiEditTool struct{}

func (t *MultiEditTool) Name() string   { return "multi_edit" }
func (t *MultiEditTool) ReadOnly() bool { return false }

func (t *MultiEditTool) Description() string {
	return "Apply multiple old_string→new_string replacements to one file, in order. Each edit must match exactly once at the moment it is applied."
}

func (t *MultiEditTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"File path"},
  "edits":{"type":"array","items":{"type":"object","properties":{"old_string":{"type":"string","description":"Exact text to replace (must be unique in the current state of the file)"},"new_string":{"type":"string","description":"Replacement text (may be empty to delete)"}},"required":["old_string","new_string"]},"description":"Ordered list of edits to apply"}
},
"required":["path","edits"]
}`)
}

func (t *MultiEditTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path  string `json:"path"`
		Edits []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if len(p.Edits) == 0 {
		return "", fmt.Errorf("edits is required")
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

	// Apply every edit to an in-memory copy; only persist if ALL succeed, so a
	// mid-sequence failure leaves the file untouched (atomic).
	applied := 0
	for i, edit := range p.Edits {
		next, err := applyReplace(content, edit.OldString, edit.NewString)
		if err != nil {
			return "", fmt.Errorf("edit %d (%s): %w", i, p.Path, err)
		}
		content = next
		applied++
	}

	if err := fileutil.SafeWriteFile(p.Path, wsRoot, []byte(content)); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	return fmt.Sprintf("Applied %d edits to %s", applied, p.Path), nil
}

// ── Previewer implementation ──────────────────────────────

func (t *MultiEditTool) Preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path  string `json:"path"`
		Edits []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, err
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return diff.Change{}, err
	}
	before := string(data)
	after := before
	for _, edit := range p.Edits {
		after = strings.Replace(after, edit.OldString, edit.NewString, 1)
	}
	return diff.Change{
		Path:   p.Path,
		Before: before,
		After:  after,
	}, nil
}
