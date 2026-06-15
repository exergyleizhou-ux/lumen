package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"lumen/internal/lsp"
	"lumen/internal/tool"
)

// ── Shared LSP connection (lazy, long-lived) ──────────────────────

var (
	lspClient   *lsp.LSPClient
	lspClientMu sync.Mutex
	lspConnected bool
)

func getLSPClient(ctx context.Context) (*lsp.LSPClient, error) {
	lspClientMu.Lock()
	defer lspClientMu.Unlock()

	if lspClient != nil && lspConnected {
		return lspClient, nil
	}

	workspace, _ := os.Getwd()
	if workspace == "" {
		workspace = "."
	}

	client, err := lsp.StartGopls(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("gopls: %w", err)
	}
	lspClient = client
	lspConnected = true
	return lspClient, nil
}

func init() {
	tool.RegisterBuiltin(&LSPRealDiagnosticTool{})
	tool.RegisterBuiltin(&LSPRealCompletionTool{})
	tool.RegisterBuiltin(&LSPRealHoverTool{})
	tool.RegisterBuiltin(&LSPRealDefinitionTool{})
	tool.RegisterBuiltin(&LSPRealReferencesTool{})
}

type LSPRealDiagnosticTool struct{}
func (t *LSPRealDiagnosticTool) Name() string        { return "lsp_diagnostics" }
func (t *LSPRealDiagnosticTool) ReadOnly() bool      { return true }
func (t *LSPRealDiagnosticTool) Description() string {
	return "Get real-time compiler/linter diagnostics for a Go file using a persistent gopls connection. Opens or reuses the document."
}
func (t *LSPRealDiagnosticTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string","description":"Path to the .go file"}},"required":["file"]}`)
}
func (t *LSPRealDiagnosticTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	if p.File == "" { return "", fmt.Errorf("file is required") }

	client, err := getLSPClient(ctx)
	if err != nil { return "", err }

	absolutePath := p.File
	if !strings.HasPrefix(p.File, "/") {
		wd, _ := os.Getwd()
		absolutePath = wd + "/" + p.File
	}
	uri := "file://" + absolutePath

	// Read file and open it in gopls
	data, err := os.ReadFile(p.File)
	if err != nil { return "", fmt.Errorf("read %s: %w", p.File, err) }

	if err := client.OpenDocument(uri, string(data)); err != nil {
		return "", fmt.Errorf("open document: %w", err)
	}
	time.Sleep(500 * time.Millisecond) // wait for gopls to analyze

	diags, err := client.GetDiagnostics(ctx, uri)
	if err != nil { return "", fmt.Errorf("diagnostics: %w", err) }

	if len(diags) == 0 {
		return "No diagnostics found. File is clean.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d issue(s) in %s:\n", len(diags), p.File)
	for _, d := range diags {
		sev := "INFO"
		switch d.Severity {
		case 1: sev = "ERROR"
		case 2: sev = "WARN"
		case 3: sev = "HINT"
		}
		fmt.Fprintf(&sb, "  [%s] L%d:%d — %s", sev, d.Range.Start.Line+1, d.Range.Start.Character+1, d.Message)
		if d.Code != "" { fmt.Fprintf(&sb, " (%s)", d.Code) }
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

type LSPRealCompletionTool struct{}
func (t *LSPRealCompletionTool) Name() string     { return "lsp_completion" }
func (t *LSPRealCompletionTool) ReadOnly() bool   { return true }
func (t *LSPRealCompletionTool) Description() string {
	return "Get code completion suggestions at a specific line/column in a Go file. Uses a persistent gopls connection."
}
func (t *LSPRealCompletionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"column":{"type":"integer"}},"required":["file","line","column"]}`)
}
func (t *LSPRealCompletionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line, Column int }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	client, err := getLSPClient(ctx)
	if err != nil { return "", err }

	uri := "file://" + absPath(p.File)
	items, err := client.GetCompletion(ctx, uri, p.Line, p.Column)
	if err != nil { return "", fmt.Errorf("completion: %w", err) }

	if len(items) == 0 { return "No completion suggestions at this position.", nil }

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d completion(s) at %s:%d:%d:\n", len(items), p.File, p.Line, p.Column)
	for i, item := range items {
		if i >= 20 { fmt.Fprintf(&sb, "  ... and %d more\n", len(items)-20); break }
		kind := "?"
		switch item.Kind {
		case 3: kind = "func"
		case 5: kind = "field"
		case 6: kind = "var"
		case 9: kind = "module"
		case 14: kind = "keyword"
		}
		fmt.Fprintf(&sb, "  [%s] %s", kind, item.Label)
		if item.Detail != "" { fmt.Fprintf(&sb, " — %s", item.Detail) }
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

type LSPRealHoverTool struct{}
func (t *LSPRealHoverTool) Name() string       { return "lsp_hover" }
func (t *LSPRealHoverTool) ReadOnly() bool     { return true }
func (t *LSPRealHoverTool) Description() string {
	return "Show type signature and documentation for a symbol at a specific position in a Go file."
}
func (t *LSPRealHoverTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"column":{"type":"integer"}},"required":["file","line","column"]}`)
}
func (t *LSPRealHoverTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line, Column int }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	client, err := getLSPClient(ctx)
	if err != nil { return "", err }
	hover, err := client.GetHover(ctx, "file://"+absPath(p.File), p.Line, p.Column)
	if err != nil { return "", fmt.Errorf("hover: %w", err) }
	if hover.Contents == "" { return "No hover info at this position.", nil }
	return hover.Contents, nil
}

type LSPRealDefinitionTool struct{}
func (t *LSPRealDefinitionTool) Name() string     { return "lsp_definition" }
func (t *LSPRealDefinitionTool) ReadOnly() bool   { return true }
func (t *LSPRealDefinitionTool) Description() string {
	return "Jump to the definition of a symbol at a specific line/column. Returns file path and location."
}
func (t *LSPRealDefinitionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"symbol":{"type":"string","description":"The symbol text on that line"}},"required":["file","line","symbol"]}`)
}
func (t *LSPRealDefinitionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line int; Symbol string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	client, err := getLSPClient(ctx)
	if err != nil { return "", err }

	loc, err := client.GetDefinition(ctx, "file://"+absPath(p.File), p.Line, 0)
	if err != nil { return "", fmt.Errorf("definition: %w", err) }

	var sb strings.Builder
	fmt.Fprintf(&sb, "Definition of %q:\n", p.Symbol)
	for _, l := range loc {
		fmt.Fprintf(&sb, "  %s L%d:%d\n", strings.TrimPrefix(l.URI, "file://"), l.Range.Start.Line+1, l.Range.Start.Character+1)
	}
	return sb.String(), nil
}

type LSPRealReferencesTool struct{}
func (t *LSPRealReferencesTool) Name() string     { return "lsp_references" }
func (t *LSPRealReferencesTool) ReadOnly() bool   { return true }
func (t *LSPRealReferencesTool) Description() string {
	return "List every reference to a symbol across the project."
}
func (t *LSPRealReferencesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"symbol":{"type":"string"}},"required":["file","line","symbol"]}`)
}
func (t *LSPRealReferencesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line int; Symbol string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	client, err := getLSPClient(ctx)
	if err != nil { return "", err }

	refs, err := client.GetReferences(ctx, "file://"+absPath(p.File), p.Line, 0, true)
	if err != nil { return "", fmt.Errorf("references: %w", err) }

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d reference(s) to %q:\n", len(refs), p.Symbol)
	for i, r := range refs {
		if i >= 50 { fmt.Fprintf(&sb, "  ... and %d more\n", len(refs)-50); break }
		fmt.Fprintf(&sb, "  %s L%d\n", strings.TrimPrefix(r.URI, "file://"), r.Range.Start.Line+1)
	}
	return sb.String(), nil
}

func absPath(path string) string {
	if strings.HasPrefix(path, "/") { return path }
	wd, _ := os.Getwd()
	if wd == "" { return "/" + path }
	return wd + "/" + path
}
