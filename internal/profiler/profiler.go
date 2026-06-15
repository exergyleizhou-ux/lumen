// Package profiler provides runtime profiling for agent operations:
// CPU time per tool call, memory allocations, session memory usage,
// and turn latency breakdown. Used to identify slow operations and
// optimize agent performance.
package profiler

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Span represents one measured operation.
type Span struct {
	Name      string        `json:"name"`
	Category  string        `json:"category"` // "tool_call", "stream", "compact", "execute"
	Start     time.Time     `json:"start"`
	Duration  time.Duration `json:"duration"`
	AllocBytes int64        `json:"alloc_bytes,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// Profile is a collection of spans for one turn or session.
type Profile struct {
	mu     sync.Mutex
	spans  []Span
	name   string
	start  time.Time
	timer  time.Time
}

// NewProfile creates a profiler.
func NewProfile(name string) *Profile {
	now := time.Now()
	return &Profile{name: name, start: now, timer: now}
}

// Start begins a new span. Returns a function to call when the span ends.
func (p *Profile) Start(name, category string) func() {
	start := time.Now()
	return func() {
		p.mu.Lock()
		p.spans = append(p.spans, Span{
			Name: name, Category: category, Start: start,
			Duration: time.Since(start),
		})
		p.mu.Unlock()
	}
}

// Span runs fn and records its duration.
func (p *Profile) Span(name, category string, fn func()) {
	done := p.Start(name, category)
	fn()
	done()
}

// Record adds a pre-computed span.
func (p *Profile) Record(s Span) {
	p.mu.Lock()
	p.spans = append(p.spans, s)
	p.mu.Unlock()
}

// TotalDuration returns the sum of all spans.
func (p *Profile) TotalDuration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	var total time.Duration
	for _, s := range p.spans {
		total += s.Duration
	}
	return total
}

// Spans returns all recorded spans.
func (p *Profile) Spans() []Span {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Span, len(p.spans))
	copy(out, p.spans)
	return out
}

// ── Analysis ───────────────────────────────────────────

// Breakdown is a category-level summary.
type Breakdown struct {
	Category string        `json:"category"`
	Count    int           `json:"count"`
	Total    time.Duration `json:"total"`
	Avg      time.Duration `json:"avg"`
	Min      time.Duration `json:"min"`
	Max      time.Duration `json:"max"`
	Percent  float64       `json:"percent"`
}

// Breakdowns returns category-level statistics.
func (p *Profile) Breakdowns() []Breakdown {
	p.mu.Lock()
	defer p.mu.Unlock()

	type catStats struct {
		total time.Duration
		count int
		min   time.Duration
		max   time.Duration
	}
	cats := map[string]*catStats{}
	var grandTotal time.Duration

	for _, s := range p.spans {
		cs, ok := cats[s.Category]
		if !ok {
			cs = &catStats{min: s.Duration, max: s.Duration}
			cats[s.Category] = cs
		}
		cs.total += s.Duration
		cs.count++
		if s.Duration < cs.min { cs.min = s.Duration }
		if s.Duration > cs.max { cs.max = s.Duration }
		grandTotal += s.Duration
	}

	var out []Breakdown
	for cat, cs := range cats {
		pct := 0.0
		if grandTotal > 0 { pct = float64(cs.total) / float64(grandTotal) * 100 }
		out = append(out, Breakdown{
			Category: cat, Count: cs.count, Total: cs.total,
			Avg: cs.total / time.Duration(cs.count), Min: cs.min, Max: cs.max, Percent: pct,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	return out
}

// TopSpans returns the N slowest spans.
func (p *Profile) TopSpans(n int) []Span {
	spans := p.Spans()
	sort.Slice(spans, func(i, j int) bool { return spans[i].Duration > spans[j].Duration })
	if n > len(spans) { n = len(spans) }
	return spans[:n]
}

// Format formats the profile for display.
func (p *Profile) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Profile: %s\n", p.name)
	fmt.Fprintf(&sb, "────────────────────────\n")
	fmt.Fprintf(&sb, "Total time: %v\n", p.TotalDuration())
	fmt.Fprintf(&sb, "Spans: %d\n\n", len(p.Spans()))

	sb.WriteString("By category:\n")
	for _, b := range p.Breakdowns() {
		fmt.Fprintf(&sb, "  %-12s %3d calls %8v avg (%.0f%%)\n", b.Category, b.Count, b.Avg, b.Percent)
	}

	sb.WriteString("\nTop 5 slowest:\n")
	for _, s := range p.TopSpans(5) {
		fmt.Fprintf(&sb, "  %-20s %8v\n", s.Name, s.Duration)
	}
	return sb.String()
}

// ── Session profile ──────────────────────────────────────

// SessionProfiler aggregates profiles across turns.
type SessionProfiler struct {
	mu       sync.Mutex
	profiles []*Profile
}

// NewSessionProfiler creates a session-level profiler.
func NewSessionProfiler() *SessionProfiler {
	return &SessionProfiler{}
}

// NewTurn creates a new per-turn profile.
func (sp *SessionProfiler) NewTurn(turn int) *Profile {
	p := NewProfile(fmt.Sprintf("turn-%d", turn))
	sp.mu.Lock()
	sp.profiles = append(sp.profiles, p)
	sp.mu.Unlock()
	return p
}

// TotalDuration returns total time across all turns.
func (sp *SessionProfiler) TotalDuration() time.Duration {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	var total time.Duration
	for _, p := range sp.profiles { total += p.TotalDuration() }
	return total
}

// SlowestTurns returns the N slowest turn indices.
func (sp *SessionProfiler) SlowestTurns(n int) []int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	type idxDur struct{ idx int; dur time.Duration }
	var items []idxDur
	for i, p := range sp.profiles { items = append(items, idxDur{i, p.TotalDuration()}) }
	sort.Slice(items, func(a, b int) bool { return items[a].dur > items[b].dur })
	out := make([]int, n)
	for i := 0; i < n && i < len(items); i++ { out[i] = items[i].idx }
	return out
}

// FormatSession formats a session summary.
func (sp *SessionProfiler) FormatSession() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Session Profile\n───────────────\n")
	fmt.Fprintf(&sb, "Turns: %d\n", len(sp.profiles))
	fmt.Fprintf(&sb, "Total time: %v\n", sp.TotalDuration())
	if len(sp.profiles) > 0 {
		avg := sp.TotalDuration() / time.Duration(len(sp.profiles))
		fmt.Fprintf(&sb, "Avg per turn: %v\n", avg)
	}
	return sb.String()
}
