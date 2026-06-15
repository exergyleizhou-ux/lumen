// Package complexity computes code complexity metrics: cyclomatic
// complexity, cognitive complexity, and maintainability index for Go
// source files. Used to identify functions that may need refactoring.
package complexity

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Metric holds complexity scores for one function.
type Metric struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Cyclomatic int    `json:"cyclomatic"`
	Cognitive  int    `json:"cognitive"`
	Lines      int    `json:"lines"`
	Nesting    int    `json:"nesting_depth"`
	Risk       string `json:"risk"` // "low", "medium", "high", "critical"
}

// Analyzer computes complexity metrics for a Go codebase.
type Analyzer struct {
	mu      sync.Mutex
	metrics []Metric
}

// NewAnalyzer creates a complexity analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// AnalyzeDir walks a directory and computes metrics for all Go files.
func (a *Analyzer) AnalyzeDir(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		return a.AnalyzeFile(path)
	})
}

// AnalyzeFile computes metrics for a single Go file.
func (a *Analyzer) AnalyzeFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	rel, _ := filepath.Rel(".", path)
	if rel == "" {
		rel = path
	}

	lines := strings.Split(string(data), "\n")

	a.mu.Lock()
	defer a.mu.Unlock()

	for lineNum := 0; lineNum < len(lines); lineNum++ {
		line := strings.TrimSpace(lines[lineNum])
		if !strings.HasPrefix(line, "func ") {
			continue
		}

		name := extractFnName(line)
		if name == "" || name == "init" {
			continue
		}

		// Find function body
		bodyStart := lineNum
		for bodyStart < len(lines) && !strings.Contains(lines[bodyStart], "{") {
			bodyStart++
		}
		if bodyStart >= len(lines) {
			continue
		}

		bodyEnd := findMatchingBrace(lines, bodyStart)
		if bodyEnd < 0 {
			continue
		}

		cyclo := cyclomaticComplexity(lines, bodyStart, bodyEnd)
		cog := cognitiveComplexity(lines, bodyStart, bodyEnd)
		nesting := maxNestingDepth(lines, bodyStart, bodyEnd)
		lineCount := bodyEnd - bodyStart

		risk := "low"
		switch {
		case cyclo > 30 || cog > 25:
			risk = "critical"
		case cyclo > 20 || cog > 15:
			risk = "high"
		case cyclo > 10 || cog > 10:
			risk = "medium"
		}

		a.metrics = append(a.metrics, Metric{
			Name:       name,
			File:       rel,
			Line:       lineNum + 1,
			Cyclomatic: cyclo,
			Cognitive:  cog,
			Lines:      lineCount,
			Nesting:    nesting,
			Risk:       risk,
		})
	}
	return nil
}

func cyclomaticComplexity(lines []string, start, end int) int {
	count := 1 // base complexity
	for i := start; i <= end && i < len(lines); i++ {
		line := lines[i]
		for _, kw := range []string{"if ", "for ", "case ", "||", "&&", "default:"} {
			if strings.Contains(line, kw) {
				count++
			}
		}
	}
	return count
}

func cognitiveComplexity(lines []string, start, end int) int {
	count := 0
	nesting := 0
	for i := start; i <= end && i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		// Track nesting
		if strings.HasSuffix(line, "{") {
			nesting++
		}
		if line == "}" {
			nesting--
			if nesting < 0 {
				nesting = 0
			}
		}
		// Structural breaks add nesting×1
		if strings.Contains(line, "if ") || strings.Contains(line, "for ") || strings.Contains(line, "switch ") {
			count += 1 + nesting
		}
		// Break/continue adds 1
		if line == "break" || line == "continue" {
			count += 1
		}
	}
	return count
}

func maxNestingDepth(lines []string, start, end int) int {
	maxDepth := 0
	current := 0
	for i := start; i <= end && i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasSuffix(line, "{") {
			current++
			if current > maxDepth {
				maxDepth = current
			}
		}
		if line == "}" {
			current--
		}
	}
	return maxDepth
}

func findMatchingBrace(lines []string, start int) int {
	depth := 0
	for i := start; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{")
		depth -= strings.Count(lines[i], "}")
		if depth == 0 {
			return i
		}
	}
	return -1
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

// Metrics returns all computed metrics, sorted by risk.
func (a *Analyzer) Metrics() []Metric {
	a.mu.Lock()
	defer a.mu.Unlock()

	sort.Slice(a.metrics, func(i, j int) bool {
		order := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
		return order[a.metrics[i].Risk] < order[a.metrics[j].Risk]
	})
	out := make([]Metric, len(a.metrics))
	copy(out, a.metrics)
	return out
}

// FormatMetrics formats metrics for display.
func FormatMetrics(metrics []Metric) string {
	if len(metrics) == 0 {
		return "No functions found to analyze.\n"
	}
	var sb strings.Builder
	icons := map[string]string{"critical": "🔴", "high": "🟠", "medium": "🟡", "low": "🟢"}
	fmt.Fprintf(&sb, "%d function(s) analyzed:\n\n", len(metrics))

	fmt.Fprintf(&sb, "%-5s %-8s %-6s %-6s %-25s %s\n", "Risk", "Cyclo", "Cog", "Lines", "Name", "File:Line")
	fmt.Fprintf(&sb, "%s\n", strings.Repeat("─", 80))
	for _, m := range metrics {
		fmt.Fprintf(&sb, "%-5s %-8d %-6d %-6d %-25s %s:%d\n",
			icons[m.Risk], m.Cyclomatic, m.Cognitive, m.Lines, m.Name, m.File, m.Line)
	}
	return sb.String()
}

// TopN returns the N most complex functions.
func TopN(metrics []Metric, n int) []Metric {
	if n > len(metrics) {
		n = len(metrics)
	}
	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].Cyclomatic > metrics[j].Cyclomatic
	})
	return metrics[:n]
}

// MaintainabilityIndex computes a simple maintainability score (0-100).
func MaintainabilityIndex(cyclomatic, lines, comments int) float64 {
	if lines == 0 {
		return 100
	}
	vol := float64(lines) / 1000.0
	score := 171.0 - 5.2*float64(cyclomatic) - 0.23*vol
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}
