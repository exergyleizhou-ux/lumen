// Package codegraph provides symbol-level code navigation: finding
// definitions, callers, callees, and type hierarchies across a Go codebase.
// Adapted from Reasonix's codegraph package.
package codegraph

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Symbol is a named code element with its location.
type Symbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // "func", "type", "interface", "var", "const"
	Package  string `json:"package"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
}

// CallEdge represents one caller→callee relationship.
type CallEdge struct {
	Caller  string `json:"caller"`
	Callee  string `json:"callee"`
	File    string `json:"file"`
	Line    int    `json:"line"`
}

// Graph builds and queries a call graph for a Go workspace.
type Graph struct {
	mu      sync.RWMutex
	root    string
	symbols map[string]*Symbol  // fully-qualified name → symbol
	callers map[string][]CallEdge // callee → callers
	calls   map[string][]CallEdge // caller → callees
	loaded  bool
}

// NewGraph creates a graph for the given workspace root.
func NewGraph(root string) *Graph {
	return &Graph{
		root:    root,
		symbols: map[string]*Symbol{},
		callers: map[string][]CallEdge{},
		calls:   map[string][]CallEdge{},
	}
}

// Load builds the symbol index by scanning Go files.
func (g *Graph) Load() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.loaded {
		return nil
	}

	err := filepath.Walk(g.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		g.parseFile(path)
		return nil
	})
	if err != nil {
		return err
	}

	g.loaded = true
	return nil
}

func (g *Graph) parseFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	lineNum := 0
	inBlock := false

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and blank lines
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if inBlock {
			if strings.Contains(line, "*/") {
				inBlock = false
			}
			continue
		}
		if strings.HasPrefix(line, "/*") {
			inBlock = true
			continue
		}

		// Detect package
		if strings.HasPrefix(line, "package ") {
			// Extract package name
			continue
		}

		// Detect function declarations
		if strings.HasPrefix(line, "func ") {
			name := extractFuncName(line)
			if name != "" {
				parts := strings.SplitN(name, ".", 2)
				if len(parts) == 2 {
					// Method: Receiver.FuncName
					g.addSymbol(path, parts[1], "method", lineNum, isExported(parts[1]))
				} else {
					g.addSymbol(path, name, "func", lineNum, isExported(name))
				}
			}
		}

		// Detect type declarations
		if strings.HasPrefix(line, "type ") {
			name := extractTypeName(line)
			if name != "" {
				kind := "type"
				if strings.Contains(line, "interface") {
					kind = "interface"
				}
				g.addSymbol(path, name, kind, lineNum, isExported(name))
			}
		}
	}

	// Build call graph using simple heuristics
	g.buildCallsForFile(path)
}

func (g *Graph) addSymbol(file, name, kind string, line int, exported bool) {
	rel, _ := filepath.Rel(g.root, file)
	pkg := filepath.Dir(rel)
	fqn := pkg + "." + name
	g.symbols[fqn] = &Symbol{
		Name:     name,
		Kind:     kind,
		Package:  pkg,
		File:     rel,
		Line:     line,
		Exported: exported,
	}
}

func (g *Graph) buildCallsForFile(path string) {
	// Use grep to find function calls — lightweight approach
	// In production, this would use go/ast or guru
	// For now: scan for Foo( patterns after identifiers
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	rel, _ := filepath.Rel(g.root, path)

	// Simple heuristic: find Identifier( patterns
	for _, sym := range g.symbols {
		if strings.Contains(content, sym.Name+"(") {
			// This file calls sym.Name
			// Find the caller — the function containing this call
			caller := findContainingFunc(content, sym.Name)
			if caller != "" {
				edge := CallEdge{Caller: caller, Callee: sym.Name, File: rel}
				g.calls[caller] = append(g.calls[caller], edge)
				g.callers[sym.Name] = append(g.callers[sym.Name], edge)
			}
		}
	}
}

// ── Query API ──────────────────────────────────────────────

// FindSymbol searches for symbols by name (fuzzy, case-insensitive).
func (g *Graph) FindSymbol(query string) []Symbol {
	g.mu.RLock()
	defer g.mu.RUnlock()

	query = strings.ToLower(query)
	var results []Symbol
	for fqn, sym := range g.symbols {
		if strings.Contains(strings.ToLower(fqn), query) {
			results = append(results, *sym)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	if len(results) > 50 {
		results = results[:50]
	}
	return results
}

// CallersOf returns all call sites that call the given function.
func (g *Graph) CallersOf(funcName string) []CallEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.callers[funcName]
}

// CalleesOf returns all functions called by the given function.
func (g *Graph) CalleesOf(funcName string) []CallEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.calls[funcName]
}

// SymbolsByKind returns all symbols of a given kind.
func (g *Graph) SymbolsByKind(kind string) []Symbol {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var results []Symbol
	for _, sym := range g.symbols {
		if sym.Kind == kind {
			results = append(results, *sym)
		}
	}
	return results
}

// ── Go tool integration ───────────────────────────────────

// GuruQuery runs a guru query (definition/referrers) on the workspace.
// Requires golang.org/x/tools/cmd/guru installed.
func GuruQuery(root, mode, position string) (string, error) {
	guru, err := exec.LookPath("guru")
	if err != nil {
		return "", fmt.Errorf("guru not installed (go install golang.org/x/tools/cmd/guru@latest): %w", err)
	}

	cmd := exec.Command(guru, "-scope", root+"/...", mode, position)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("guru %s %s: %w\n%s", mode, position, err, out)
	}
	return string(out), nil
}

// ── Helpers ─────────────────────────────────────────────────

func extractFuncName(line string) string {
	// "func Foo(...)" or "func (r *Receiver) Foo(...)"
	line = strings.TrimPrefix(line, "func ")
	// Check for method receiver
	if strings.HasPrefix(line, "(") {
		end := strings.Index(line, ")")
		if end < 0 {
			return ""
		}
		line = strings.TrimSpace(line[end+1:])
	}
	// Extract name before '('
	if idx := strings.Index(line, "("); idx > 0 {
		return strings.TrimSpace(line[:idx])
	}
	return ""
}

func extractTypeName(line string) string {
	// "type Foo ..." or "type Foo interface{...}"
	line = strings.TrimPrefix(line, "type ")
	if idx := strings.IndexAny(line, " {[\t"); idx > 0 {
		return line[:idx]
	}
	return line
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}

func findContainingFunc(content, callee string) string {
	lines := strings.Split(content, "\n")
	var currentFunc string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "func ") {
			currentFunc = extractFuncName(line)
		}
		if currentFunc != "" && strings.Contains(line, callee+"(") {
			return currentFunc
		}
	}
	return ""
}
