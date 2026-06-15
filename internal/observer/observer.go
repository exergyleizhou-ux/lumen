// Package observer provides OpenTelemetry-based distributed tracing, span
// management, metrics export, and log correlation for Lumen agent sessions.
// It ties into the agent loop for request-level observability.
package observer

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// SpanKind classifies a span.
type SpanKind int

const (
	SpanInternal SpanKind = iota
	SpanClient
	SpanServer
	SpanProducer
	SpanConsumer
)

func (k SpanKind) String() string {
	switch k {
	case SpanClient: return "client"
	case SpanServer: return "server"
	case SpanProducer: return "producer"
	case SpanConsumer: return "consumer"
	default: return "internal"
	}
}

// Span represents a single operation trace.
type Span struct {
	ID         string            `json:"id"`
	TraceID    string            `json:"trace_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Name       string            `json:"name"`
	Kind       SpanKind          `json:"kind"`
	StartedAt  time.Time         `json:"started_at"`
	EndedAt    time.Time         `json:"ended_at,omitempty"`
	Status     string            `json:"status"` // ok, error
	Attributes map[string]string `json:"attributes,omitempty"`
	Events     []SpanEvent       `json:"events,omitempty"`
}

// SpanEvent is a timestamped event within a span.
type SpanEvent struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Tracer creates and manages spans.
type Tracer struct {
	mu     sync.Mutex
	spans  map[string]*Span
	traces map[string][]*Span
	nextID int64
	maxSpans int
}

// NewTracer creates a tracer.
func NewTracer() *Tracer {
	return &Tracer{spans: map[string]*Span{}, traces: map[string][]*Span{}, maxSpans: 10000}
}

// StartSpan begins a new span.
func (t *Tracer) StartSpan(traceID, parentID, name string, kind SpanKind) *Span {
	t.mu.Lock(); defer t.mu.Unlock()
	t.nextID++
	span := &Span{ID: fmt.Sprintf("span-%d", t.nextID), TraceID: traceID, ParentID: parentID, Name: name, Kind: kind, StartedAt: time.Now(), Status: "ok", Attributes: map[string]string{}}
	t.spans[span.ID] = span
	t.traces[traceID] = append(t.traces[traceID], span)
	if len(t.spans) > t.maxSpans { t.prune() }
	return span
}

// EndSpan finishes a span.
func (t *Tracer) EndSpan(spanID string, status string) {
	t.mu.Lock(); defer t.mu.Unlock()
	if s, ok := t.spans[spanID]; ok { s.EndedAt = time.Now(); s.Status = status }
}

// AddEvent adds an event to a span.
func (t *Tracer) AddEvent(spanID, name string, attrs map[string]string) {
	t.mu.Lock(); defer t.mu.Unlock()
	if s, ok := t.spans[spanID]; ok {
		s.Events = append(s.Events, SpanEvent{Name: name, Timestamp: time.Now(), Attributes: attrs})
	}
}

// SetAttribute sets a span attribute.
func (t *Tracer) SetAttribute(spanID, key, value string) {
	t.mu.Lock(); defer t.mu.Unlock()
	if s, ok := t.spans[spanID]; ok { s.Attributes[key] = value }
}

// GetTrace returns all spans for a trace.
func (t *Tracer) GetTrace(traceID string) []*Span {
	t.mu.Lock(); defer t.mu.Unlock()
	var spans []*Span
	for _, s := range t.spans { if s.TraceID == traceID { spans = append(spans, s) } }
	sort.Slice(spans, func(i, j int) bool { return spans[i].StartedAt.Before(spans[j].StartedAt) })
	return spans
}

// SpanCount returns the number of active spans.
func (t *Tracer) SpanCount() int { t.mu.Lock(); defer t.mu.Unlock(); return len(t.spans) }

// FormatTrace returns a text timeline of a trace.
func (t *Tracer) FormatTrace(traceID string) string {
	spans := t.GetTrace(traceID)
	if len(spans) == 0 { return fmt.Sprintf("Trace %s not found.\n", traceID) }

	var sb strings.Builder
	fmt.Fprintf(&sb, "Trace: %s (%d spans)\n%s\n\n", traceID, len(spans), strings.Repeat("─", 60))
	for _, s := range spans {
		indent := 0
		for _, p := range spans { if p.ID == s.ParentID { indent = 2; break } }
		indentStr := strings.Repeat("  ", indent)
		dur := time.Duration(0)
		if !s.EndedAt.IsZero() { dur = s.EndedAt.Sub(s.StartedAt) }
		icon := "✅"; if s.Status == "error" { icon = "🔴" }
		fmt.Fprintf(&sb, "%s%s %s [%s] %v", indentStr, icon, s.Name, s.Kind, dur)
		if len(s.Attributes) > 0 { fmt.Fprintf(&sb, " %v", s.Attributes) }
		sb.WriteByte('\n')
		for _, ev := range s.Events {
			fmt.Fprintf(&sb, "%s    📍 %s %v\n", indentStr, ev.Name, ev.Timestamp.Format("15:04:05.000"))
		}
	}
	return sb.String()
}

func (t *Tracer) prune() {
	var oldest *Span
	for _, s := range t.spans {
		if oldest == nil || s.StartedAt.Before(oldest.StartedAt) { oldest = s }
	}
	if oldest != nil { delete(t.spans, oldest.ID) }
}

// ── LogCorrelator ─────────────────────────────────────────

// LogCorrelator links log entries to traces.
type LogCorrelator struct {
	mu      sync.Mutex
	logs    map[string][]LogEntry
	maxLogs int
}

// LogEntry is a log line with trace context.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	TraceID   string    `json:"trace_id,omitempty"`
	SpanID    string    `json:"span_id,omitempty"`
}

// NewLogCorrelator creates a log correlator.
func NewLogCorrelator() *LogCorrelator {
	return &LogCorrelator{logs: map[string][]LogEntry{}, maxLogs: 5000}
}

// Log records a log entry.
func (lc *LogCorrelator) Log(traceID, spanID, level, message string) {
	lc.mu.Lock(); defer lc.mu.Unlock()
	entry := LogEntry{Timestamp: time.Now(), Level: level, Message: message, TraceID: traceID, SpanID: spanID}
	lc.logs[traceID] = append(lc.logs[traceID], entry)
	if len(lc.logs[traceID]) > lc.maxLogs { lc.logs[traceID] = lc.logs[traceID][1:] }
}

// GetTraceLogs returns logs for a trace.
func (lc *LogCorrelator) GetTraceLogs(traceID string) []LogEntry {
	lc.mu.Lock(); defer lc.mu.Unlock()
	out := make([]LogEntry, len(lc.logs[traceID]))
	copy(out, lc.logs[traceID])
	return out
}

// ── SampleCollector ───────────────────────────────────────

// SampleCollector collects and aggregates samples.
type SampleCollector struct {
	mu      sync.Mutex
	samples map[string][]float64
}

// NewSampleCollector creates a sample collector.
func NewSampleCollector() *SampleCollector {
	return &SampleCollector{samples: map[string][]float64{}}
}

// Record records a sample value.
func (sc *SampleCollector) Record(name string, value float64) {
	sc.mu.Lock(); defer sc.mu.Unlock()
	sc.samples[name] = append(sc.samples[name], value)
	if len(sc.samples[name]) > 1000 { sc.samples[name] = sc.samples[name][len(sc.samples[name])-1000:] }
}

// Stats returns min, max, mean for a metric.
func (sc *SampleCollector) Stats(name string) (min, max, mean float64, count int) {
	sc.mu.Lock(); defer sc.mu.Unlock()
	samples := sc.samples[name]
	if len(samples) == 0 { return }
	count = len(samples)
	min, max = samples[0], samples[0]
	var sum float64
	for _, v := range samples {
		sum += v
		if v < min { min = v }
		if v > max { max = v }
	}
	mean = sum / float64(count)
	return
}
