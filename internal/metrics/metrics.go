// Package metrics provides lightweight telemetry: counters, gauges, and
// timers for measuring agent performance. Thread-safe, zero-allocation.
package metrics

import ("fmt";"sort";"strings";"sync";"sync/atomic";"time")

// Counter is a monotonically increasing integer counter.
type Counter struct{ val atomic.Int64 }
func NewCounter() *Counter            { return &Counter{} }
func (c *Counter) Inc()               { c.val.Add(1) }
func (c *Counter) Add(n int64)        { c.val.Add(n) }
func (c *Counter) Value() int64       { return c.val.Load() }

// Gauge is an integer value that can go up or down.
type Gauge struct{ val atomic.Int64 }
func NewGauge() *Gauge                { return &Gauge{} }
func (g *Gauge) Set(n int64)          { g.val.Store(n) }
func (g *Gauge) Value() int64         { return g.val.Load() }

// Timer measures elapsed time and records it to a Histogram.
type Timer struct{ start time.Time; h *Histogram }
func StartTimer(h *Histogram) *Timer   { return &Timer{start: time.Now(), h: h} }
func (t *Timer) Stop()                 { t.h.Observe(float64(time.Since(t.start).Milliseconds())) }

// Histogram tracks a distribution of float64 values across pre-defined buckets.
type Histogram struct {
	mu      sync.Mutex
	buckets []int64
	bounds  []float64
	count   int64
	sum     float64
}

// NewHistogram creates a histogram with the given bucket boundaries.
func NewHistogram(bounds []float64) *Histogram {
	return &Histogram{bounds: bounds, buckets: make([]int64, len(bounds)+1)}
}

// Observe records a value in the appropriate bucket.
func (hist *Histogram) Observe(v float64) {
	hist.mu.Lock(); defer hist.mu.Unlock()
	hist.count++; hist.sum += v
	for i, b := range hist.bounds {
		if v <= b { hist.buckets[i]++; return }
	}
	hist.buckets[len(hist.bounds)]++
}

// Snapshot returns the current count, sum, and bucket values.
func (hist *Histogram) Snapshot() (count int64, sum float64, buckets []int64) {
	hist.mu.Lock(); defer hist.mu.Unlock()
	bs := make([]int64, len(hist.buckets)); copy(bs, hist.buckets)
	return hist.count, hist.sum, bs
}

// Registry holds named metrics.
type Registry struct {
	mu         sync.Mutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

// NewRegistry creates an empty metric registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]*Counter{},
		gauges:     map[string]*Gauge{},
		histograms: map[string]*Histogram{},
	}
}

// Counter returns or creates a named counter.
func (r *Registry) Counter(name string) *Counter {
	r.mu.Lock(); defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok { return c }
	c := NewCounter(); r.counters[name] = c; return c
}

// Gauge returns or creates a named gauge.
func (r *Registry) Gauge(name string) *Gauge {
	r.mu.Lock(); defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok { return g }
	g := NewGauge(); r.gauges[name] = g; return g
}

// Histogram returns or creates a named histogram.
func (r *Registry) Histogram(name string, bounds []float64) *Histogram {
	r.mu.Lock(); defer r.mu.Unlock()
	if h, ok := r.histograms[name]; ok { return h }
	h := NewHistogram(bounds); r.histograms[name] = h; return h
}

// FormatStats returns a human-readable summary of all metrics.
func (r *Registry) FormatStats() string {
	r.mu.Lock(); defer r.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Metrics (%d counters, %d gauges, %d histograms):\n\n",
		len(r.counters), len(r.gauges), len(r.histograms)))

	type pair struct{ k string; v int64 }
	var items []pair
	for n, c := range r.counters { items = append(items, pair{n, c.Value()}) }
	sort.Slice(items, func(i, j int) bool { return items[i].v > items[j].v })
	sb.WriteString("Counters:\n")
	for _, it := range items { fmt.Fprintf(&sb, "  %-30s %d\n", it.k, it.v) }
	return sb.String()
}
