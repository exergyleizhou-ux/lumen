// Package deadcode detects unused code: functions, types, interfaces, and
// imports that have no references. It provides a scanner that parses Go
// source and identifies symbols with zero callers.
package deadcode

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Finding is one piece of potentially dead code.
type Finding struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // "func", "type", "interface", "var", "import"
	File     string `json:"file"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
	Reason   string `json:"reason"` // "no references found", "always returns nil", etc.
}

// Scanner analyzes a Go codebase for dead code.
type Scanner struct {
	mu      sync.Mutex
	findings []Finding
	refs    map[string]int // symbol → reference count
	defs    map[string]Finding
}

// NewScanner creates a dead code scanner.
func NewScanner() *Scanner {
	return &Scanner{
		refs: map[string]int{},
		defs: map[string]Finding{},
	}
}

// ScanDir walks a directory and finds dead code.
func (s *Scanner) ScanDir(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		return s.ScanFile(path)
	})
}

// ScanFile analyzes a single Go file for dead code.
func (s *Scanner) ScanFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	rel, _ := filepath.Rel(".", path)
	if rel == "" {
		rel = path
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Detect function declarations
		if strings.HasPrefix(line, "func ") {
			name := extractFnName(line)
			if name != "" && name != "init" {
				fqn := rel + "." + name
				s.defs[fqn] = Finding{Name: name, Kind: "func", File: rel, Line: lineNum, Exported: isExported(name)}
			}
		}

		// Detect type declarations
		if strings.HasPrefix(line, "type ") {
			name := extractTypeName(line)
			if name != "" {
				fqn := rel + "." + name
				kind := "type"
				if strings.Contains(line, "interface") {
					kind = "interface"
				}
				s.defs[fqn] = Finding{Name: name, Kind: kind, File: rel, Line: lineNum, Exported: isExported(name)}
			}
		}
	}

	// Count references: for each defined symbol, check if it's referenced elsewhere
	for fqn := range s.defs {
		name := s.defs[fqn].Name
		if name == "" {
			continue
		}
		// Simple heuristic: count occurrences of the name in the file
		s.refs[fqn] = strings.Count(content, name) - 1 // -1 for the definition itself
		if s.refs[fqn] < 0 {
			s.refs[fqn] = 0
		}
	}

	return nil
}

// Findings returns all dead code findings, sorted by file.
func (s *Scanner) Findings() []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Finding
	for fqn, f := range s.defs {
		if s.refs[fqn] == 0 && !f.Exported {
			f.Reason = "no references found in package"
			out = append(out, f)
		}
	}

	// Also flag functions that always return nil/empty
	for fqn, f := range s.defs {
		if f.Kind == "func" && s.refs[fqn] > 0 {
			// Check for stub patterns
			if s.isStub(f.File, f.Line) {
				f.Reason = "always returns nil/empty (possible stub)"
				out = append(out, f)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

func (s *Scanner) isStub(file string, line int) bool {
	// Read the function body to check for stub patterns
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	if line >= len(lines) {
		return false
	}
	// Look at the next few lines for "return nil" or "return "" " as the only statement
	for i := line; i < line+10 && i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "return nil" || trimmed == `return ""` || trimmed == "return 0" || trimmed == "return false" || trimmed == "return nil, nil" {
			return true
		}
		if trimmed == "}" {
			return false
		}
	}
	return false
}

// FormatFindings formats findings for display.
func FormatFindings(findings []Finding) string {
	if len(findings) == 0 {
		return "No dead code found.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d potentially dead symbols:\n\n", len(findings))
	for _, f := range findings {
		icon := "🔴"
		if f.Exported {
			icon = "🟡"
		}
		fmt.Fprintf(&sb, "%s %s %s (%s:%d) — %s\n", icon, f.Kind, f.Name, f.File, f.Line, f.Reason)
	}
	return sb.String()
}

func extractFnName(line string) string {
	line = strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(line, "(") {
		if end := strings.Index(line, ")"); end > 0 {
			line = strings.TrimSpace(line[end+1:])
		}
	}
	if idx := strings.Index(line, "("); idx > 0 {
		return strings.TrimSpace(line[:idx])
	}
	return ""
}

func extractTypeName(line string) string {
	line = strings.TrimPrefix(line, "type ")
	line = strings.TrimSpace(line)
	for i, r := range line {
		if r == ' ' || r == '{' || r == '[' || r == '\t' {
			return line[:i]
		}
	}
	return line
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}
