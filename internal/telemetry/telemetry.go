// Package telemetry collects and reports anonymized usage telemetry:
// turn counts, token usage, tool call frequency, cache hit rates, and
// session duration. All data is local-only unless explicitly opted in.
// Adapted from claw-code's telemetry crate.
package telemetry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event is one telemetry event.
type Event struct {
	Name      string         `json:"name"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
	SessionID string         `json:"session_id"`
}

// Collector gathers telemetry events and computes aggregates.
type Collector struct {
	mu       sync.Mutex
	events   []Event
	sessionID string
	maxEvents int
}

// NewCollector creates a telemetry collector.
func NewCollector(sessionID string, maxEvents int) *Collector {
	if maxEvents <= 0 {
		maxEvents = 5000
	}
	return &Collector{sessionID: sessionID, maxEvents: maxEvents}
}

// Record adds a telemetry event.
func (c *Collector) Record(name string, data map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := Event{Name: name, Timestamp: time.Now(), Data: data, SessionID: c.sessionID}
	c.events = append(c.events, e)
	if len(c.events) > c.maxEvents {
		c.events = c.events[len(c.events)-c.maxEvents:]
	}
}

// TurnStart records the start of a new turn.
func (c *Collector) TurnStart(turn int) {
	c.Record("turn_start", map[string]any{"turn": turn})
}

// TurnEnd records the end of a turn with token usage.
func (c *Collector) TurnEnd(turn int, promptTokens, completionTokens, cacheHit, cacheMiss int, duration time.Duration) {
	c.Record("turn_end", map[string]any{
		"turn": turn, "prompt_tokens": promptTokens, "completion_tokens": completionTokens,
		"cache_hit": cacheHit, "cache_miss": cacheMiss, "duration_ms": duration.Milliseconds(),
	})
}

// ToolCall records a tool invocation.
func (c *Collector) ToolCall(name string, success bool, duration time.Duration) {
	c.Record("tool_call", map[string]any{
		"tool": name, "success": success, "duration_ms": duration.Milliseconds(),
	})
}

// PermissionDeny records a denied tool call.
func (c *Collector) PermissionDeny(tool, reason string) {
	c.Record("permission_deny", map[string]any{"tool": tool, "reason": reason})
}

// ── Aggregation ──────────────────────────────────────────

// Summary is an aggregate summary of collected telemetry.
type Summary struct {
	TotalTurns       int           `json:"total_turns"`
	TotalToolCalls   int           `json:"total_tool_calls"`
	SuccessfulCalls  int           `json:"successful_calls"`
	FailedCalls      int           `json:"failed_calls"`
	DeniedCalls      int           `json:"denied_calls"`
	TotalPrompt      int64         `json:"total_prompt_tokens"`
	TotalCompletion  int64         `json:"total_completion_tokens"`
	TotalCacheHit    int64         `json:"total_cache_hit"`
	TotalCacheMiss   int64         `json:"total_cache_miss"`
	TotalDuration    time.Duration `json:"total_duration_ms"`
	ToolFrequency    map[string]int `json:"tool_frequency"`
	AvgTurnTokens    int64         `json:"avg_turn_tokens"`
	CacheHitRate     float64       `json:"cache_hit_rate"`
}

// Summarize computes aggregate statistics.
func (c *Collector) Summarize() Summary {
	c.mu.Lock()
	defer c.mu.Unlock()

	s := Summary{ToolFrequency: map[string]int{}}
	for _, e := range c.events {
		switch e.Name {
		case "turn_start":
			s.TotalTurns++
		case "turn_end":
			if t, ok := e.Data["prompt_tokens"].(int); ok {
				s.TotalPrompt += int64(t)
			}
			if t, ok := e.Data["completion_tokens"].(int); ok {
				s.TotalCompletion += int64(t)
			}
			if h, ok := e.Data["cache_hit"].(int); ok {
				s.TotalCacheHit += int64(h)
			}
			if m, ok := e.Data["cache_miss"].(int); ok {
				s.TotalCacheMiss += int64(m)
			}
			if d, ok := e.Data["duration_ms"].(int64); ok {
				s.TotalDuration += time.Duration(d) * time.Millisecond
			}
		case "tool_call":
			s.TotalToolCalls++
			if success, ok := e.Data["success"].(bool); ok {
				if success {
					s.SuccessfulCalls++
				} else {
					s.FailedCalls++
				}
			}
			if tool, ok := e.Data["tool"].(string); ok {
				s.ToolFrequency[tool]++
			}
		case "permission_deny":
			s.DeniedCalls++
		}
	}
	if s.TotalTurns > 0 {
		s.AvgTurnTokens = (s.TotalPrompt + s.TotalCompletion) / int64(s.TotalTurns)
	}
	cacheTotal := s.TotalCacheHit + s.TotalCacheMiss
	if cacheTotal > 0 {
		s.CacheHitRate = float64(s.TotalCacheHit) / float64(cacheTotal) * 100
	}
	return s
}

// FormatSummary formats the summary for display.
func (s Summary) Format() string {
	var sb strings.Builder
	sb.WriteString("Session Telemetry\n")
	sb.WriteString("─────────────────\n")
	fmt.Fprintf(&sb, "Turns: %d\n", s.TotalTurns)
	fmt.Fprintf(&sb, "Tool calls: %d (✓%d ✗%d ⊘%d)\n", s.TotalToolCalls, s.SuccessfulCalls, s.FailedCalls, s.DeniedCalls)
	fmt.Fprintf(&sb, "Tokens: %d prompt + %d completion = %d\n", s.TotalPrompt, s.TotalCompletion, s.TotalPrompt+s.TotalCompletion)
	fmt.Fprintf(&sb, "Cache: %.1f%% hit (%d/%d)\n", s.CacheHitRate, s.TotalCacheHit, s.TotalCacheHit+s.TotalCacheMiss)
	fmt.Fprintf(&sb, "Duration: %v\n", s.TotalDuration.Truncate(time.Second))
	if s.TotalTurns > 0 {
		fmt.Fprintf(&sb, "Avg tokens/turn: %d\n", s.AvgTurnTokens)
	}
	if len(s.ToolFrequency) > 0 {
		sb.WriteString("\nTool frequency:\n")
		type tf struct{ name string; count int }
		var freq []tf
		for n, c := range s.ToolFrequency {
			freq = append(freq, tf{n, c})
		}
		sort.Slice(freq, func(i, j int) bool { return freq[i].count > freq[j].count })
		for _, f := range freq {
			fmt.Fprintf(&sb, "  %s: %d\n", f.name, f.count)
		}
	}
	return sb.String()
}

// Events returns all collected events.
func (c *Collector) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	copy(out, c.events)
	return out
}

// Count returns the number of collected events.
func (c *Collector) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// Reset clears all events.
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}
