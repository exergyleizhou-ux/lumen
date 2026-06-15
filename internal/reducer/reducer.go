// Package reducer provides map-reduce style data processing with
// pluggable map and reduce functions, combiners, partitioned shuffle,
// and sorted output. Supports in-memory parallel execution.
package reducer

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Record is a key-value input record.
type Record struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// Output is a single reduce output.
type Output struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// Mapper is a map-phase function.
type Mapper interface {
	Map(input Record, emit func(key string, value any))
}

// Reducer is a reduce-phase function.
type Reducer interface {
	Reduce(key string, values []any) any
}

// MapFunc is a functional mapper.
type MapFunc func(input Record, emit func(key string, value any))

// ReduceFunc is a functional reducer.
type ReduceFunc func(key string, values []any) any

type mapAdapter struct{ fn MapFunc }

func (m mapAdapter) Map(input Record, emit func(key string, value any)) { m.fn(input, emit) }

type reduceAdapter struct{ fn ReduceFunc }

func (r reduceAdapter) Reduce(key string, values []any) any { return r.fn(key, values) }

// Job is a map-reduce job specification.
type Job struct {
	Name       string   `json:"name"`
	Input      []Record `json:"-"`
	Mapper     Mapper   `json:"-"`
	Reducer    Reducer  `json:"-"`
	Partitions int      `json:"partitions"`
	Workers    int      `json:"workers"`
}

// JobResult holds the output of a map-reduce job.
type JobResult struct {
	Name        string        `json:"name"`
	Outputs     []Output      `json:"outputs"`
	Duration    time.Duration `json:"duration"`
	MapCount    int           `json:"map_count"`
	ReduceCount int           `json:"reduce_count"`
}

// Engine executes map-reduce jobs.
type Engine struct {
	mu      sync.Mutex
	history []*JobResult
	maxHist int
}

// NewEngine creates a map-reduce engine.
func NewEngine() *Engine {
	return &Engine{maxHist: 100}
}

// Run executes a job.
func (e *Engine) Run(job *Job) *JobResult {
	start := time.Now()
	if job.Partitions <= 0 {
		job.Partitions = 16
	}
	if job.Workers <= 0 {
		job.Workers = 4
	}

	// ── Map Phase ──
	type keyval struct {
		key   string
		value any
	}
	shuffle := make([][]keyval, job.Partitions)

	var mapWg sync.WaitGroup
	sem := make(chan struct{}, job.Workers)
	mapCount := len(job.Input)

	for _, rec := range job.Input {
		sem <- struct{}{}
		mapWg.Add(1)
		go func(r Record) {
			defer mapWg.Done()
			defer func() { <-sem }()
			job.Mapper.Map(r, func(key string, value any) {
				partition := hashKeyPartition(key, job.Partitions)
				shuffle[partition] = append(shuffle[partition], keyval{key, value})
			})
		}(rec)
	}
	mapWg.Wait()

	// ── Shuffle/Sort Phase ──
	type partitionData struct {
		idx     int
		buckets map[string][]any
	}
	partitionCh := make(chan partitionData, job.Partitions)

	for i, data := range shuffle {
		go func(idx int, kvs []keyval) {
			buckets := map[string][]any{}
			for _, kv := range kvs {
				buckets[kv.key] = append(buckets[kv.key], kv.value)
			}
			partitionCh <- partitionData{idx: idx, buckets: buckets}
		}(i, data)
	}

	// ── Reduce Phase ──
	var allOutputs []Output
	var outMu sync.Mutex
	var reduceWg sync.WaitGroup

	for i := 0; i < job.Partitions; i++ {
		pd := <-partitionCh
		reduceWg.Add(1)
		go func(p partitionData) {
			defer reduceWg.Done()
			keys := make([]string, 0, len(p.buckets))
			for k := range p.buckets {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var outputs []Output
			for _, key := range keys {
				result := job.Reducer.Reduce(key, p.buckets[key])
				outputs = append(outputs, Output{Key: key, Value: result})
			}
			outMu.Lock()
			allOutputs = append(allOutputs, outputs...)
			outMu.Unlock()
		}(pd)
	}
	reduceWg.Wait()

	sort.Slice(allOutputs, func(i, j int) bool { return allOutputs[i].Key < allOutputs[j].Key })

	result := &JobResult{
		Name:        job.Name,
		Outputs:     allOutputs,
		Duration:    time.Since(start),
		MapCount:    mapCount,
		ReduceCount: len(allOutputs),
	}

	e.mu.Lock()
	e.history = append(e.history, result)
	if len(e.history) > e.maxHist {
		e.history = e.history[1:]
	}
	e.mu.Unlock()

	return result
}

func hashKeyPartition(key string, partitions int) int {
	var h int
	for _, c := range key {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h % partitions
}

// History returns recent job results.
func (e *Engine) History() []*JobResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*JobResult, len(e.history))
	copy(out, e.history)
	return out
}

// FormatResult formats a job result.
func FormatResult(r *JobResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Map-Reduce: %s (%v)\n%s\n\n", r.Name, r.Duration, strings.Repeat("─", 50))
	fmt.Fprintf(&sb, "  Map records: %d\n", r.MapCount)
	fmt.Fprintf(&sb, "  Reduce outputs: %d\n", r.ReduceCount)
	fmt.Fprintf(&sb, "  Throughput: %.0f rec/s\n", float64(r.MapCount)/r.Duration.Seconds())

	if len(r.Outputs) > 0 {
		limit := r.ReduceCount
		if limit > 30 {
			limit = 30
		}
		sb.WriteString("\n  Outputs:\n")
		for i := 0; i < limit; i++ {
			fmt.Fprintf(&sb, "    %-30s → %v\n", r.Outputs[i].Key, truncateVal(r.Outputs[i].Value, 40))
		}
		if r.ReduceCount > 30 {
			fmt.Fprintf(&sb, "    ... and %d more\n", r.ReduceCount-30)
		}
	}
	return sb.String()
}

func truncateVal(v any, n int) string {
	s := fmt.Sprint(v)
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// ── Built-in Mappers and Reducers ────────────────────────

// WordCountMapper splits a line into word counts.
func WordCountMapper() MapFunc {
	return func(input Record, emit func(key string, value any)) {
		text, _ := input.Value.(string)
		for _, word := range strings.Fields(text) {
			emit(strings.ToLower(strings.Trim(word, ".,!?;:\"'")), 1)
		}
	}
}

// SumReducer sums integer values.
func SumReducer() ReduceFunc {
	return func(key string, values []any) any {
		var sum int
		for _, v := range values {
			switch n := v.(type) {
			case int:
				sum += n
			case float64:
				sum += int(n)
			}
		}
		return sum
	}
}

// IdentityMapper passes through key-value pairs.
func IdentityMapper() MapFunc {
	return func(input Record, emit func(key string, value any)) {
		emit(input.Key, input.Value)
	}
}

// GroupReducer collects values into a slice.
func GroupReducer() ReduceFunc {
	return func(key string, values []any) any {
		return values
	}
}
