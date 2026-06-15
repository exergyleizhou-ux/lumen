// Package template provides Go template rendering with common functions
// for shell scripts, prompts, and configuration files. Supports markdown
// rendering, shell escaping, JSON/YAML helpers, and sprig-like utilities.
package template

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"
)

// Engine renders templates with a shared function library.
type Engine struct {
	mu       sync.Mutex
	funcs    template.FuncMap
	cache    map[string]*template.Template
	cacheOn  bool
}

// NewEngine creates a template engine with default functions.
func NewEngine() *Engine {
	e := &Engine{cache: map[string]*template.Template{}, cacheOn: true}
	e.funcs = template.FuncMap{
		"upper":    strings.ToUpper,
		"lower":    strings.ToLower,
		"title":    strings.Title,
		"trim":     strings.TrimSpace,
		"contains": strings.Contains,
		"replace":  strings.ReplaceAll,
		"join":     strings.Join,
		"split":    strings.Split,
		"toJSON":   toJSON,
		"fromJSON": fromJSON,
		"default":  defaultValue,
		"indent":   indent,
		"quote":    quote,
		"shellEscape": shellEscape,
		"list":     makeList,
	}
	return e
}

// AddFunc registers a custom template function.
func (e *Engine) AddFunc(name string, fn any) {
	e.mu.Lock(); defer e.mu.Unlock()
	e.funcs[name] = fn
}

// Render evaluates a template string with the given data.
func (e *Engine) Render(tmpl string, data any) (string, error) {
	e.mu.Lock(); defer e.mu.Unlock()

	var t *template.Template
	var err error

	if e.cacheOn {
		if cached, ok := e.cache[tmpl]; ok {
			t = cached
		}
	}

	if t == nil {
		t, err = template.New("inline").Funcs(e.funcs).Parse(tmpl)
		if err != nil { return "", fmt.Errorf("parse: %w", err) }
		if e.cacheOn { e.cache[tmpl] = t }
	}

	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("execute: %w", err)
	}
	return sb.String(), nil
}

// RenderFile renders a template file with data.
func (e *Engine) RenderFile(path string, data any) (string, error) {
	t, err := template.New(path).Funcs(e.funcs).ParseFiles(path)
	if err != nil { return "", fmt.Errorf("parse %s: %w", path, err) }
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil { return "", fmt.Errorf("execute: %w", err) }
	return sb.String(), nil
}

// ── Prompt template helpers ──────────────────────────────

// PromptContext holds common prompt template data.
type PromptContext struct {
	Task        string            `json:"task"`
	Files       []string          `json:"files,omitempty"`
	Context     string            `json:"context,omitempty"`
	Language    string            `json:"language,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
	Examples    []PromptExample   `json:"examples,omitempty"`
	Extra       map[string]any    `json:"extra,omitempty"`
}

// PromptExample is a few-shot example for prompt templates.
type PromptExample struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// RenderPrompt renders a prompt template with the given context.
func (e *Engine) RenderPrompt(tmpl string, ctx *PromptContext) (string, error) {
	return e.Render(tmpl, ctx)
}

// ── Template functions ────────────────────────────────────

func toJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil { return fmt.Sprintf("%v", v) }
	return string(b)
}

func fromJSON(s string) any {
	var v any; json.Unmarshal([]byte(s), &v); return v
}

func defaultValue(def, val any) any {
	if val == nil || val == "" { return def }
	return val
}

func indent(spaces int, s string) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, l := range lines { lines[i] = prefix + l }
	return strings.Join(lines, "\n")
}

func quote(s string) string { return fmt.Sprintf("%q", s) }

func shellEscape(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func makeList(items ...any) []any { return items }

// ── Built-in prompt templates ──────────────────────────────

// SystemPromptTemplate is the default system prompt template.
const SystemPromptTemplate = `You are a coding agent. {{ .Task }}

Rules:
{{ range .Constraints }}
- {{ . }}
{{ end }}

{{ if .Files }}
Working with files:
{{ range .Files }}
- {{ . }}
{{ end }}
{{ end }}

{{ if .Context }}
Context:
{{ .Context }}
{{ end }}

{{ if .Examples }}
Examples:
{{ range .Examples }}
Input: {{ .Input }}
Output: {{ .Output }}
{{ end }}
{{ end }}
`

// DiffTemplate renders a diff for display.
const DiffTemplate = `## Changes

{{ range .Files }}
### {{ .Path }} ({{ .Changes }} change(s))

` + "```" + `diff
{{ .Diff }}
` + "```" + `

{{ end }}
`

// ChangelogTemplate renders a changelog entry.
const ChangelogTemplate = `## {{ .Version }} ({{ .Date }})

{{ range .Sections }}
### {{ .Title }}
{{ range .Items }}
- {{ . }}
{{ end }}

{{ end }}
`
