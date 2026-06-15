// Package tracepoint provides lightweight runtime tracepoint instrumentation
// for agent execution paths. It supports static and dynamic tracepoints with
// conditional firing, hit counting, and flame-chart-style output.
package tracepoint

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Point is a named tracepoint.
type Point struct {
	Name      string               `json:"name"`
	Category  string               `json:"category"`
	Enabled   bool                 `json:"enabled"`
	Hits      int64                `json:"hits"`
	LastHit   time.Time            `json:"last_hit,omitempty"`
	Condition func() bool          `json:"-"`
	Action    func(hitCount int64) `json:"-"`
}

// Registry manages tracepoints.
type Registry struct {
	mu     sync.RWMutex
	points map[string]*Point
}

// NewRegistry creates a tracepoint registry.
func NewRegistry() *Registry {
	return &Registry{points: map[string]*Point{}}
}

// Register adds a tracepoint.
func (r *Registry) Register(name, category string) *Point {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.points[name]; ok {
		return p
	}
	p := &Point{Name: name, Category: category, Enabled: true}
	r.points[name] = p
	return p
}

// Enable activates a tracepoint.
func (r *Registry) Enable(name string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.points[name]; ok {
		p.Enabled = true
	}
}

// Disable deactivates a tracepoint.
func (r *Registry) Disable(name string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.points[name]; ok {
		p.Enabled = false
	}
}

// Fire triggers a tracepoint if enabled.
func (r *Registry) Fire(name string) {
	r.mu.RLock()
	p, ok := r.points[name]
	r.mu.RUnlock()
	if !ok || !p.Enabled {
		return
	}
	if p.Condition != nil && !p.Condition() {
		return
	}
	hits := atomic.AddInt64(&p.Hits, 1)
	p.LastHit = time.Now()
	if p.Action != nil {
		p.Action(hits)
	}
}

// SetCondition attaches a condition to a tracepoint.
func (r *Registry) SetCondition(name string, cond func() bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.points[name]; ok {
		p.Condition = cond
	}
}

// SetAction attaches an action to a tracepoint.
func (r *Registry) SetAction(name string, action func(hitCount int64)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.points[name]; ok {
		p.Action = action
	}
}

// Hits returns the hit count for a tracepoint.
func (r *Registry) Hits(name string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.points[name]; ok {
		return atomic.LoadInt64(&p.Hits)
	}
	return 0
}

// All returns all tracepoints sorted by name.
func (r *Registry) All() []*Point {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Point
	for _, p := range r.points {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ByCategory returns tracepoints grouped by category.
func (r *Registry) ByCategory() map[string][]*Point {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := map[string][]*Point{}
	for _, p := range r.points {
		result[p.Category] = append(result[p.Category], p)
	}
	for k := range result {
		sort.Slice(result[k], func(i, j int) bool { return result[k][i].Name < result[k][j].Name })
	}
	return result
}

// Reset clears all hit counts.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.points {
		atomic.StoreInt64(&p.Hits, 0)
	}
}

// FormatTracepoints formats tracepoint statistics.
func (r *Registry) FormatTracepoints() string {
	var sb strings.Builder
	byCat := r.ByCategory()
	categories := make([]string, 0, len(byCat))
	for c := range byCat {
		categories = append(categories, c)
	}
	sort.Strings(categories)

	fmt.Fprintf(&sb, "Tracepoints (%d):\n%s\n\n", len(r.points), strings.Repeat("─", 50))
	for _, cat := range categories {
		fmt.Fprintf(&sb, "  [%s]\n", cat)
		for _, p := range byCat[cat] {
			state := "🟢"
			if !p.Enabled {
				state = "⚫"
			}
			fmt.Fprintf(&sb, "    %s %-30s hits=%-8d  last=%v\n",
				state, p.Name, atomic.LoadInt64(&p.Hits), p.LastHit.Format("15:04:05"))
		}
	}
	return sb.String()
}

// ── Trace Session ─────────────────────────────────────────

// Span is a timed execution span.
type Span struct {
	Name     string            `json:"name"`
	Start    time.Time         `json:"start"`
	End      time.Time         `json:"end"`
	Duration time.Duration     `json:"duration"`
	Parent   string            `json:"parent,omitempty"`
	Children []*Span           `json:"-"`
	Tags     map[string]string `json:"tags,omitempty"`
}

// Session records a hierarchy of timed spans.
type Session struct {
	mu      sync.Mutex
	spans   map[string]*Span
	roots   []*Span
	current []string // stack of active spans
	nextID  int64
}

// NewSession creates a trace session.
func NewSession() *Session {
	return &Session{spans: map[string]*Span{}}
}

// Begin starts a new span, optionally as a child of the current span.
func (s *Session) Begin(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("span-%d", atomic.AddInt64(&s.nextID, 1))
	span := &Span{Name: name, Start: time.Now(), Tags: map[string]string{}}
	if len(s.current) > 0 {
		parentID := s.current[len(s.current)-1]
		span.Parent = parentID
		if parent, ok := s.spans[parentID]; ok {
			parent.Children = append(parent.Children, span)
		}
	} else {
		s.roots = append(s.roots, span)
	}
	s.spans[id] = span
	s.current = append(s.current, id)
	return id
}

// End finishes the most recently started span.
func (s *Session) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.current) == 0 {
		return
	}
	id := s.current[len(s.current)-1]
	s.current = s.current[:len(s.current)-1]
	if span, ok := s.spans[id]; ok {
		span.End = time.Now()
		span.Duration = span.End.Sub(span.Start)
	}
}

// Tag adds a key-value tag to the current span.
func (s *Session) Tag(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.current) == 0 {
		return
	}
	id := s.current[len(s.current)-1]
	if span, ok := s.spans[id]; ok {
		span.Tags[key] = value
	}
}

// TotalDuration returns the total wall-clock duration.
func (s *Session) TotalDuration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalDurationLocked()
}

// totalDurationLocked assumes the caller holds the lock.
func (s *Session) totalDurationLocked() time.Duration {
	var total time.Duration
	for _, root := range s.roots {
		if !root.End.IsZero() {
			d := root.End.Sub(root.Start)
			if d > total {
				total = d
			}
		}
	}
	return total
}

// FormatFlameChart renders a text flame chart.
func (s *Session) FormatFlameChart() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sb strings.Builder
	totalWidth := 80
	totalDur := s.totalDurationLocked()
	if totalDur == 0 {
		return "No trace data.\n"
	}

	fmt.Fprintf(&sb, "Flame Chart (%v):\n%s\n\n", totalDur, strings.Repeat("─", totalWidth))
	for _, root := range s.roots {
		s.formatSpan(&sb, root, 0, totalDur, totalWidth)
	}
	return sb.String()
}

func (s *Session) formatSpan(sb *strings.Builder, span *Span, depth int, totalDur time.Duration, totalWidth int) {
	indent := strings.Repeat("  ", depth)
	width := int(float64(span.Duration) / float64(totalDur) * float64(totalWidth))
	if width < 1 {
		width = 1
	}
	if width > totalWidth {
		width = totalWidth
	}

	bar := strings.Repeat("█", width)
	fmt.Fprintf(sb, "%s%-20s %s %v\n", indent, span.Name, bar, span.Duration)
	for _, child := range span.Children {
		s.formatSpan(sb, child, depth+1, totalDur, totalWidth)
	}
}

// FormatTree renders a tree of spans.
func (s *Session) FormatTree() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Trace Tree (%d spans):\n%s\n\n", len(s.spans), strings.Repeat("─", 50))
	for _, root := range s.roots {
		s.formatTreeRec(&sb, root, 0)
	}
	return sb.String()
}

func (s *Session) formatTreeRec(sb *strings.Builder, span *Span, depth int) {
	indent := strings.Repeat("  ", depth)
	prefix := "├─"
	if depth == 0 {
		prefix = "●"
	}
	fmt.Fprintf(sb, "%s%s %s [%v]", indent, prefix, span.Name, span.Duration)
	if len(span.Tags) > 0 {
		var tags []string
		for k, v := range span.Tags {
			tags = append(tags, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(tags)
		fmt.Fprintf(sb, " {%s}", strings.Join(tags, ", "))
	}
	sb.WriteByte('\n')
	for _, child := range span.Children {
		s.formatTreeRec(sb, child, depth+1)
	}
}
