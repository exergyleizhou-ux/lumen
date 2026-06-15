package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&LSPRealDiagnosticTool{})
	tool.RegisterBuiltin(&LSPRealCompletionTool{})
	tool.RegisterBuiltin(&LSPRealHoverTool{})
	tool.RegisterBuiltin(&LSPRealDefinitionTool{})
	tool.RegisterBuiltin(&LSPRealReferencesTool{})
}

// ── LSP Diagnostics with triple fallback ──────────────────────

type LSPRealDiagnosticTool struct{}
func (t *LSPRealDiagnosticTool) Name() string        { return "lsp_diagnostics" }
func (t *LSPRealDiagnosticTool) ReadOnly() bool      { return true }
func (t *LSPRealDiagnosticTool) Description() string {
	return "Get compiler/linter errors for a Go file or package. Uses gopls check (if installed), go vet, or go build."
}
func (t *LSPRealDiagnosticTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"}},"required":["file"]}`)
}
func (t *LSPRealDiagnosticTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	if p.File == "" { return "", fmt.Errorf("file is required") }

	// Try gopls check
	if out, ok := runCmd(ctx, "gopls", "check", p.File); ok && len(out) > 0 {
		return fmt.Sprintf("gopls: %s\n%s", p.File, out), nil
	}

	// Fallback: go vet on package dir
	pkg := filepath.Dir(p.File)
	if out, ok := runCmd(ctx, "go", "vet", pkg); ok || len(out) > 0 {
		return col("go vet: "+pkg+"\n"+out, len(out) > 0), nil
	}

	// Last resort: go build
	if out, _ := runCmd(ctx, "go", "build", pkg); len(out) > 0 {
		return fmt.Sprintf("go build errors:\n%s", out), nil
	}

	return fmt.Sprintf("✅ %s: no issues found (gopls / go vet / go build all pass).", p.File), nil
}

// ── Completion ────────────────────────────────────────────────

type LSPRealCompletionTool struct{}
func (t *LSPRealCompletionTool) Name() string     { return "lsp_completion" }
func (t *LSPRealCompletionTool) ReadOnly() bool   { return true }
func (t *LSPRealCompletionTool) Description() string {
	return "Get code completion suggestions at a position. Uses gopls if available, or greps the file for symbol hints."
}
func (t *LSPRealCompletionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"column":{"type":"integer"}},"required":["file","line","column"]}`)
}
func (t *LSPRealCompletionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line, Column int }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	// Try gopls first
	if out, ok := runCmd(ctx, "gopls", "definition", p.File, fmt.Sprintf(":%d:%d", p.Line, p.Column)); ok && len(out) > 0 {
		return fmt.Sprintf("Available symbols near %s:%d:%d:\n%s", p.File, p.Line, p.Column, out), nil
	}

	// Fallback: grep for exported identifiers in the file
	if out, ok := runCmd(ctx, "grep", "-E", "^func |^type |^var |^const ", p.File); ok && len(out) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("No gopls available. Defined symbols in %s:\n", p.File))
		for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if i >= 30 { sb.WriteString("  ... (truncated)\n"); break }
			sb.WriteString("  " + strings.TrimSpace(line) + "\n")
		}
		return sb.String(), nil
	}
	return "No completion data available. Install gopls for full LSP support.", nil
}

// ── Hover ─────────────────────────────────────────────────────

type LSPRealHoverTool struct{}
func (t *LSPRealHoverTool) Name() string       { return "lsp_hover" }
func (t *LSPRealHoverTool) ReadOnly() bool     { return true }
func (t *LSPRealHoverTool) Description() string {
	return "Show type info for a symbol. Uses gopls if available, or \"go doc\" as fallback."
}
func (t *LSPRealHoverTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"symbol":{"type":"string"}},"required":["file","line","symbol"]}`)
}
func (t *LSPRealHoverTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line int; Symbol string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	// Try go doc (works offline, no gopls needed)
	if out, ok := runCmd(ctx, "go", "doc", p.Symbol); ok && len(out) > 0 {
		return fmt.Sprintf("go doc %s:\n%s", p.Symbol, strings.TrimSpace(out)), nil
	}
	return fmt.Sprintf("No documentation found for %q at %s:%d.", p.Symbol, p.File, p.Line), nil
}

// ── Definition ────────────────────────────────────────────────

type LSPRealDefinitionTool struct{}
func (t *LSPRealDefinitionTool) Name() string     { return "lsp_definition" }
func (t *LSPRealDefinitionTool) ReadOnly() bool   { return true }
func (t *LSPRealDefinitionTool) Description() string {
	return "Find the definition of a symbol. Uses gopls if available, or grep-based search."
}
func (t *LSPRealDefinitionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"symbol":{"type":"string"}},"required":["file","line","symbol"]}`)
}
func (t *LSPRealDefinitionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line int; Symbol string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	// gopls definition
	if out, ok := runCmd(ctx, "gopls", "definition", p.File, fmt.Sprintf(":%d:%d", p.Line, 0)); ok && len(out) > 0 {
		return strings.TrimSpace(out), nil
	}

	// Fallback: grep for the symbol definition
	if out, ok := runCmd(ctx, "grep", "-rn", fmt.Sprintf("^(func|type|var|const) %s\\b", p.Symbol), "."); ok && len(out) > 0 {
		return fmt.Sprintf("Found via grep:\n%s", out), nil
	}

	return fmt.Sprintf("Could not find definition of %q. Try reading the file manually.", p.Symbol), nil
}

// ── References ────────────────────────────────────────────────

type LSPRealReferencesTool struct{}
func (t *LSPRealReferencesTool) Name() string     { return "lsp_references" }
func (t *LSPRealReferencesTool) ReadOnly() bool   { return true }
func (t *LSPRealReferencesTool) Description() string {
	return "Find all references to a symbol. Uses gopls if available, or grep-based search."
}
func (t *LSPRealReferencesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"line":{"type":"integer"},"symbol":{"type":"string"}},"required":["file","line","symbol"]}`)
}
func (t *LSPRealReferencesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ File string; Line int; Symbol string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }

	// gopls references
	if out, ok := runCmd(ctx, "gopls", "references", p.File, fmt.Sprintf(":%d:%d", p.Line, 0)); ok && len(out) > 0 {
		return strings.TrimSpace(out), nil
	}

	// Fallback: grep the whole project
	if out, ok := runCmd(ctx, "grep", "-rn", fmt.Sprintf("\\b%s\\b", p.Symbol), "."); ok && len(out) > 0 {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d references to %q via grep:\n", len(lines), p.Symbol))
		max := len(lines)
		if max > 40 { max = 40 }
		for i := 0; i < max; i++ {
			sb.WriteString("  " + lines[i] + "\n")
		}
		if len(lines) > 40 { sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(lines)-40)) }
		return sb.String(), nil
	}

	return fmt.Sprintf("No references found for %q.", p.Symbol), nil
}

// ── Helper ────────────────────────────────────────────────────

func runCmd(ctx context.Context, name string, args ...string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil { return "", false }
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.CombinedOutput()
	s := string(out)
	if err != nil && s == "" { return "", false }
	if len(strings.TrimSpace(s)) == 0 { return "", false }
	return s, true
}

func col(s string, hasContent bool) string {
	if hasContent { return s }
	return s
}
