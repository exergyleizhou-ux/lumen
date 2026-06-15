// Package audit provides an audit trail for all agent actions.
// Every tool call, file modification, and permission decision is
// recorded with timestamp, user, and result. The audit log supports
// filtering, searching, and export for compliance and debugging.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one audit record.
type Entry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`    // "tool_call", "file_write", "permission_deny", "config_change"
	Tool      string    `json:"tool,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	File      string    `json:"file,omitempty"`
	Result    string    `json:"result,omitempty"` // "success", "blocked", "error"
	User      string    `json:"user,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}

// Logger records audit entries to a JSONL file and in-memory buffer.
type Logger struct {
	mu       sync.Mutex
	file     *os.File
	buffer   []Entry
	maxBuffer int
}

// NewLogger creates an audit logger writing to the given path.
func NewLogger(path string, maxBuffer int) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if maxBuffer <= 0 {
		maxBuffer = 10000
	}
	return &Logger{file: f, maxBuffer: maxBuffer}, nil
}

// Log records an audit entry.
func (l *Logger) Log(e Entry) {
	e.Timestamp = time.Now()
	if e.ID == "" {
		e.ID = fmt.Sprintf("audit-%d", time.Now().UnixNano())
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buffer = append(l.buffer, e)
	if len(l.buffer) > l.maxBuffer {
		l.buffer = l.buffer[len(l.buffer)-l.maxBuffer:]
	}

	data, _ := json.Marshal(e)
	l.file.Write(append(data, '\n'))
}

// ToolCall is a convenience method for logging tool calls.
func (l *Logger) ToolCall(tool, detail, result string) {
	l.Log(Entry{Action: "tool_call", Tool: tool, Detail: detail, Result: result})
}

// FileWrite logs a file modification.
func (l *Logger) FileWrite(path, detail string) {
	l.Log(Entry{Action: "file_write", File: path, Detail: detail, Result: "success"})
}

// PermissionDeny logs a denied tool call.
func (l *Logger) PermissionDeny(tool, reason string) {
	l.Log(Entry{Action: "permission_deny", Tool: tool, Detail: reason, Result: "blocked"})
}

// Query returns entries matching the given filters.
func (l *Logger) Query(action, tool string, since time.Time, limit int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()

	var results []Entry
	for _, e := range l.buffer {
		if action != "" && e.Action != action {
			continue
		}
		if tool != "" && e.Tool != tool {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			continue
		}
		results = append(results, e)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})

	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}
	return results
}

// Recent returns the most recent N entries.
func (l *Logger) Recent(n int) []Entry {
	return l.Query("", "", time.Time{}, n)
}

// Close flushes and closes the log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// Size returns the number of buffered entries.
func (l *Logger) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buffer)
}

// FormatEntries formats audit entries for display.
func FormatEntries(entries []Entry) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d audit entries:\n\n", len(entries))
	for _, e := range entries {
		icon := "✓"
		switch e.Result {
		case "blocked":
			icon = "⊘"
		case "error":
			icon = "✗"
		}
		fmt.Fprintf(&sb, "%s %s %s", icon, e.Timestamp.Format("15:04:05"), e.Action)
		if e.Tool != "" {
			fmt.Fprintf(&sb, " [%s]", e.Tool)
		}
		if e.File != "" {
			fmt.Fprintf(&sb, " %s", e.File)
		}
		if e.Detail != "" {
			fmt.Fprintf(&sb, " — %s", e.Detail)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Summary returns a statistical summary of the audit log.
func (l *Logger) Summary() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	total := len(l.buffer)
	tools := map[string]int{}
	results := map[string]int{}
	files := map[string]int{}

	for _, e := range l.buffer {
		if e.Tool != "" {
			tools[e.Tool]++
		}
		results[e.Result]++
		if e.File != "" {
			files[e.File]++
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Audit Summary (%d entries)\n", total)
	fmt.Fprintf(&sb, "───────────────────────\n")
	fmt.Fprintf(&sb, "Results: success=%d blocked=%d error=%d\n\n",
		results["success"], results["blocked"], results["error"])
	sb.WriteString("Top tools:\n")
	type toolCount struct{ name string; count int }
	var tc []toolCount
	for n, c := range tools {
		tc = append(tc, toolCount{n, c})
	}
	sort.Slice(tc, func(i, j int) bool { return tc[i].count > tc[j].count })
	for i, t := range tc {
		if i >= 5 { break }
		fmt.Fprintf(&sb, "  %s: %d\n", t.name, t.count)
	}
	if len(files) > 0 {
		fmt.Fprintf(&sb, "\nFiles touched: %d\n", len(files))
	}
	return sb.String()
}
