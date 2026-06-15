package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"lumen/internal/reducer"
	"lumen/internal/stream"
	"lumen/internal/tool"
)

// ── Adapters for reducer function types ─────────────────────────────────────

// mapFuncAdapter turns a reducer.MapFunc into a reducer.Mapper.
type mapFuncAdapter struct{ fn reducer.MapFunc }

func (a mapFuncAdapter) Map(input reducer.Record, emit func(key string, value any)) {
	a.fn(input, emit)
}

// reduceFuncAdapter turns a reducer.ReduceFunc into a reducer.Reducer.
type reduceFuncAdapter struct{ fn reducer.ReduceFunc }

func (a reduceFuncAdapter) Reduce(key string, values []any) any {
	return a.fn(key, values)
}

func init() {
	tool.RegisterBuiltin(&RunMapReduceTool{})
	tool.RegisterBuiltin(&StreamMetricsTool{})
}

// ── Shared state ────────────────────────────────────────────────────────────

var (
	reducerEngine  *reducer.Engine
	reducerOnce    sync.Once
	streamPipeline *stream.Pipeline
	streamOnce     sync.Once
	streamMetrics  *stream.Metrics
)

func getReducerEngine() *reducer.Engine {
	reducerOnce.Do(func() {
		reducerEngine = reducer.NewEngine()
	})
	return reducerEngine
}

func getStreamPipeline() *stream.Pipeline {
	streamOnce.Do(func() {
		streamMetrics = stream.NewMetrics()
		topology := &stream.Topology{
			Name:       "default",
			Processors: nil,
			Sinks:      nil,
		}
		streamPipeline = stream.NewPipeline(topology)
	})
	return streamPipeline
}

// ── run_mapreduce ───────────────────────────────────────────────────────────

type RunMapReduceTool struct{}

func (t *RunMapReduceTool) Name() string   { return "run_mapreduce" }
func (t *RunMapReduceTool) ReadOnly() bool { return false }

func (t *RunMapReduceTool) Description() string {
	return "Run a word-count map-reduce job on the provided text input. Each line becomes a record; outputs word frequencies sorted by key."
}

func (t *RunMapReduceTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "input":{"type":"string","description":"Text to process; each line is treated as a separate record"},
  "workers":{"type":"integer","description":"Number of parallel workers (default 4)"}
},
"required":["input"]
}`)
}

func (t *RunMapReduceTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Input   string `json:"input"`
		Workers int    `json:"workers"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Input == "" {
		return "", fmt.Errorf("input is required")
	}

	// Split input into lines as records
	lines := strings.Split(p.Input, "\n")
	var records []reducer.Record
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		records = append(records, reducer.Record{
			Key:   fmt.Sprintf("line-%d", i),
			Value: line,
		})
	}

	if len(records) == 0 {
		return "No non-empty lines to process.", nil
	}

	job := &reducer.Job{
		Name:    "word-count",
		Input:   records,
		Mapper:  mapFuncAdapter{fn: reducer.WordCountMapper()},
		Reducer: reduceFuncAdapter{fn: reducer.SumReducer()},
	}
	if p.Workers > 0 {
		job.Workers = p.Workers
	}

	eng := getReducerEngine()
	result := eng.Run(job)
	return reducer.FormatResult(result), nil
}

// ── stream_metrics ──────────────────────────────────────────────────────────

type StreamMetricsTool struct{}

func (t *StreamMetricsTool) Name() string   { return "stream_metrics" }
func (t *StreamMetricsTool) ReadOnly() bool { return true }

func (t *StreamMetricsTool) Description() string {
	return "Return stream processing metrics: records in/out, dropped counts, windows closed, average window size, and average latency."
}

func (t *StreamMetricsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *StreamMetricsTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	// Get metrics from the stream pipeline
	pipeline := getStreamPipeline()
	m := pipeline.Metrics()

	// Also feed a few records to make metrics non-trivial if they are zero
	// but we don't force it — just return current state
	formatted := m.FormatMetrics()

	// Add pipeline topology info
	extra := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
	}
	extraJSON, _ := json.MarshalIndent(extra, "", "  ")

	return formatted + "\n" + string(extraJSON), nil
}
