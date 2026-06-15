// Package exporter converts agent sessions and data to various output
// formats: JSON, HTML, Markdown, CSV. Used for sharing results, creating
// reports, or archiving sessions.
package exporter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Format is an output format.
type Format string

const (
	FormatJSON     Format = "json"
	FormatHTML     Format = "html"
	FormatMarkdown Format = "md"
	FormatCSV      Format = "csv"
)

// SessionData holds session data for export.
type SessionData struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Date      time.Time        `json:"date"`
	Messages  []MessageData    `json:"messages"`
	Usage     UsageData        `json:"usage"`
	Changes   []ChangeData     `json:"changes"`
}

// MessageData holds one message for export.
type MessageData struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	ToolCalls []ToolCallData `json:"tool_calls,omitempty"`
}

// ToolCallData holds one tool call for export.
type ToolCallData struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

// UsageData holds token usage for export.
type UsageData struct {
	PromptTokens     int `json:"prompt"`
	CompletionTokens int `json:"completion"`
	TotalTokens      int `json:"total"`
	CacheHitTokens   int `json:"cache_hit"`
	CacheMissTokens  int `json:"cache_miss"`
}

// ChangeData holds file change data for export.
type ChangeData struct {
	Path       string   `json:"path"`
	Operations []string `json:"operations"`
}

// Export writes session data to a file in the given format.
func Export(data *SessionData, format Format, outputPath string) error {
	os.MkdirAll(filepath.Dir(outputPath), 0o755)

	switch format {
	case FormatJSON:
		return exportJSON(data, outputPath)
	case FormatHTML:
		return exportHTML(data, outputPath)
	case FormatMarkdown:
		return exportMarkdown(data, outputPath)
	case FormatCSV:
		return exportCSV(data, outputPath)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func exportJSON(data *SessionData, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func exportHTML(data *SessionData, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">
<style>body{font-family:system-ui;max-width:900px;margin:0 auto;padding:20px}
.user{background:#e3f2fd;padding:8px 12px;border-radius:8px;margin:8px 0}
.assistant{background:#f5f5f5;padding:8px 12px;border-radius:8px;margin:8px 0}
.tool{color:#666;font-size:0.9em;padding:4px 8px}
.header{color:#333;border-bottom:2px solid #1976d2;padding-bottom:8px}
</style></head><body>`)

	fmt.Fprintf(f, "<h1 class='header'>%s</h1>\n", html.EscapeString(data.Title))
	fmt.Fprintf(f, "<p>%s · %d tokens</p>\n", data.Date.Format("2006-01-02 15:04"), data.Usage.TotalTokens)

	for _, m := range data.Messages {
		switch m.Role {
		case "user":
			fmt.Fprintf(f, "<div class='user'><strong>You</strong><br>%s</div>\n", html.EscapeString(m.Content))
		case "assistant":
			fmt.Fprintf(f, "<div class='assistant'><strong>Lumen</strong><br>%s</div>\n", html.EscapeString(m.Content))
		case "tool":
			fmt.Fprintf(f, "<div class='tool'>🔧 %s</div>\n", html.EscapeString(m.Content))
		}
	}
	f.WriteString("</body></html>")
	return nil
}

func exportMarkdown(data *SessionData, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# %s\n\n", data.Title)
	fmt.Fprintf(f, "**%s** · %d tokens\n\n", data.Date.Format("2006-01-02 15:04"), data.Usage.TotalTokens)

	for _, m := range data.Messages {
		switch m.Role {
		case "user":
			fmt.Fprintf(f, "### You\n\n%s\n\n", m.Content)
		case "assistant":
			fmt.Fprintf(f, "### Lumen\n\n%s\n\n", m.Content)
		case "tool":
			fmt.Fprintf(f, "> 🔧 %s\n\n", m.Content)
		}
	}

	if len(data.Changes) > 0 {
		fmt.Fprintf(f, "## Changed Files\n\n")
		for _, c := range data.Changes {
			fmt.Fprintf(f, "- %s\n", c.Path)
		}
	}
	return nil
}

func exportCSV(data *SessionData, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"Role", "Content"})
	for _, m := range data.Messages {
		w.Write([]string{m.Role, m.Content})
	}
	w.Flush()
	return w.Error()
}

// ── Convenience exporters ──────────────────────────────────

// ToJSON returns the session as an indented JSON string.
func ToJSON(data *SessionData) (string, error) {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ToMarkdown returns the session as a Markdown string.
func ToMarkdown(data *SessionData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", data.Title)
	fmt.Fprintf(&sb, "**%s** · %d tokens\n\n", data.Date.Format("2006-01-02 15:04"), data.Usage.TotalTokens)
	for _, m := range data.Messages {
		switch m.Role {
		case "user":
			fmt.Fprintf(&sb, "### You\n\n%s\n\n", m.Content)
		case "assistant":
			fmt.Fprintf(&sb, "### Lumen\n\n%s\n\n", m.Content)
		}
	}
	return sb.String()
}

// ── Diff export ────────────────────────────────────────────

// DiffData holds a unified diff for export.
type DiffData struct {
	FilePath string `json:"file_path"`
	Before   string `json:"before"`
	After    string `json:"after"`
}

// ExportDiff writes a diff report to a file.
func ExportDiff(diffs []DiffData, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# Diff Report\n\n")
	fmt.Fprintf(f, "%d file(s) changed\n\n", len(diffs))
	for _, d := range diffs {
		fmt.Fprintf(f, "## %s\n\n", d.FilePath)
		beforeLines := strings.Split(d.Before, "\n")
		afterLines := strings.Split(d.After, "\n")
		for _, l := range beforeLines {
			fmt.Fprintf(f, "- %s\n", l)
		}
		for _, l := range afterLines {
			fmt.Fprintf(f, "+ %s\n", l)
		}
		fmt.Fprintln(f)
	}
	return nil
}
