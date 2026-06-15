// Package codemap analyzes and visualizes code structure: symbol
// extraction, call graphs, import trees, dependency matrices, and
// complexity scoring for Go and TypeScript codebases.
package codemap

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Symbol is a named code element.
type Symbol struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"` // func, type, method, interface, var, const
	Package  string    `json:"package"`
	File     string    `json:"file"`
	Line     int       `json:"line"`
	Exported bool      `json:"exported"`
	Doc      string    `json:"doc,omitempty"`
}

// CallEdge is a calls relationship between symbols.
type CallEdge struct {
	Caller   string `json:"caller"`
	Callee   string `json:"callee"`
	Count    int    `json:"count"`
	File     string `json:"file"`
}

// ImportEdge is an import relationship between packages.
type ImportEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Alias    string `json:"alias,omitempty"`
}

// Map is a code structure map.
type Map struct {
	mu       sync.RWMutex
	symbols  map[string]*Symbol
	calls    []CallEdge
	imports  []ImportEdge
	files    []string
	packages map[string][]string // package -> file list
}

// NewMap creates a code map.
func NewMap() *Map {
	return &Map{symbols: map[string]*Symbol{}, packages: map[string][]string{}}
}

// AddSymbol registers a symbol.
func (m *Map) AddSymbol(sym *Symbol) {
	m.mu.Lock(); defer m.mu.Unlock()
	id := fmt.Sprintf("%s.%s", sym.Package, sym.Name)
	m.symbols[id] = sym
	m.packages[sym.Package] = append(m.packages[sym.Package], sym.File)
}

// AddCall registers a call edge.
func (m *Map) AddCall(caller, callee, file string) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.calls = append(m.calls, CallEdge{Caller: caller, Callee: callee, Count: 1, File: file})
}

// AddImport registers an import edge.
func (m *Map) AddImport(from, to string) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.imports = append(m.imports, ImportEdge{From: from, To: to})
}

// Symbols returns all symbols.
func (m *Map) Symbols() []*Symbol {
	m.mu.RLock(); defer m.mu.RUnlock()
	var out []*Symbol
	for _, s := range m.symbols { out = append(out, s) }
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Calls returns all call edges.
func (m *Map) Calls() []CallEdge {
	m.mu.RLock(); defer m.mu.RUnlock()
	out := make([]CallEdge, len(m.calls))
	copy(out, m.calls)
	return out
}

// Callees returns everything called by a symbol.
func (m *Map) Callees(caller string) []string {
	m.mu.RLock(); defer m.mu.RUnlock()
	seen := map[string]bool{}
	for _, c := range m.calls {
		if c.Caller == caller { seen[c.Callee] = true }
	}
	var out []string
	for s := range seen { out = append(out, s) }
	sort.Strings(out)
	return out
}

// Callers returns everything that calls a symbol.
func (m *Map) Callers(callee string) []string {
	m.mu.RLock(); defer m.mu.RUnlock()
	seen := map[string]bool{}
	for _, c := range m.calls {
		if c.Callee == callee { seen[c.Caller] = true }
	}
	var out []string
	for s := range seen { out = append(out, s) }
	sort.Strings(out)
	return out
}

// PackageSymbols returns symbols in a package.
func (m *Map) PackageSymbols(pkg string) []*Symbol {
	m.mu.RLock(); defer m.mu.RUnlock()
	var out []*Symbol
	for _, s := range m.symbols {
		if s.Package == pkg { out = append(out, s) }
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ── Analysis ──────────────────────────────────────────────

// ComplexityScore holds code complexity metrics.
type ComplexityScore struct {
	Cyclomatic     int `json:"cyclomatic"`
	Cognitive      int `json:"cognitive"`
	LinesOfCode    int `json:"loc"`
	CommentDensity float64 `json:"comment_density"`
}

// EstimateComplexity provides a rough complexity estimate.
func EstimateComplexity(symbols []*Symbol) map[string]*ComplexityScore {
	scores := map[string]*ComplexityScore{}
	for _, s := range symbols {
		// Approximate: exported + kind determines rough complexity
		base := 1
		switch s.Kind {
		case "func", "method": base = 2
		case "type": base = 3
		case "interface": base = 5
		}
		if s.Exported { base++ }
		scores[s.Name] = &ComplexityScore{Cyclomatic: base, Cognitive: base * 2}
	}
	return scores
}

// DependencyMatrix builds a package dependency matrix.
func (m *Map) DependencyMatrix() map[string]map[string]bool {
	m.mu.RLock(); defer m.mu.RUnlock()
	matrix := map[string]map[string]bool{}
	for _, imp := range m.imports {
		if _, ok := matrix[imp.From]; !ok { matrix[imp.From] = map[string]bool{} }
		matrix[imp.From][imp.To] = true
	}
	return matrix
}

// CircularDeps detects circular package dependencies.
func (m *Map) CircularDeps() [][]string {
	matrix := m.DependencyMatrix()
	var cycles [][]string

	// DFS-based cycle detection
	white, gray, black := 0, 1, 2
	color := map[string]int{}
	for pkg := range matrix {
		if color[pkg] == 0 {
			var path []string
			if detectCycle(pkg, matrix, color, &path, white, gray, black) {
				cycles = append(cycles, append([]string{}, path...))
			}
		}
	}
	return cycles
}

func detectCycle(v string, matrix map[string]map[string]bool, color map[string]int, path *[]string, white, gray, black int) bool {
	color[v] = gray
	*path = append(*path, v)
	for dep := range matrix[v] {
		if color[dep] == gray { return true }
		if color[dep] == white && detectCycle(dep, matrix, color, path, white, gray, black) { return true }
	}
	color[v] = black
	*path = (*path)[:len(*path)-1]
	return false
}

// ── Formatters ────────────────────────────────────────────

// FormatSymbol formats a symbol.
func FormatSymbol(s *Symbol) string {
	export := ""; if s.Exported { export = " [exported]" }
	return fmt.Sprintf("%s.%s (%s) %s:%d%s", s.Package, s.Name, s.Kind, s.File, s.Line, export)
}

// FormatCallGraph formats calls in DOT graph format.
func (m *Map) FormatCallGraph() string {
	var sb strings.Builder
	sb.WriteString("digraph calls {\n")
	sb.WriteString("  rankdir=LR;\n")
	for _, c := range m.Calls() {
		fmt.Fprintf(&sb, "  \"%s\" -> \"%s\";\n", c.Caller, c.Callee)
	}
	sb.WriteString("}\n")
	return sb.String()
}

// FormatSummary prints a code map summary.
func (m *Map) FormatSummary() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Code Map Summary:\n%s\n\n", strings.Repeat("─", 50))
	fmt.Fprintf(&sb, "  Symbols:  %d\n", len(m.symbols))
	fmt.Fprintf(&sb, "  Calls:    %d\n", len(m.calls))
	fmt.Fprintf(&sb, "  Imports:  %d\n", len(m.imports))
	fmt.Fprintf(&sb, "  Packages: %d\n", len(m.packages))

	cycles := m.CircularDeps()
	if len(cycles) > 0 {
		fmt.Fprintf(&sb, "\n  ⚠️  Circular Dependencies:\n")
		for _, c := range cycles { fmt.Fprintf(&sb, "     %s\n", strings.Join(c, " → ")) }
	}
	return sb.String()
}
