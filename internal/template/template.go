// Package template provides a template rendering engine supporting Go
// templates, Markdown with frontmatter, and prompt composition. Used for
// generating agent prompts, configuration files, and reports.
package template

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"
)

// Engine renders templates with context data.
type Engine struct {
	mu      sync.RWMutex
	funcs   template.FuncMap
	cache   map[string]*template.Template
	files   map[string]string
}

// NewEngine creates a template engine.
func NewEngine() *Engine {
	e := &Engine{
		funcs: template.FuncMap{},
		cache: map[string]*template.Template{},
		files: map[string]string{},
	}
	e.funcs["upper"] = strings.ToUpper
	e.funcs["lower"] = strings.ToLower
	e.funcs["now"] = func() string { return time.Now().Format(time.RFC3339) }
	e.funcs["join"] = func(sep string, items []string) string { return strings.Join(items, sep) }
	e.funcs["contains"] = strings.Contains
	e.funcs["trim"] = strings.TrimSpace
	return e
}

// AddFunc registers a custom template function.
func (e *Engine) AddFunc(name string, fn any) {
	e.mu.Lock(); defer e.mu.Unlock()
	e.funcs[name] = fn
}

// LoadFile loads a template file by name.
func (e *Engine) LoadFile(name, content string) {
	e.mu.Lock(); defer e.mu.Unlock()
	e.files[name] = content
	delete(e.cache, name) // Invalidate cache
}

// Render renders a named template with data.
func (e *Engine) Render(name string, data any) (string, error) {
	e.mu.Lock()
	tmpl, ok := e.cache[name]
	if !ok {
		content, hasFile := e.files[name]
		if !hasFile {
			e.mu.Unlock()
			return "", fmt.Errorf("template %q not found", name)
		}
		var err error
		tmpl, err = template.New(name).Funcs(e.funcs).Parse(content)
		if err != nil { e.mu.Unlock(); return "", fmt.Errorf("parse %q: %w", name, err) }
		e.cache[name] = tmpl
	}
	e.mu.Unlock()

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render %q: %w", name, err)
	}
	return buf.String(), nil
}

// RenderString renders an inline template string.
func (e *Engine) RenderString(tmplStr string, data any) (string, error) {
	e.mu.Lock()
	tmpl, err := template.New("inline").Funcs(e.funcs).Parse(tmplStr)
	e.mu.Unlock()
	if err != nil { return "", fmt.Errorf("parse: %w", err) }

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	return buf.String(), nil
}

// Names returns loaded template names.
func (e *Engine) Names() []string {
	e.mu.RLock(); defer e.mu.RUnlock()
	var out []string
	for n := range e.files { out = append(out, n) }
	sort.Strings(out)
	return out
}

// ── Prompt Builder ───────────────────────────────────────

// PromptBuilder composes structured prompts for LLM agents.
type PromptBuilder struct {
	mu       sync.Mutex
	sections []promptSection
}

type promptSection struct {
	Title   string
	Content string
	Order   int
}

// NewPromptBuilder creates a prompt builder.
func NewPromptBuilder() *PromptBuilder { return &PromptBuilder{} }

// AddSection adds a section to the prompt.
func (pb *PromptBuilder) AddSection(title, content string, order int) {
	pb.mu.Lock(); defer pb.mu.Unlock()
	pb.sections = append(pb.sections, promptSection{Title: title, Content: content, Order: order})
	sort.Slice(pb.sections, func(i, j int) bool { return pb.sections[i].Order < pb.sections[j].Order })
}

// Build assembles the final prompt string.
func (pb *PromptBuilder) Build() string {
	pb.mu.Lock(); defer pb.mu.Unlock()
	var sb strings.Builder
	for _, s := range pb.sections {
		sb.WriteString(fmt.Sprintf("## %s\n\n", s.Title))
		sb.WriteString(s.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// Clear removes all sections.
func (pb *PromptBuilder) Clear() {
	pb.mu.Lock(); defer pb.mu.Unlock()
	pb.sections = nil
}

// ── Report Generator ─────────────────────────────────────

// ReportData is the input for report generation.
type ReportData struct {
	Title    string            `json:"title"`
	Date     time.Time         `json:"date"`
	Sections []ReportSection   `json:"sections"`
	Summary  string            `json:"summary"`
}

// ReportSection is one section of a report.
type ReportSection struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	Metrics map[string]float64 `json:"metrics,omitempty"`
}

// GenerateReport produces a Markdown report.
func GenerateReport(data ReportData) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", data.Title))
	sb.WriteString(fmt.Sprintf("**Date:** %s\n\n", data.Date.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("> %s\n\n---\n\n", data.Summary))

	for _, s := range data.Sections {
		sb.WriteString(fmt.Sprintf("## %s\n\n", s.Title))
		sb.WriteString(s.Body)
		sb.WriteString("\n\n")
		if len(s.Metrics) > 0 {
			sb.WriteString("| Metric | Value |\n|--------|-------|\n")
			for k, v := range s.Metrics {
				sb.WriteString(fmt.Sprintf("| %s | %.2f |\n", k, v))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
