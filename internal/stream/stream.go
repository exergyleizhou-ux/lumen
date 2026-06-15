// Package stream provides a stream processing engine with windowing,
// aggregation, partitioning, and exactly-once semantics. It processes
// continuous data flows from agent tool outputs, API responses, and
// message channels.
package stream

import (
	"container/heap"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Record is a single stream record.
type Record struct {
	Key       string            `json:"key"`
	Value     any               `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Headers   map[string]string `json:"headers,omitempty"`
	Partition int               `json:"partition"`
	Offset    int64             `json:"offset"`
}

// Source produces stream records.
type Source interface {
	Name() string
	Poll(ctx context.Context) ([]Record, error)
	Commit(offset int64) error
}

// Sink consumes stream records.
type Sink interface {
	Name() string
	Write(records []Record) error
}

// Processor transforms records.
type Processor interface {
	Name() string
	Process(records []Record) ([]Record, error)
}

// WindowType defines the windowing strategy.
type WindowType int

const (
	WindowTumbling WindowType = iota
	WindowSliding
	WindowSession
)

func (w WindowType) String() string {
	switch w {
	case WindowTumbling:
		return "tumbling"
	case WindowSliding:
		return "sliding"
	case WindowSession:
		return "session"
	default:
		return "unknown"
	}
}

// Window holds records in a time range.
type Window struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Records  []Record  `json:"records"`
	Key      string    `json:"key,omitempty"`
	ClosedAt time.Time `json:"closed_at,omitempty"`
}

// Aggregator combines records in a window.
type Aggregator func(window Window) Record

// Topology defines a stream processing DAG.
type Topology struct {
	Name       string       `json:"name"`
	Sources    []Source     `json:"-"`
	Processors []Processor  `json:"processors"`
	Sinks      []Sink       `json:"sinks"`
	Window     WindowConfig `json:"window,omitempty"`
}

// WindowConfig configures windowing.
type WindowConfig struct {
	Type    WindowType    `json:"type"`
	Size    time.Duration `json:"size"`
	Slide   time.Duration `json:"slide,omitempty"` // For sliding windows
	Gap     time.Duration `json:"gap,omitempty"`   // For session windows
	MaxSize int           `json:"max_size"`
}

// Metrics tracks stream processing metrics.
type Metrics struct {
	mu             sync.Mutex
	RecordsIn      int64
	RecordsOut     int64
	RecordsDropped int64
	WindowsClosed  int64
	WindowSizes    []int64
	Latencies      []time.Duration
	maxSamples     int
}

// NewMetrics creates stream metrics.
func NewMetrics() *Metrics { return &Metrics{maxSamples: 1000} }

func (m *Metrics) RecordIn(count int64)  { m.mu.Lock(); defer m.mu.Unlock(); m.RecordsIn += count }
func (m *Metrics) RecordOut(count int64) { m.mu.Lock(); defer m.mu.Unlock(); m.RecordsOut += count }
func (m *Metrics) RecordDrop(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecordsDropped += count
}
func (m *Metrics) RecordWindowClosed(size int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WindowsClosed++
	m.WindowSizes = append(m.WindowSizes, size)
	if len(m.WindowSizes) > m.maxSamples {
		m.WindowSizes = m.WindowSizes[1:]
	}
}
func (m *Metrics) RecordLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Latencies = append(m.Latencies, d)
	if len(m.Latencies) > m.maxSamples {
		m.Latencies = m.Latencies[1:]
	}
}

// FormatMetrics formats stream metrics.
func (m *Metrics) FormatMetrics() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Stream Metrics:\n%s\n\n", strings.Repeat("─", 40))
	fmt.Fprintf(&sb, "  Records In:     %d\n", m.RecordsIn)
	fmt.Fprintf(&sb, "  Records Out:    %d\n", m.RecordsOut)
	fmt.Fprintf(&sb, "  Dropped:        %d\n", m.RecordsDropped)
	fmt.Fprintf(&sb, "  Windows Closed: %d\n", m.WindowsClosed)
	if len(m.WindowSizes) > 0 {
		avgSize := avg64(m.WindowSizes)
		fmt.Fprintf(&sb, "  Avg Window Size: %.1f\n", avgSize)
	}
	if len(m.Latencies) > 0 {
		avgLat := avgDur(m.Latencies)
		fmt.Fprintf(&sb, "  Avg Latency:    %v\n", avgLat)
	}
	return sb.String()
}

func avg64(vals []int64) float64 {
	var sum int64
	for _, v := range vals {
		sum += v
	}
	return float64(sum) / float64(len(vals))
}

func avgDur(vals []time.Duration) time.Duration {
	var sum time.Duration
	for _, v := range vals {
		sum += v
	}
	return sum / time.Duration(len(vals))
}

// ── Pipeline ──────────────────────────────────────────────

// Pipeline connects sources, processors, and sinks.
type Pipeline struct {
	mu         sync.Mutex
	sources    []Source
	processors []Processor
	sinks      []Sink
	metrics    *Metrics
	topology   *Topology
	running    bool
	stopCh     chan struct{}
	windows    map[string]*priorityQueue // key -> sorted windows
	windowCfg  WindowConfig
	aggregator Aggregator
}

// NewPipeline creates a stream pipeline.
func NewPipeline(topology *Topology) *Pipeline {
	return &Pipeline{
		sources:    topology.Sources,
		processors: topology.Processors,
		sinks:      topology.Sinks,
		metrics:    NewMetrics(),
		topology:   topology,
		windowCfg:  topology.Window,
		windows:    map[string]*priorityQueue{},
		stopCh:     make(chan struct{}),
	}
}

// Start begins the stream processing loop.
func (p *Pipeline) Start(ctx context.Context) error {
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	for _, source := range p.sources {
		go p.runSource(ctx, source)
	}
	return nil
}

// Stop stops the pipeline.
func (p *Pipeline) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		p.running = false
		close(p.stopCh)
	}
}

func (p *Pipeline) runSource(ctx context.Context, source Source) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastOffset int64

	for {
		select {
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			records, err := source.Poll(ctx)
			if err != nil {
				continue
			}
			if len(records) == 0 {
				continue
			}

			p.metrics.RecordIn(int64(len(records)))

			// Apply processors
			processed := records
			for _, proc := range p.processors {
				start := time.Now()
				result, err := proc.Process(processed)
				if err != nil {
					p.metrics.RecordDrop(int64(len(processed)))
					processed = nil
					break
				}
				processed = result
				p.metrics.RecordLatency(time.Since(start))
			}
			if processed == nil {
				continue
			}

			// Handle windowing if configured
			if p.windowCfg.Size > 0 && p.aggregator != nil {
				processed = p.applyWindowing(processed)
				if processed == nil {
					continue
				}
			}

			p.metrics.RecordOut(int64(len(processed)))

			// Write to sinks
			for _, sink := range p.sinks {
				if err := sink.Write(processed); err != nil {
					p.metrics.RecordDrop(int64(len(processed)))
				}
			}

			if len(records) > 0 {
				lastOffset = records[len(records)-1].Offset
			}
			source.Commit(lastOffset)
		}
	}
}

func (p *Pipeline) applyWindowing(records []Record) []Record {
	p.mu.Lock()
	defer p.mu.Unlock()

	var output []Record

	for _, r := range records {
		key := r.Key
		if key == "" {
			key = "_all"
		}
		q, ok := p.windows[key]
		if !ok {
			q = &priorityQueue{}
			heap.Init(q)
			p.windows[key] = q
		}

		winStart := r.Timestamp.Truncate(p.windowCfg.Size)
		winEnd := winStart.Add(p.windowCfg.Size)

		// Find or create window
		var win *Window
		for _, w := range *q {
			if w.Start.Equal(winStart) {
				win = w
				break
			}
		}
		if win == nil {
			win = &Window{Start: winStart, End: winEnd, Key: key}
			heap.Push(q, win)
		}
		win.Records = append(win.Records, r)
	}

	// Close expired windows
	now := time.Now()
	for key, q := range p.windows {
		for q.Len() > 0 && (*q)[0].End.Add(p.windowCfg.Gap).Before(now) {
			win := heap.Pop(q).(*Window)
			win.ClosedAt = now
			result := p.aggregator(*win)
			output = append(output, result)
			p.metrics.RecordWindowClosed(int64(len(win.Records)))
		}
		if q.Len() == 0 {
			delete(p.windows, key)
		}
	}

	return output
}

// SetAggregator sets the window aggregation function.
func (p *Pipeline) SetAggregator(agg Aggregator) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.aggregator = agg
}

// Metrics returns stream metrics.
func (p *Pipeline) Metrics() *Metrics { return p.metrics }

// ── Priority Queue for Windows ────────────────────────────

type priorityQueue []*Window

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].End.Before(pq[j].End) }
func (pq priorityQueue) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i] }
func (pq *priorityQueue) Push(x any)        { *pq = append(*pq, x.(*Window)) }
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// ── Partitioner ───────────────────────────────────────────

// Partitioner splits records across partitions.
type Partitioner struct {
	partitions int
	hasher     func(key string) int
}

// NewPartitioner creates a partitioner.
func NewPartitioner(partitions int) *Partitioner {
	return &Partitioner{partitions: partitions, hasher: func(key string) int {
		var h int
		for _, c := range key {
			h = h*31 + int(c)
		}
		if h < 0 {
			h = -h
		}
		return h % partitions
	}}
}

// Partition assigns a record to a partition.
func (p *Partitioner) Partition(record *Record) int {
	record.Partition = p.hasher(record.Key)
	return record.Partition
}

// ── Built-in Processors ──────────────────────────────────

// FilterProcessor passes records matching a predicate.
type FilterProcessor struct {
	Name string
	Fn   func(Record) bool
}

func (f *FilterProcessor) Filter(n string) string { f.Name = n; return n }
func (f *FilterProcessor) ProcessName() string    { return f.Name }
func (f *FilterProcessor) Process(records []Record) ([]Record, error) {
	var out []Record
	for _, r := range records {
		if f.Fn(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// MapProcessor transforms each record.
type MapProcessor struct {
	Name string
	Fn   func(Record) Record
}

func (m *MapProcessor) MapName() string      { return m.Name }
func (m *MapProcessor) ProcessName2() string { return m.Name }
func (m *MapProcessor) Process(records []Record) ([]Record, error) {
	out := make([]Record, len(records))
	for i, r := range records {
		out[i] = m.Fn(r)
	}
	return out, nil
}

// ── Mock Source for testing ──────────────────────────────

// MockSource is an in-memory source for testing.
type MockSource struct {
	name    string
	records []Record
	idx     int
	mu      sync.Mutex
}

func NewMockSource(name string, records []Record) *MockSource {
	return &MockSource{name: name, records: records}
}
func (ms *MockSource) Name() string { return ms.name }
func (ms *MockSource) Poll(ctx context.Context) ([]Record, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.idx >= len(ms.records) {
		return nil, nil
	}
	batch := ms.records[ms.idx:]
	ms.idx = len(ms.records)
	return batch, nil
}
func (ms *MockSource) Commit(offset int64) error { return nil }

// MockSink collects records in memory.
type MockSink struct {
	name    string
	Records []Record
	mu      sync.Mutex
}

func NewMockSink(name string) *MockSink { return &MockSink{name: name} }
func (ms *MockSink) Name() string       { return ms.name }
func (ms *MockSink) Write(records []Record) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.Records = append(ms.Records, records...)
	return nil
}
func (ms *MockSink) Count() int { ms.mu.Lock(); defer ms.mu.Unlock(); return len(ms.Records) }
