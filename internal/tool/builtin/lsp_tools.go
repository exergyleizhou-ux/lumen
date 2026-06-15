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

	"lumen/internal/lsp"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&LSPDiagnosticsTool{})
	tool.RegisterBuiltin(&LSPDefinitionTool{})
	tool.RegisterBuiltin(&LSPReferencesTool{})
	tool.RegisterBuiltin(&LSPHoverTool{})
}

// ── Shared LSP client ─────────────────────────────────────

var (
	lspClient   *lsp.Client
	lspClientMu sync.Mutex
	lspErr      error
)

func getLSPClient() *lsp.Client {
	lspClientMu.Lock()
	defer lspClientMu.Unlock()

	if lspClient != nil {
		return lspClient
	}
	if lspErr != nil {
		return nil
	}

	// Find gopls or any available LSP
	cmd, args := findLSP()
	if cmd == "" {
		lspErr = fmt.Errorf("no LSP server found — install gopls, rust-analyzer, or typescript-language-server")
		return nil
	}

	root := workspaceRoot()
	client, err := lsp.StartClient(cmd, args, "file://"+root)
	if err != nil {
		lspErr = fmt.Errorf("lsp start: %w", err)
		return nil
	}

	lspClient = client
	return lspClient
}

func findLSP() (string, []string) {
	for _, c := range []string{"gopls", "rust-analyzer", "typescript-language-server"} {
		if p, err := exec.LookPath(c); err == nil {
			if c == "typescript-language-server" {
				return p, []string{"--stdio"}
			}
			return p, nil
		}
	}
	return "", nil
}

func workspaceRoot() string {
	wd, _ := os.Getwd()
	for dir := wd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return dir
		}
	}
	return wd
}

func fileToURI(path string) string {
	if filepath.IsAbs(path) {
		return "file://" + path
	}
	abs, _ := filepath.Abs(path)
	return "file://" + abs
}

// ── lsp_diagnostics ──────────────────────────────────────

type LSPDiagnosticsTool struct{}

func (t *LSPDiagnosticsTool) Name() string   { return "lsp_diagnostics" }
func (t *LSPDiagnosticsTool) ReadOnly() bool { return true }

func (t *LSPDiagnosticsTool) Description() string {
	return "Report compiler/linter diagnostics (errors, warnings) for a file from its language server. Use after editing to check the change compiles."
}

func (t *LSPDiagnosticsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string","description":"Path to the source file, relative to the workspace root or absolute."}},"required":["file"]}`)
}

func (t *LSPDiagnosticsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.File == "" {
		return "", fmt.Errorf("file is required")
	}

	c := getLSPClient()
	if c == nil {
		return "", lspErr
	}

	diags, err := c.RequestDiagnostics(ctx, p.File)
	if err != nil {
		return "", fmt.Errorf("diagnostics: %w", err)
	}
	if len(diags) == 0 {
		return fmt.Sprintf("no diagnostics for %s (clean)", p.File), nil
	}

	var sb strings.Builder
	for _, d := range diags {
		sev := "?"
		switch d.Severity {
		case 1:
			sev = "error"
		case 2:
			sev = "warning"
		case 3:
			sev = "info"
		case 4:
			sev = "hint"
		}
		fmt.Fprintf(&sb, "%s:%d:%d: %s: %s\n",
			p.File, d.Range.Start.Line+1, d.Range.Start.Character+1, sev, d.Message)
	}
	return sb.String(), nil
}

// ── lsp_definition ───────────────────────────────────────

type LSPDefinitionTool struct{}

func (t *LSPDefinitionTool) Name() string   { return "lsp_definition" }
func (t *LSPDefinitionTool) ReadOnly() bool { return true }

func (t *LSPDefinitionTool) Description() string {
	return "Jump to where a symbol is defined. Give the file, the 1-based line the symbol appears on, and the symbol text itself."
}

func (t *LSPDefinitionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string","description":"Path to the source file"},"line":{"type":"integer","description":"1-based line number"},"symbol":{"type":"string","description":"The exact symbol text on that line"}},"required":["file","line","symbol"]}`)
}

func (t *LSPDefinitionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	c := getLSPClient()
	if c == nil {
		return "", lspErr
	}

	locs, err := c.Definition(ctx, p.File, p.Line-1, 0)
	if err != nil {
		return "", fmt.Errorf("definition: %w", err)
	}
	if len(locs) == 0 {
		return "", fmt.Errorf("no definition found for %q at %s:%d", p.Symbol, p.File, p.Line)
	}

	var sb strings.Builder
	for _, loc := range locs {
		path := strings.TrimPrefix(loc.URI, "file://")
		fmt.Fprintf(&sb, "%s:%d:%d\n", path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	return sb.String(), nil
}

// ── lsp_references ───────────────────────────────────────

type LSPReferencesTool struct{}

func (t *LSPReferencesTool) Name() string   { return "lsp_references" }
func (t *LSPReferencesTool) ReadOnly() bool { return true }

func (t *LSPReferencesTool) Description() string {
	return "List every reference to a symbol across the workspace."
}

func (t *LSPReferencesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"symbol":{"type":"string"}},"required":["file","line","symbol"]}`)
}

func (t *LSPReferencesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	c := getLSPClient()
	if c == nil {
		return "", lspErr
	}

	locs, err := c.References(ctx, p.File, p.Line-1, 0)
	if err != nil {
		return "", fmt.Errorf("references: %w", err)
	}
	if len(locs) == 0 {
		return "", fmt.Errorf("no references found for %q at %s:%d", p.Symbol, p.File, p.Line)
	}

	var sb strings.Builder
	for _, loc := range locs {
		path := strings.TrimPrefix(loc.URI, "file://")
		fmt.Fprintf(&sb, "%s:%d:%d\n", path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	fmt.Fprintf(&sb, "\n%d reference(s)", len(locs))
	return sb.String(), nil
}

// ── lsp_hover ────────────────────────────────────────────

type LSPHoverTool struct{}

func (t *LSPHoverTool) Name() string   { return "lsp_hover" }
func (t *LSPHoverTool) ReadOnly() bool { return true }

func (t *LSPHoverTool) Description() string {
	return "Show the type signature and documentation for a symbol. Give the file, the 1-based line, and the symbol text."
}

func (t *LSPHoverTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"symbol":{"type":"string"}},"required":["file","line","symbol"]}`)
}

func (t *LSPHoverTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	c := getLSPClient()
	if c == nil {
		return "", lspErr
	}

	hover, err := c.Hover(ctx, p.File, p.Line-1, 0)
	if err != nil {
		return "", fmt.Errorf("hover: %w", err)
	}
	if hover.Contents == "" {
		return "(no hover information)", nil
	}
	return hover.Contents, nil
}
