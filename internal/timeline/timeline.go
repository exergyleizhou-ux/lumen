// Package timeline records a structured, per-turn event log for session
// replay and change tracking. Every event the agent emits (tool calls, file
// writes, edits, bash commands) is appended to a JSONL timeline file.
// FanBox 的「会话回放」+「变更收件箱」概念翻译为纯 Go 实现。
package timeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"lumen/internal/event"
)

// Entry is one recorded event in the session timeline.
type Entry struct {
	Turn      int       `json:"turn"`
	Timestamp time.Time `json:"ts"`
	Kind      string    `json:"kind"`            // "tool_dispatch", "tool_result", "text", "notice", "phase"
	ToolName  string    `json:"tool,omitempty"`  // bash / write_file / edit_file / ...
	Detail    string    `json:"detail,omitempty"` // first line of output, or command summary
	Path      string    `json:"path,omitempty"`   // file path for write_file / edit_file
	Success   bool      `json:"success,omitempty"`
}

// Recorder appends entries to a JSONL file in real time as the agent runs.
// It is safe for concurrent use (the agent may dispatch tool calls in
// parallel goroutines).
type Recorder struct {
	mu      sync.Mutex
	file    *os.File
	turn    int
	pending map[string]string // tool call ID → inferred path
}

// NewRecorder creates a timeline recorder writing to path. The file is
// opened in append mode so it survives process restarts.
func NewRecorder(path string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Recorder{file: f, pending: map[string]string{}}, nil
}

// NewTurn advances the turn counter. Call at the start of each user turn.
func (r *Recorder) NewTurn() {
	r.mu.Lock()
	r.turn++
	r.mu.Unlock()
}

// Record appends one entry to the timeline.
func (r *Recorder) Record(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Turn == 0 {
		e.Turn = r.turn
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	data, _ := json.Marshal(e)
	r.file.Write(append(data, '\n'))
}

// Close flushes and closes the underlying file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}

// ── Event → Entry conversion ──────────────────────────────

// RecordEvent converts an agent Event into a timeline Entry and records it.
// Only tool calls, results, and notices are recorded; text deltas are
// skipped (too noisy).
func (r *Recorder) RecordEvent(ev event.Event) {
	switch ev.Kind {
	case event.ToolDispatch:
		path := extractPath(ev.Tool.Name, ev.Tool.Args)
		if path != "" {
			r.mu.Lock()
			r.pending[ev.Tool.ID] = path
			r.mu.Unlock()
		}
		r.Record(Entry{
			Kind:     "tool_dispatch",
			ToolName: ev.Tool.Name,
			Detail:   summarizeToolArgs(ev.Tool.Name, ev.Tool.Args),
			Path:     path,
		})
	case event.ToolResult:
		success := ev.Tool.Err == "" && !ev.Tool.Blocked
		r.mu.Lock()
		path := r.pending[ev.Tool.ID]
		delete(r.pending, ev.Tool.ID)
		r.mu.Unlock()
		r.Record(Entry{
			Kind:     "tool_result",
			ToolName: ev.Tool.Name,
			Detail:   firstLine(ev.Tool.Output, 200),
			Success:  success,
			Path:     path,
		})
	case event.Notice:
		r.Record(Entry{
			Kind:   "notice",
			Detail: ev.Text,
		})
	case event.Phase:
		r.Record(Entry{
			Kind:   "phase",
			Detail: ev.Text,
		})
	case event.TurnStarted:
		r.NewTurn()
	}
}

// ── Change tracking (FanBox "变更收件箱") ─────────────────

// ChangedFile represents a file that was modified during the session.
type ChangedFile struct {
	Path       string    `json:"path"`
	Turns      []int     `json:"turns"`       // which turns touched this file
	Operations []string  `json:"operations"`  // "write", "edit", "bash"
	LastTouch  time.Time `json:"last_touch"`
}

// LoadChanges reads the timeline file and returns all files that were
// modified during the session, grouped by path.
func LoadChanges(timelinePath string) ([]ChangedFile, error) {
	data, err := os.ReadFile(timelinePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	byPath := map[string]*ChangedFile{}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Path == "" || !e.Success {
			continue
		}
		cf, ok := byPath[e.Path]
		if !ok {
			cf = &ChangedFile{Path: e.Path}
			byPath[e.Path] = cf
		}
		cf.Operations = append(cf.Operations, e.ToolName)
		cf.Turns = append(cf.Turns, e.Turn)
		if e.Timestamp.After(cf.LastTouch) {
			cf.LastTouch = e.Timestamp
		}
	}

	var out []ChangedFile
	for _, cf := range byPath {
		// Deduplicate operations
		seen := map[string]bool{}
		unique := cf.Operations[:0]
		for _, op := range cf.Operations {
			if !seen[op] {
				seen[op] = true
				unique = append(unique, op)
			}
		}
		cf.Operations = unique
		out = append(out, *cf)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastTouch.After(out[j].LastTouch)
	})
	return out, nil
}

// ── Replay ─────────────────────────────────────────────────

// LoadTimeline reads the full timeline for replay.
func LoadTimeline(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// FormatTimeline formats entries as a human-readable timeline.
func FormatTimeline(entries []Entry) string {
	var sb strings.Builder
	sb.WriteString("Session Timeline\n")
	sb.WriteString("────────────────\n\n")

	currentTurn := 0
	for _, e := range entries {
		if e.Turn != currentTurn {
			currentTurn = e.Turn
			fmt.Fprintf(&sb, "\n── Turn %d ──\n", e.Turn)
		}
		icon := "  "
		switch e.Kind {
		case "tool_dispatch":
			icon = "⚙"
		case "tool_result":
			if e.Success {
				icon = " ✓"
			} else {
				icon = " ✗"
			}
		case "notice":
			icon = "ℹ"
		case "phase":
			icon = "●"
		}
		fmt.Fprintf(&sb, "%s %s", icon, e.ToolName)
		if e.Detail != "" {
			fmt.Fprintf(&sb, " — %s", e.Detail)
		}
		if e.Path != "" {
			fmt.Fprintf(&sb, " [%s]", filepath.Base(e.Path))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// FormatChanges formats the change inbox as a summary.
func FormatChanges(changes []ChangedFile) string {
	if len(changes) == 0 {
		return "No files changed this session.\n"
	}
	var sb strings.Builder
	sb.WriteString("Changed files this session\n")
	sb.WriteString("─────────────────────────\n")
	for _, cf := range changes {
		icons := ""
		for _, op := range cf.Operations {
			switch op {
			case "write_file":
				icons += "✎"
			case "edit_file":
				icons += "✏"
			case "multi_edit":
				icons += "✏✏"
			case "bash":
				icons += "⚡"
			default:
				icons += "·"
			}
		}
		fmt.Fprintf(&sb, "  %s  %s", icons, cf.Path)
		if len(cf.Turns) > 1 {
			fmt.Fprintf(&sb, "  (%d turns)", len(cf.Turns))
		}
		sb.WriteByte('\n')
	}
	sb.WriteString(fmt.Sprintf("\n%d file(s) changed.\n", len(changes)))
	return sb.String()
}

// ── Helpers ────────────────────────────────────────────────

func summarizeToolArgs(name string, args string) string {
	if args == "" {
		return ""
	}
	switch name {
	case "bash":
		var p struct{ Command string `json:"command"` }
		if json.Unmarshal([]byte(args), &p) == nil && p.Command != "" {
			return p.Command
		}
	case "write_file", "edit_file":
		var p struct{ Path string `json:"path"` }
		if json.Unmarshal([]byte(args), &p) == nil && p.Path != "" {
			return filepath.Base(p.Path)
		}
	}
	return ""
}

func extractPath(name string, args string) string {
	switch name {
	case "write_file", "edit_file":
		var p struct{ Path string `json:"path"` }
		if json.Unmarshal([]byte(args), &p) == nil && p.Path != "" {
			return p.Path
		}
	case "multi_edit":
		var p struct{ Path string `json:"path"` }
		if json.Unmarshal([]byte(args), &p) == nil && p.Path != "" {
			return p.Path
		}
	}
	return ""
}

func firstLine(s string, maxLen int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
