package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/lsp"
	"lumen/internal/tool"
)

// ── Persistent gopls connection ────────────────────────────

var (
	goplsClient   *lsp.LSPClient
	goplsMu       sync.Mutex
	goplsStarted  bool
	goplsWorkspace string
	goplsDocs     = map[string]bool{} // tracked open documents
)

func getGopls(ctx context.Context) (*lsp.LSPClient, error) {
	goplsMu.Lock()
	defer goplsMu.Unlock()

	if goplsClient != nil && goplsStarted {
		return goplsClient, nil
	}

	wd, _ := os.Getwd()
	client, err := lsp.StartGopls(ctx, wd)
	if err != nil {
		return nil, err
	}
	goplsClient = client
	goplsStarted = true
	goplsWorkspace = wd
	return goplsClient, nil
}

func openInGopls(ctx context.Context, filePath string) error {
	client, err := getGopls(ctx)
	if err != nil {
		return err
	}

	abs, _ := filepath.Abs(filePath)
	uri := "file://" + abs
	if goplsDocs[uri] {
		return nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	if err := client.OpenDocument(uri, string(data)); err != nil {
		return fmt.Errorf("open: %w", err)
	}
	goplsMu.Lock()
	goplsDocs[uri] = true
	goplsMu.Unlock()

	// Brief pause for server-side analysis
	select {
	case <-ctx.Done():
	case <-time.After(200 * time.Millisecond):
	}
	return nil
}

func absURI(path string) string {
	abs, _ := filepath.Abs(path)
	return "file://" + abs
}

// ── Init ────────────────────────────────────────────────────

func init() {
	tool.RegisterBuiltin(&LSPDiagnosticTool{})
	tool.RegisterBuiltin(&LSPCompletionTool{})
	tool.RegisterBuiltin(&LSPHoverTool{})
	tool.RegisterBuiltin(&LSPDefinitionTool{})
	tool.RegisterBuiltin(&LSPReferencesTool{})
}

// ── Diagnostics ─────────────────────────────────────────────

type LSPDiagnosticTool struct{}
func (t *LSPDiagnosticTool) Name() string     { return "lsp_diagnostics" }
func (t *LSPDiagnosticTool) ReadOnly() bool   { return true }
func (t *LSPDiagnosticTool) Description() string {
	return "Real-time Go diagnostics via persistent gopls. Opens the file once, then returns errors/warnings with line numbers. Falls back to go vet."
}
func (t *LSPDiagnosticTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"}},"required":["file"]}`)
}
func (t *LSPDiagnosticTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	if p.File == "" { return "", fmt.Errorf("file is required") }

	// Priority 1: persistent gopls
	if err := openInGopls(ctx, p.File); err == nil {
		client, err2 := getGopls(ctx)
		if err2 == nil {
			diags, err3 := client.GetDiagnostics(ctx, absURI(p.File))
			if err3 == nil {
				if len(diags) == 0 {
					return fmt.Sprintf("0 issues in %s — clean.", p.File), nil
				}
				var sb strings.Builder
				fmt.Fprintf(&sb, "%d issue(s) in %s:\n", len(diags), p.File)
				for _, d := range diags {
					sev := "?"
					switch d.Severity {
					case 1: sev = "ERROR"
					case 2: sev = "WARN"
					case 3: sev = "INFO"
					case 4: sev = "HINT"
					}
					fmt.Fprintf(&sb, "  %s  L%d:%d  %s", sev, d.Range.Start.Line+1, d.Range.Start.Character+1, d.Message)
					if d.Code != "" { fmt.Fprintf(&sb, "  (%s)", d.Code) }
					sb.WriteByte('\n')
				}
				return sb.String(), nil
			}
		}
	}

	// Priority 2: go vet
	pkg := filepath.Dir(p.File)
	cmd := exec.CommandContext(ctx, "go", "vet", pkg)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		return fmt.Sprintf("%s\n%s", p.File, string(out)), nil
	}
	if err != nil && len(out) == 0 {
		return "", fmt.Errorf("go vet: %w", err)
	}
	return fmt.Sprintf("0 issues in %s — clean.", p.File), nil
}

// ── Completion ─────────────────────────────────────────────

type LSPCompletionTool struct{}
func (t *LSPCompletionTool) Name() string     { return "lsp_completion" }
func (t *LSPCompletionTool) ReadOnly() bool   { return true }
func (t *LSPCompletionTool) Description() string {
	return "Code completion via persistent gopls. Shows suggestions at a file position with type info."
}
func (t *LSPCompletionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"column":{"type":"integer"}},"required":["file","line","column"]}`)
}
func (t *LSPCompletionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line, Column int }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	if err := openInGopls(ctx, p.File); err == nil {
		client, _ := getGopls(ctx)
		if client != nil {
			items, err := client.GetCompletion(ctx, absURI(p.File), p.Line, p.Column)
			if err == nil && len(items) > 0 {
				var sb strings.Builder
				fmt.Fprintf(&sb, "%d completion(s) at L%d:%d:\n", len(items), p.Line, p.Column)
				for i, it := range items {
					if i >= 20 { fmt.Fprintf(&sb, "  ... +%d more\n", len(items)-20); break }
					kind := "?"
					switch it.Kind {
					case 3: kind = "func"
					case 5: kind = "field"
					case 6: kind = "var"
					case 9: kind = "pkg"
					case 14: kind = "kw"
					}
					fmt.Fprintf(&sb, "  [%s] %s", kind, it.Label)
					if it.Detail != "" { fmt.Fprintf(&sb, " — %s", it.Detail) }
					sb.WriteByte('\n')
				}
				return sb.String(), nil
			}
		}
	}
	return fmt.Sprintf("No completion data at %s:%d:%d. Install gopls for full LSP.", p.File, p.Line, p.Column), nil
}

// ── Hover ───────────────────────────────────────────────────

type LSPHoverTool struct{}
func (t *LSPHoverTool) Name() string    { return "lsp_hover" }
func (t *LSPHoverTool) ReadOnly() bool  { return true }
func (t *LSPHoverTool) Description() string {
	return "Type info via persistent gopls hover. Falls back to go doc."
}
func (t *LSPHoverTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"column":{"type":"integer"}},"required":["file","line","column"]}`)
}
func (t *LSPHoverTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line, Column int }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	if err := openInGopls(ctx, p.File); err == nil {
		client, _ := getGopls(ctx)
		if client != nil {
			hover, err := client.GetHover(ctx, absURI(p.File), p.Line, p.Column)
			if err == nil && hover != nil && hover.Contents != "" {
				return hover.Contents, nil
			}
		}
	}
	return fmt.Sprintf("No hover info at %s:%d:%d", p.File, p.Line, p.Column), nil
}

// ── Definition ──────────────────────────────────────────────

type LSPDefinitionTool struct{}
func (t *LSPDefinitionTool) Name() string     { return "lsp_definition" }
func (t *LSPDefinitionTool) ReadOnly() bool   { return true }
func (t *LSPDefinitionTool) Description() string {
	return "Jump-to-definition via persistent gopls. Returns file:line location."
}
func (t *LSPDefinitionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"column":{"type":"integer"}},"required":["file","line","column"]}`)
}
func (t *LSPDefinitionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line, Column int }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	if err := openInGopls(ctx, p.File); err == nil {
		client, _ := getGopls(ctx)
		if client != nil {
			locs, err := client.GetDefinition(ctx, absURI(p.File), p.Line, p.Column)
			if err == nil && len(locs) > 0 {
				var sb strings.Builder
				for _, loc := range locs {
					path := strings.TrimPrefix(loc.URI, "file://")
					fmt.Fprintf(&sb, "%s:%d:%d\n", path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
				}
				return sb.String(), nil
			}
		}
	}
	return fmt.Sprintf("No definition at %s:%d:%d", p.File, p.Line, p.Column), nil
}

// ── References ──────────────────────────────────────────────

type LSPReferencesTool struct{}
func (t *LSPReferencesTool) Name() string     { return "lsp_references" }
func (t *LSPReferencesTool) ReadOnly() bool   { return true }
func (t *LSPReferencesTool) Description() string {
	return "Find-all-references via persistent gopls. Lists every usage site across the project."
}
func (t *LSPReferencesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"column":{"type":"integer"}},"required":["file","line","column"]}`)
}
func (t *LSPReferencesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line, Column int }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	if err := openInGopls(ctx, p.File); err == nil {
		client, _ := getGopls(ctx)
		if client != nil {
			refs, err := client.GetReferences(ctx, absURI(p.File), p.Line, p.Column, true)
			if err == nil {
				var sb strings.Builder
				fmt.Fprintf(&sb, "%d reference(s):\n", len(refs))
				for i, r := range refs {
					if i >= 50 { fmt.Fprintf(&sb, "  ... +%d more\n", len(refs)-50); break }
					path := strings.TrimPrefix(r.URI, "file://")
					fmt.Fprintf(&sb, "  %s L%d\n", path, r.Range.Start.Line+1)
				}
				return sb.String(), nil
			}
		}
	}
	return fmt.Sprintf("No references at %s:%d:%d", p.File, p.Line, p.Column), nil
}
