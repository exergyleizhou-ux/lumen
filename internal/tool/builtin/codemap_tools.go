package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"lumen/internal/codemap"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&FindSymbolTool{})
	tool.RegisterBuiltin(&ListPackageSymbolsTool{})
	tool.RegisterBuiltin(&FindCallersTool{})
	tool.RegisterBuiltin(&FindCalleesTool{})
	tool.RegisterBuiltin(&GetCallGraphTool{})
	tool.RegisterBuiltin(&DetectCircularDepsTool{})
}

// ── Shared code map ─────────────────────────────────────────────────────────

var (
	codeMap     *codemap.Map
	codeMapOnce sync.Once
)

func getCodeMap() *codemap.Map {
	codeMapOnce.Do(func() {
		codeMap = codemap.NewMap()
	})
	return codeMap
}

// ── find_symbol ─────────────────────────────────────────────────────────────

type FindSymbolTool struct{}

func (t *FindSymbolTool) Name() string   { return "find_symbol" }
func (t *FindSymbolTool) ReadOnly() bool { return true }

func (t *FindSymbolTool) Description() string {
	return "Find a code symbol by name in the code map. Returns matching symbols with their package, kind, file, and line."
}

func (t *FindSymbolTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "name":{"type":"string","description":"Symbol name to search for (exact match on name portion)"}
},
"required":["name"]
}`)
}

func (t *FindSymbolTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Name == "" {
		return "", fmt.Errorf("name is required")
	}

	cm := getCodeMap()
	// FindByName scans all symbols matching on the name portion
	var matches []*codemap.Symbol
	for _, sym := range cm.Symbols() {
		if sym.Name == p.Name {
			matches = append(matches, sym)
		}
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No symbol found with name %q", p.Name), nil
	}

	var formatted []string
	for _, sym := range matches {
		formatted = append(formatted, codemap.FormatSymbol(sym))
	}

	out := map[string]interface{}{
		"query":   p.Name,
		"count":   len(matches),
		"symbols": formatted,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// ── list_package_symbols ────────────────────────────────────────────────────

type ListPackageSymbolsTool struct{}

func (t *ListPackageSymbolsTool) Name() string   { return "list_package_symbols" }
func (t *ListPackageSymbolsTool) ReadOnly() bool { return true }

func (t *ListPackageSymbolsTool) Description() string {
	return "List all symbols registered in a package. Provide the package name."
}

func (t *ListPackageSymbolsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "package":{"type":"string","description":"Package name to list symbols for"}
},
"required":["package"]
}`)
}

func (t *ListPackageSymbolsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Package string `json:"package"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Package == "" {
		return "", fmt.Errorf("package is required")
	}

	cm := getCodeMap()
	symbols := cm.PackageSymbols(p.Package)

	var formatted []string
	for _, sym := range symbols {
		formatted = append(formatted, codemap.FormatSymbol(sym))
	}

	out := map[string]interface{}{
		"package": p.Package,
		"count":   len(symbols),
		"symbols": formatted,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// ── find_callers ────────────────────────────────────────────────────────────

type FindCallersTool struct{}

func (t *FindCallersTool) Name() string   { return "find_callers" }
func (t *FindCallersTool) ReadOnly() bool { return true }

func (t *FindCallersTool) Description() string {
	return "Find all callers of a symbol (who calls this symbol). Provide the callee name."
}

func (t *FindCallersTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "callee":{"type":"string","description":"Name of the callee symbol"}
},
"required":["callee"]
}`)
}

func (t *FindCallersTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Callee string `json:"callee"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Callee == "" {
		return "", fmt.Errorf("callee is required")
	}

	cm := getCodeMap()
	callers := cm.Callers(p.Callee)

	out := map[string]interface{}{
		"callee":  p.Callee,
		"count":   len(callers),
		"callers": callers,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// ── find_callees ────────────────────────────────────────────────────────────

type FindCalleesTool struct{}

func (t *FindCalleesTool) Name() string   { return "find_callees" }
func (t *FindCalleesTool) ReadOnly() bool { return true }

func (t *FindCalleesTool) Description() string {
	return "Find all callees of a symbol (what this symbol calls). Provide the caller name."
}

func (t *FindCalleesTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "caller":{"type":"string","description":"Name of the caller symbol"}
},
"required":["caller"]
}`)
}

func (t *FindCalleesTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Caller string `json:"caller"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Caller == "" {
		return "", fmt.Errorf("caller is required")
	}

	cm := getCodeMap()
	callees := cm.Callees(p.Caller)

	out := map[string]interface{}{
		"caller":  p.Caller,
		"count":   len(callees),
		"callees": callees,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// ── get_call_graph ──────────────────────────────────────────────────────────

type GetCallGraphTool struct{}

func (t *GetCallGraphTool) Name() string   { return "get_call_graph" }
func (t *GetCallGraphTool) ReadOnly() bool { return true }

func (t *GetCallGraphTool) Description() string {
	return "Return the call graph in DOT format. Nodes are symbols; edges are calls between them."
}

func (t *GetCallGraphTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *GetCallGraphTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	cm := getCodeMap()
	return cm.FormatCallGraph(), nil
}

// ── detect_circular_deps ────────────────────────────────────────────────────

type DetectCircularDepsTool struct{}

func (t *DetectCircularDepsTool) Name() string   { return "detect_circular_deps" }
func (t *DetectCircularDepsTool) ReadOnly() bool { return true }

func (t *DetectCircularDepsTool) Description() string {
	return "Detect circular package dependencies in the code map. Returns any cycles found."
}

func (t *DetectCircularDepsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *DetectCircularDepsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	cm := getCodeMap()
	cycles := cm.CircularDeps()

	out := map[string]interface{}{
		"circular_dependencies": len(cycles) > 0,
		"count":                 len(cycles),
		"cycles":                cycles,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}
