// Package inline provides code inlining analysis: identifies functions
// that should be inlined based on size and call frequency, computes
// inline cost models, and generates inline recommendations for the
// agent to apply during refactoring sessions.
package inline

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Candidate is a function that may benefit from inlining.
type Candidate struct {
	Name      string  `json:"name"`
	File      string  `json:"file"`
	Line      int     `json:"line"`
	BodyLines int     `json:"body_lines"`
	CallCount int     `json:"call_count"`
	Cost      float64 `json:"cost"`
	Inline    bool    `json:"inline"`
	Reason    string  `json:"reason"`
}

// Analyzer finds inline candidates in Go source code.
type Analyzer struct {
	mu     sync.Mutex
	dir    string
	byName map[string]*Candidate
	byFile map[string][]*Candidate
}

// NewAnalyzer creates an inline analyzer for a directory.
func NewAnalyzer(dir string) *Analyzer {
	return &Analyzer{dir: dir, byName: map[string]*Candidate{}, byFile: map[string][]*Candidate{}}
}

// Scan walks the directory and finds inline candidates.
func (a *Analyzer) Scan() error {
	return filepath.Walk(a.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			if info != nil && info.IsDir() && (strings.HasPrefix(info.Name(), ".") || info.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		return a.scanFile(path)
	})
}

func (a *Analyzer) scanFile(path string) error {
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()

	rel, _ := filepath.Rel(a.dir, path)
	if rel == "" { rel = path }

	lines := []string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() { lines = append(lines, scanner.Text()) }

	a.mu.Lock()
	defer a.mu.Unlock()

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "func ") { continue }
		name := extractFuncName(line)
		if name == "" || name == "init" { continue }
		bodyEnd := findFuncEnd(lines, i)
		if bodyEnd < 0 { continue }
		bodyLines := bodyEnd - i
		c := &Candidate{Name: name, File: rel, Line: i + 1, BodyLines: bodyLines}
		a.byName[name] = c
		a.byFile[rel] = append(a.byFile[rel], c)
	}

	// Count calls
	content := strings.Join(lines, "\n")
	for name := range a.byName {
		a.byName[name].CallCount = strings.Count(content, name+"(")
	}
	return nil
}

// Analyze computes inline recommendations.
func (a *Analyzer) Analyze() []Candidate {
	a.mu.Lock()
	defer a.mu.Unlock()

	const (
		maxBodyLines = 20
		minCallCount = 3
		maxCost = 5.0
	)

	var results []Candidate
	for _, c := range a.byName {
		c.Cost = float64(c.BodyLines) / float64(max(1, c.CallCount))
		switch {
		case c.BodyLines <= 5 && c.CallCount >= minCallCount:
			c.Inline = true
			c.Reason = "small function called frequently — low inlining cost"
		case c.BodyLines <= maxBodyLines && c.CallCount >= 10:
			c.Inline = true
			c.Reason = "moderate function called very frequently — net benefit"
		case c.BodyLines == 1:
			c.Inline = true
			c.Reason = "single-line wrapper — always inline"
		case c.Cost > maxCost:
			c.Reason = fmt.Sprintf("high inline cost (%.1f) — not recommended", c.Cost)
		default:
			c.Reason = "inline benefit unclear — manual review recommended"
		}
		results = append(results, *c)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Cost < results[j].Cost })
	return results
}

// TopN returns the N best inline candidates.
func (a *Analyzer) TopN(n int) []Candidate {
	all := a.Analyze()
	if n > len(all) { n = len(all) }
	return all[:n]
}

// FormatResults formats inline analysis for display.
func FormatResults(candidates []Candidate) string {
	if len(candidates) == 0 { return "No inline candidates found.\n" }
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d inline candidate(s):\n\n", len(candidates))
	for _, c := range candidates {
		icon := "○"
		if c.Inline { icon = "●" }
		fmt.Fprintf(&sb, "%s %s (%s:%d) — %d lines, %d calls, cost=%.1f\n",
			icon, c.Name, c.File, c.Line, c.BodyLines, c.CallCount, c.Cost)
		fmt.Fprintf(&sb, "   %s\n", c.Reason)
	}
	return sb.String()
}

func extractFuncName(line string) string {
	line = strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(line, "(") {
		if end := strings.Index(line, ")"); end > 0 { line = strings.TrimSpace(line[end+1:]) }
	}
	if idx := strings.Index(line, "("); idx > 0 { return strings.TrimSpace(line[:idx]) }
	return ""
}

func findFuncEnd(lines []string, start int) int {
	depth := 0
	for i := start; i < len(lines); i++ {
		for _, r := range lines[i] {
			if r == '{' { depth++ }
			if r == '}' { depth-- }
		}
		if depth == 0 && i > start { return i }
	}
	return -1
}
