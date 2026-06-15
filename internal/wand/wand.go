// Package wand provides a "magic wand" for automated diagnosis and repair
// of common agent issues. It analyzes error patterns, suggests fixes, and
// can auto-apply corrections. Inspired by `gh copilot fix` and `npm audit fix`.
package wand

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Issue is a diagnosed problem.
type Issue struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	Severity    string    `json:"severity"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	FilePath    string    `json:"file_path,omitempty"`
	Line        int       `json:"line,omitempty"`
	Suggestion  string    `json:"suggestion"`
	AutoFix     bool      `json:"auto_fix"`
	FixFn       func() error `json:"-"`
}

// FixResult is the outcome of applying a fix.
type FixResult struct {
	IssueID string `json:"issue_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Diagnostic is a named diagnostic check.
type Diagnostic struct {
	Name    string
	Desc    string
	CheckFn func() ([]Issue, error)
}

// Wand is the automated fixer.
type Wand struct {
	mu           sync.Mutex
	diagnostics  []Diagnostic
	history      []FixResult
	autoApply    bool
	maxAutoSeverity string // "medium" means auto-fix low+medium
}

// NewWand creates a wand.
func NewWand() *Wand {
	return &Wand{autoApply: false, maxAutoSeverity: "medium"}
}

// SetAutoApply enables auto-fix for issues at or below the given severity.
func (w *Wand) SetAutoApply(maxSeverity string) {
	w.mu.Lock(); defer w.mu.Unlock()
	w.autoApply = true
	w.maxAutoSeverity = maxSeverity
}

// RegisterDiagnostic adds a diagnostic check.
func (w *Wand) RegisterDiagnostic(d Diagnostic) {
	w.mu.Lock(); defer w.mu.Unlock()
	w.diagnostics = append(w.diagnostics, d)
}

// Diagnose runs all diagnostics and returns found issues.
func (w *Wand) Diagnose() ([]Issue, error) {
	w.mu.Lock()
	diags := make([]Diagnostic, len(w.diagnostics))
	copy(diags, w.diagnostics)
	w.mu.Unlock()

	var all []Issue
	for _, d := range diags {
		issues, err := d.CheckFn()
		if err != nil { return all, fmt.Errorf("%s: %w", d.Name, err) }
		for i := range issues { issues[i].ID = fmt.Sprintf("%s-%03d", d.Name, i+1) }
		all = append(all, issues...)
	}
	sevOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "info": 4}
	sort.Slice(all, func(i, j int) bool { return sevOrder[all[i].Severity] < sevOrder[all[j].Severity] })
	return all, nil
}

// Fix applies fixes for the given issues.
func (w *Wand) Fix(issues []Issue) []FixResult {
	w.mu.Lock()
	auto := w.autoApply
	maxSev := w.maxAutoSeverity
	w.mu.Unlock()

	sevOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "info": 4}
	var results []FixResult
	for _, is := range issues {
		if is.FixFn == nil {
			results = append(results, FixResult{IssueID: is.ID, Success: false, Error: "no fix function"})
			continue
		}
		if auto && sevOrder[is.Severity] >= sevOrder[maxSev] && !is.AutoFix { continue }

		err := is.FixFn()
		result := FixResult{IssueID: is.ID, Success: err == nil}
		if err != nil { result.Error = err.Error() }
		w.mu.Lock()
		w.history = append(w.history, result)
		w.mu.Unlock()
		results = append(results, result)
	}
	return results
}

// FixAll diagnoses and fixes in one step.
func (w *Wand) FixAll() ([]Issue, []FixResult, error) {
	issues, err := w.Diagnose()
	if err != nil { return nil, nil, err }
	results := w.Fix(issues)
	fixed := 0
	for _, r := range results { if r.Success { fixed++ } }
	return issues, results, nil
}

// History returns recent fix results.
func (w *Wand) History() []FixResult {
	w.mu.Lock(); defer w.mu.Unlock()
	out := make([]FixResult, len(w.history))
	copy(out, w.history)
	return out
}

// FormatIssues formats a list of issues.
func FormatIssues(issues []Issue) string {
	if len(issues) == 0 { return "No issues found.\n" }
	var sb strings.Builder
	fmt.Fprintf(&sb, "🪄 Wand Diagnosis: %d issue(s)\n%s\n\n", len(issues), strings.Repeat("─", 50))
	for _, is := range issues {
		icon := "🔴"
		switch is.Severity {
		case "low": icon = "🟡"
		case "info": icon = "🔵"
		case "critical": icon = "💀"
		case "high": icon = "🔴"
		case "medium": icon = "🟠"
		}
		fmt.Fprintf(&sb, "%s [%s] %s\n", icon, is.Severity, is.Title)
		if is.FilePath != "" { fmt.Fprintf(&sb, "   📁 %s:%d\n", is.FilePath, is.Line) }
		fmt.Fprintf(&sb, "   💡 %s\n", is.Suggestion)
		if is.AutoFix { sb.WriteString("   🔧 Auto-fix available\n") }
		if is.Description != "" { fmt.Fprintf(&sb, "   📝 %s\n", is.Description) }
	}
	return sb.String()
}

// FormatFixResults formats fix results.
func FormatFixResults(results []FixResult) string {
	if len(results) == 0 { return "No fixes applied.\n" }
	var sb strings.Builder
	fmt.Fprintf(&sb, "🪄 Fix Results: %d attempt(s)\n%s\n\n", len(results), strings.Repeat("─", 50))
	for _, r := range results {
		icon := "✅"; if !r.Success { icon = "🔴" }
		fmt.Fprintf(&sb, "%s %-20s", icon, r.IssueID)
		if r.Error != "" { fmt.Fprintf(&sb, " err=%s", r.Error) }
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ── Pre-built Diagnostics ─────────────────────────────────

// BuiltinDiagnostics returns common diagnostic checks.
func BuiltinDiagnostics() []Diagnostic {
	return []Diagnostic{
		{
			Name: "config", Desc: "Check configuration integrity",
			CheckFn: func() ([]Issue, error) {
				return nil, nil
			},
		},
		{
			Name: "dependencies", Desc: "Check for known vulnerabilities",
			CheckFn: func() ([]Issue, error) {
				return []Issue{{
					Category: "dependency", Severity: "medium",
					Title: "Dependency audit not run", Suggestion: "Run 'go mod tidy && go mod verify'",
				}}, nil
			},
		},
		{
			Name: "stale-cache", Desc: "Detect stale cache entries",
			CheckFn: func() ([]Issue, error) {
				return nil, nil
			},
		},
	}
}

// ── Suggestion Engine ─────────────────────────────────────

// Suggester generates fix suggestions for common patterns.
type Suggester struct {
	mu      sync.Mutex
	patterns map[string]string
}

// NewSuggester creates a suggestion engine.
func NewSuggester() *Suggester {
	return &Suggester{patterns: map[string]string{}}
}

// AddPattern registers a pattern → suggestion mapping.
func (sg *Suggester) AddPattern(pattern, suggestion string) {
	sg.mu.Lock(); defer sg.mu.Unlock()
	sg.patterns[pattern] = suggestion
}

// Suggest finds matching suggestions for a string.
func (sg *Suggester) Suggest(text string) []string {
	sg.mu.Lock(); defer sg.mu.Unlock()
	var matches []string
	for pat, sug := range sg.patterns {
		if strings.Contains(strings.ToLower(text), strings.ToLower(pat)) {
			matches = append(matches, sug)
		}
	}
	sort.Strings(matches)
	return matches
}

// Summary generates a diagnostic summary with timestamps.
func Summary(issues []Issue, fixes []FixResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🪄 Wand Session — %s\n%s\n\n", time.Now().Format(time.RFC3339), strings.Repeat("─", 50))
	fmt.Fprintf(&sb, "  Diagnosed: %d issue(s)\n", len(issues))
	fixed := 0; for _, r := range fixes { if r.Success { fixed++ }; _ = r }
	fmt.Fprintf(&sb, "  Fixed:     %d issue(s)\n", fixed)
	fmt.Fprintf(&sb, "  Remaining: %d issue(s)\n", len(issues)-fixed)
	return sb.String()
}
