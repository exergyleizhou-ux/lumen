// Package heapdump provides heap analysis, memory profiling snapshots,
// and allocation tracking. It captures goroutine stacks, allocation
// hotspots, and memory trends for diagnosing agent memory issues.
package heapdump

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"
)

// Snapshot represents a heap memory snapshot.
type Snapshot struct {
	Timestamp time.Time `json:"timestamp"`
	Alloc     uint64    `json:"alloc_bytes"`
	TotalAlloc uint64   `json:"total_alloc_bytes"`
	Sys       uint64    `json:"sys_bytes"`
	NumGC     uint32    `json:"num_gc"`
	HeapObjects uint64  `json:"heap_objects"`
	Goroutines int      `json:"goroutines"`
	StackTraces []StackFrame `json:"stack_traces,omitempty"`
}

// StackFrame is one frame in a goroutine stack.
type StackFrame struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// Collector captures heap snapshots.
type Collector struct {
	mu        sync.Mutex
	snapshots []*Snapshot
	maxSnaps  int
	tmpDir    string
}

// NewCollector creates a heap dump collector.
func NewCollector(maxSnapshots int) *Collector {
	return &Collector{maxSnaps: maxSnapshots, tmpDir: os.TempDir()}
}

// Snap captures a heap snapshot.
func (c *Collector) Snap() *Snapshot {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	s := &Snapshot{
		Timestamp:   time.Now(),
		Alloc:       ms.Alloc,
		TotalAlloc:  ms.TotalAlloc,
		Sys:         ms.Sys,
		NumGC:       ms.NumGC,
		HeapObjects: ms.HeapObjects,
		Goroutines:  runtime.NumGoroutine(),
	}

	c.mu.Lock()
	c.snapshots = append(c.snapshots, s)
	if len(c.snapshots) > c.maxSnaps { c.snapshots = c.snapshots[1:] }
	c.mu.Unlock()
	return s
}

// WriteProfile writes a pprof heap profile to a file.
func (c *Collector) WriteProfile(filename string) error {
	f, err := os.Create(filename)
	if err != nil { return err }
	defer f.Close()
	return pprof.WriteHeapProfile(f)
}

// Delta computes the delta between two snapshots.
func Delta(before, after *Snapshot) map[string]int64 {
	return map[string]int64{
		"alloc_delta":      int64(after.Alloc) - int64(before.Alloc),
		"total_alloc_delta": int64(after.TotalAlloc) - int64(before.TotalAlloc),
		"goroutines_delta": int64(after.Goroutines) - int64(before.Goroutines),
		"heap_objects_delta": int64(after.HeapObjects) - int64(before.HeapObjects),
		"gc_count_delta":   int64(after.NumGC) - int64(before.NumGC),
	}
}

// Latest returns the most recent snapshot.
func (c *Collector) Latest() *Snapshot {
	c.mu.Lock(); defer c.mu.Unlock()
	if len(c.snapshots) == 0 { return nil }
	return c.snapshots[len(c.snapshots)-1]
}

// Trend returns alloc values over time.
func (c *Collector) Trend() []uint64 {
	c.mu.Lock(); defer c.mu.Unlock()
	var out []uint64
	for _, s := range c.snapshots { out = append(out, s.Alloc) }
	return out
}

// FormatSnapshot formats a single snapshot.
func FormatSnapshot(s *Snapshot) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Heap Snapshot: %s\n%s\n\n", s.Timestamp.Format(time.RFC3339), strings.Repeat("─", 50))
	fmt.Fprintf(&sb, "  Alloc:       %s\n", byteCount(s.Alloc))
	fmt.Fprintf(&sb, "  TotalAlloc:  %s\n", byteCount(s.TotalAlloc))
	fmt.Fprintf(&sb, "  Sys:         %s\n", byteCount(s.Sys))
	fmt.Fprintf(&sb, "  HeapObjects: %d\n", s.HeapObjects)
	fmt.Fprintf(&sb, "  Goroutines:  %d\n", s.Goroutines)
	fmt.Fprintf(&sb, "  GC Cycles:   %d\n", s.NumGC)
	return sb.String()
}

func byteCount(n uint64) string {
	switch {
	case n < 1024: return fmt.Sprintf("%dB", n)
	case n < 1024*1024: return fmt.Sprintf("%.1fKB", float64(n)/1024)
	case n < 1024*1024*1024: return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	default: return fmt.Sprintf("%.1fGB", float64(n)/1024/1024/1024)
	}
}

// FormatDelta formats a delta report.
func FormatDelta(delta map[string]int64) string {
	var sb strings.Builder
	keys := make([]string, 0, len(delta))
	for k := range delta { keys = append(keys, k) }
	sort.Strings(keys)
	fmt.Fprintf(&sb, "Heap Delta:\n%s\n\n", strings.Repeat("─", 30))
	for _, k := range keys {
		v := delta[k]
		icon := ""
		if v > 0 { icon = "📈" } else if v < 0 { icon = "📉" }
		fmt.Fprintf(&sb, "  %s %s: %+d\n", icon, k, v)
	}
	return sb.String()
}

// ── Allocation Tracker ────────────────────────────────────

// AllocationRecord tracks one allocation event.
type AllocationRecord struct {
	Size    uint64    `json:"size"`
	Count   int       `json:"count"`
	File    string    `json:"file"`
	Line    int       `json:"line"`
	Time    time.Time `json:"time"`
}

// AllocationTracker tracks allocation patterns.
type AllocationTracker struct {
	mu      sync.Mutex
	records []AllocationRecord
	maxRecs int
}

// NewAllocationTracker creates an allocation tracker.
func NewAllocationTracker(maxRecords int) *AllocationTracker {
	return &AllocationTracker{maxRecs: maxRecords}
}

// Record logs an allocation event.
func (at *AllocationTracker) Record(size uint64, file string, line int) {
	at.mu.Lock(); defer at.mu.Unlock()
	at.records = append(at.records, AllocationRecord{
		Size: size, Count: 1, File: file, Line: line, Time: time.Now(),
	})
	if len(at.records) > at.maxRecs { at.records = at.records[1:] }
}

// ByFile aggregates allocations by file.
func (at *AllocationTracker) ByFile() map[string]uint64 {
	at.mu.Lock(); defer at.mu.Unlock()
	agg := map[string]uint64{}
	for _, r := range at.records { agg[r.File] += r.Size }
	return agg
}

// TopFiles returns the top N allocating files.
func (at *AllocationTracker) TopFiles(n int) []struct {
	File string
	Size uint64
} {
	byFile := at.ByFile()
	type kv struct{ f string; s uint64 }
	var pairs []kv
	for f, s := range byFile { pairs = append(pairs, kv{f, s}) }
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].s > pairs[j].s })
	var out []struct{ File string; Size uint64 }
	for i := 0; i < n && i < len(pairs); i++ {
		out = append(out, struct{ File string; Size uint64 }{pairs[i].f, pairs[i].s})
	}
	return out
}

// FormatAllocations formats the top allocation sources.
func (at *AllocationTracker) FormatAllocations(topN int) string {
	var sb strings.Builder
	top := at.TopFiles(topN)
	fmt.Fprintf(&sb, "Top Allocators (%d):\n%s\n\n", len(top), strings.Repeat("─", 40))
	for _, t := range top {
		fmt.Fprintf(&sb, "  %-40s %s\n", t.File, byteCount(t.Size))
	}
	return sb.String()
}

// ── Goroutine Dumper ──────────────────────────────────────

// GoroutineDumper captures goroutine stack traces.
type GoroutineDumper struct {
	out io.Writer
}

// NewGoroutineDumper creates a goroutine dumper.
func NewGoroutineDumper(w io.Writer) *GoroutineDumper {
	return &GoroutineDumper{out: w}
}

// Dump writes all goroutine stacks.
func (gd *GoroutineDumper) Dump() error {
	buf := make([]byte, 64*1024)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) { _, err := gd.out.Write(buf[:n]); return err }
		buf = make([]byte, 2*len(buf))
	}
}

// DumpProfile writes goroutine profile.
func (gd *GoroutineDumper) DumpProfile(filename string) error {
	f, err := os.Create(filename)
	if err != nil { return err }
	defer f.Close()
	return pprof.Lookup("goroutine").WriteTo(f, 0)
}

// CountByState returns goroutine counts by state prefix.
func CountByState() map[string]int {
	buf := make([]byte, 128*1024)
	n := runtime.Stack(buf, true)
	lines := strings.Split(string(buf[:n]), "\n")
	counts := map[string]int{}
	for _, line := range lines {
		if strings.Contains(line, "goroutine ") {
			state := extractState(line)
			counts[state]++
		}
	}
	return counts
}

func extractState(line string) string {
	// goroutine 123 [running]:
	if idx := strings.Index(line, "["); idx >= 0 {
		if end := strings.Index(line[idx:], "]"); end >= 0 {
			return line[idx+1 : idx+end]
		}
	}
	if idx := strings.Index(line, ", "); idx >= 0 {
		// goroutine 123, running:
		rest := line[idx+2:]
		if end := strings.Index(rest, ","); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
		return strings.TrimSpace(rest)
	}
	return "unknown"
}

// FormatGoroutineSummary formats goroutine state summary.
func FormatGoroutineSummary() string {
	counts := CountByState()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Goroutine Summary (%d total):\n%s\n\n", runtime.NumGoroutine(), strings.Repeat("─", 40))
	keys := make([]string, 0, len(counts))
	for k := range counts { keys = append(keys, k) }
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "  %-20s %d\n", k, counts[k])
	}
	return sb.String()
}
