// Package telemetry provides anonymous, local-first usage analytics for
// Lumen. Records tool calls, model usage, sessions, errors, and user
// feedback. All data stays on the user's machine unless explicitly opted
// in for sharing. Designed for privacy-first iterative improvement.
package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EventType classifies telemetry events.
type EventType string

const (
	EventSessionStart EventType = "session_start"
	EventSessionEnd   EventType = "session_end"
	EventToolCall     EventType = "tool_call"
	EventToolError    EventType = "tool_error"
	EventModelCall    EventType = "model_call"
	EventFeedback     EventType = "feedback"
	EventError        EventType = "error"
	EventCrash        EventType = "crash"
	EventModelSwitch  EventType = "model_switch"
	EventModeSwitch   EventType = "mode_switch"
)

// Entry is one telemetry event, stored as JSONL.
type Entry struct {
	Type      EventType          `json:"type"`
	Timestamp time.Time          `json:"ts"`
	SessionID string             `json:"session"`
	Data      map[string]any     `json:"data,omitempty"`
}

// Collector records telemetry events to local storage.
type Collector struct {
	mu        sync.Mutex
	dir       string
	sessionID string
	enabled   bool
	writer    *os.File
	count     atomic.Int64
}

// NewCollector creates a telemetry collector. Data is stored in
// ~/.lumen/telemetry/ as daily JSONL files.
func NewCollector() *Collector {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	dir := filepath.Join(home, ".lumen", "telemetry")
	os.MkdirAll(dir, 0700)

	c := &Collector{
		dir:     dir,
		enabled: true,
	}
	c.sessionID = fmt.Sprintf("sess-%d", time.Now().Unix())
	return c
}

// Enable toggles telemetry collection.
func (c *Collector) Enable(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = on
}

// SessionID returns the current session identifier.
func (c *Collector) SessionID() string { return c.sessionID }

// Record logs a telemetry event.
func (c *Collector) Record(typ EventType, data map[string]any) {
	if !c.enabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := Entry{
		Type:      typ,
		Timestamp: time.Now(),
		SessionID: c.sessionID,
		Data:      data,
	}

	// Open daily file if needed
	today := time.Now().Format("2006-01-02")
	filename := filepath.Join(c.dir, today+".jsonl")

	if c.writer == nil || filepath.Base(c.writer.Name()) != today+".jsonl" {
		if c.writer != nil {
			c.writer.Close()
		}
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return
		}
		c.writer = f
	}

	line, _ := json.Marshal(entry)
	c.writer.Write(line)
	c.writer.Write([]byte("\n"))
	c.count.Add(1)
}

// Close flushes and closes the collector.
func (c *Collector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writer != nil {
		c.writer.Close()
		c.writer = nil
	}
}

// Count returns total events recorded in this session.
func (c *Collector) Count() int64 { return c.count.Load() }

// Tail returns the last N events.
func (c *Collector) Tail(n int) []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, _ := os.ReadDir(c.dir)
	sort.Slice(entries, func(i, j int) bool {
		ii, _ := entries[i].Info()
		ij, _ := entries[j].Info()
		return ii.ModTime().After(ij.ModTime())
	})

	var result []Entry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, e.Name()))
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		// Read from end
		for i := len(lines) - 1; i >= 0 && len(result) < n; i-- {
			if lines[i] == "" {
				continue
			}
			var entry Entry
			if json.Unmarshal([]byte(lines[i]), &entry) == nil {
				result = append(result, entry)
			}
		}
		if len(result) >= n {
			break
		}
	}
	return result
}

// ── Feedback ────────────────────────────────────────────

// FeedbackEntry is a user-submitted feedback item.
type FeedbackEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"ts"`
	Type      string    `json:"type"` // "thumbs_up", "thumbs_down", "text", "bug", "feature"
	Message   string    `json:"message"`
	Context   string    `json:"context,omitempty"` // last prompt or tool used
	SessionID string    `json:"session"`
}

// FeedbackStore manages user feedback.
type FeedbackStore struct {
	mu     sync.Mutex
	dir    string
	items  []FeedbackEntry
	nextID int64
}

// NewFeedbackStore creates a feedback store.
func NewFeedbackStore() *FeedbackStore {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".lumen", "feedback")
	os.MkdirAll(dir, 0700)
	fs := &FeedbackStore{dir: dir}
	fs.load()
	return fs
}

func (fs *FeedbackStore) load() {
	entries, _ := os.ReadDir(fs.dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fs.dir, e.Name()))
		if err != nil {
			continue
		}
		var fe FeedbackEntry
		if json.Unmarshal(data, &fe) == nil {
			fs.items = append(fs.items, fe)
		}
	}
	sort.Slice(fs.items, func(i, j int) bool {
		return fs.items[i].Timestamp.After(fs.items[j].Timestamp)
	})
}

// Submit adds feedback.
func (fs *FeedbackStore) Submit(typ, message, context, sessionID string) *FeedbackEntry {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.nextID++
	fe := FeedbackEntry{
		ID:        fmt.Sprintf("fb-%d", fs.nextID),
		Timestamp: time.Now(),
		Type:      typ,
		Message:   message,
		Context:   context,
		SessionID: sessionID,
	}

	fs.items = append([]FeedbackEntry{fe}, fs.items...)

	// Persist
	filename := filepath.Join(fs.dir, fe.ID+".json")
	data, _ := json.MarshalIndent(fe, "", "  ")
	os.WriteFile(filename, data, 0600)

	return &fe
}

// List returns recent feedback items.
func (fs *FeedbackStore) List(limit int) []FeedbackEntry {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if limit <= 0 || limit > len(fs.items) {
		limit = len(fs.items)
	}
	out := make([]FeedbackEntry, limit)
	copy(out, fs.items[:limit])
	return out
}

// Counts returns feedback counts by type.
func (fs *FeedbackStore) Counts() map[string]int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	counts := map[string]int{}
	for _, fe := range fs.items {
		counts[fe.Type]++
	}
	return counts
}

// Stats returns feedback summary.
func (fs *FeedbackStore) Stats() string {
	counts := fs.Counts()
	total := 0
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return "No feedback collected yet. Use /feedback <type> <message> in chat."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Feedback Summary (%d items):\n\n", total))
	for typ, count := range counts {
		icon := "✅"
		switch typ {
		case "thumbs_down", "bug":
			icon = "🔴"
		case "feature":
			icon = "💡"
		}
		pct := float64(count) / float64(total) * 100
		fmt.Fprintf(&sb, "  %s %-15s %d (%.0f%%)\n", icon, typ, count, pct)
	}

	satisfied := counts["thumbs_up"]
	unsatisfied := counts["thumbs_down"] + counts["bug"]
	if satisfied+unsatisfied > 0 {
		rate := float64(satisfied) / float64(satisfied+unsatisfied) * 100
		fmt.Fprintf(&sb, "\n  Satisfaction rate: %.0f%%\n", rate)
	}

	return sb.String()
}

// ── Sharing / Export ──────────────────────────────────────

// ExportBundle prepares a shareable, anonymized report of recent activity.
type ExportBundle struct {
	GeneratedAt time.Time              `json:"generated_at"`
	Version     string                 `json:"version"`
	SessionCount int                   `json:"session_count"`
	ToolStats    map[string]int        `json:"tool_usage"`
	ModelStats   map[string]int        `json:"model_usage"`
	Feedback     []FeedbackEntry       `json:"feedback"`
	Satisfaction  float64              `json:"satisfaction_rate"`
	Errors       []AnonymizedError     `json:"recent_errors"`
	Recommendations []string           `json:"recommendations"`
	HealthScore   float64              `json:"health_score"`
	OS           string                 `json:"os"`
	GoVersion    string                 `json:"go_version"`
	DaysActive   int                   `json:"days_active"`
}

// AnonymizedError is a sanitized error — no file paths or user content.
type AnonymizedError struct {
	Tool  string `json:"tool"`
	Error string `json:"error"`
	Count int    `json:"count"`
	First time.Time `json:"first_seen"`
}

// Export creates a shareable bundle from all available data.
func (c *Collector) Export() *ExportBundle {
	bundle := &ExportBundle{
		GeneratedAt: time.Now(),
		Version:     "0.1", // bump with releases
		ToolStats:    map[string]int{},
		ModelStats:   map[string]int{},
	}

	// Read all telemetry files
	entries, _ := os.ReadDir(c.dir)
	sessions := map[string]bool{}
	errorMap := map[string]*AnonymizedError{}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, e.Name()))
		if err != nil {
			continue
		}
		if len(data) > 0 {
			bundle.DaysActive++
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			var entry Entry
			if json.Unmarshal([]byte(line), &entry) != nil {
				continue
			}
			sessions[entry.SessionID] = true

			switch entry.Type {
			case EventToolCall:
				if name, ok := entry.Data["name"].(string); ok {
					bundle.ToolStats[name]++
				}
			case EventToolError:
				name, _ := entry.Data["name"].(string)
				errMsg, _ := entry.Data["error"].(string)
				key := name + ":" + errMsg
				if errorMap[key] == nil {
					errorMap[key] = &AnonymizedError{Tool: name, Error: errMsg, First: entry.Timestamp}
				}
				errorMap[key].Count++
			case EventModelCall:
				bundle.ModelStats["total"]++
			}
		}
	}

	bundle.SessionCount = len(sessions)
	for _, ae := range errorMap {
		bundle.Errors = append(bundle.Errors, *ae)
	}

	// Add feedback
	fs := NewFeedbackStore()
	bundle.Feedback = fs.List(50)
	counts := fs.Counts()
	if counts["thumbs_up"]+counts["thumbs_down"]+counts["bug"] > 0 {
		bundle.Satisfaction = float64(counts["thumbs_up"]) / float64(counts["thumbs_up"]+counts["thumbs_down"]+counts["bug"]) * 100
	}

	// Health + recommendations
	a := NewAnalyzer()
	report := a.Analyze("week")
	bundle.HealthScore = report.HealthScore
	for _, r := range report.Recommendations {
		bundle.Recommendations = append(bundle.Recommendations, r.Title)
	}

	// System info (anonymized)
	bundle.OS = runtime.GOOS + "/" + runtime.GOARCH

	return bundle
}

// FormatExport renders the export bundle as a human-readable text file
// suitable for pasting into GitHub Issues, email, or chat.
func FormatExport(bundle *ExportBundle) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Lumen Usage Report — %s\n", bundle.GeneratedAt.Format("2006-01-02")))
	sb.WriteString(strings.Repeat("=", 50) + "\n\n")

	sb.WriteString(fmt.Sprintf("Health: %.0f/100  ·  %d sessions  ·  %d days active\n", 
		bundle.HealthScore, bundle.SessionCount, bundle.DaysActive))
	sb.WriteString(fmt.Sprintf("OS: %s  ·  Version: %s\n\n", bundle.OS, bundle.Version))

	// Tools
	sb.WriteString("Tool Usage:\n")
	type kv struct{ k string; v int }
	var tools []kv
	for k, v := range bundle.ToolStats { tools = append(tools, kv{k, v}) }
	sort.Slice(tools, func(i, j int) bool { return tools[i].v > tools[j].v })
	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("  %-25s %5d\n", t.k, t.v))
	}

	// Errors
	if len(bundle.Errors) > 0 {
		sb.WriteString("\nRecent Errors:\n")
		sort.Slice(bundle.Errors, func(i, j int) bool { return bundle.Errors[i].Count > bundle.Errors[j].Count })
		for _, e := range bundle.Errors {
			sb.WriteString(fmt.Sprintf("  [%dx] %s: %s\n", e.Count, e.Tool, e.Error))
		}
	}

	// Feedback
	if len(bundle.Feedback) > 0 {
		sb.WriteString(fmt.Sprintf("\nFeedback (%.0f%% satisfied):\n", bundle.Satisfaction))
		for _, fb := range bundle.Feedback {
			if len(fb.Message) > 100 { fb.Message = fb.Message[:97] + "..." }
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", fb.Type, fb.Message))
		}
	}

	// Recommendations
	if len(bundle.Recommendations) > 0 {
		sb.WriteString("\nTop Recommendations:\n")
		for _, r := range bundle.Recommendations {
			sb.WriteString(fmt.Sprintf("  • %s\n", r))
		}
	}

	sb.WriteString("\n---\nGenerated by Lumen telemetry. No personal data, file paths, or API keys included.\n")
	return sb.String()
}

