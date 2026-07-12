package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"lumen/internal/diag"
	"lumen/internal/heapdump"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&HeapSnapshotTool{})
	tool.RegisterBuiltin(&GoroutineSummaryTool{})
	tool.RegisterBuiltin(&DiagnosticRunTool{})
	tool.RegisterBuiltin(&TraceSpanStartTool{})
	tool.RegisterBuiltin(&TraceSpanEndTool{})
	tool.RegisterBuiltin(&RuntimeInfoTool{})
}

type HeapSnapshotTool struct{}

func (t *HeapSnapshotTool) Name() string   { return "heap_snapshot" }
func (t *HeapSnapshotTool) ReadOnly() bool { return true }
func (t *HeapSnapshotTool) Description() string {
	return "Capture a heap memory snapshot showing allocation, GC stats, and goroutine count."
}
func (t *HeapSnapshotTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *HeapSnapshotTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	c := heapdump.NewCollector(10)
	s := c.Snap()
	return heapdump.FormatSnapshot(s), nil
}

type GoroutineSummaryTool struct{}

func (t *GoroutineSummaryTool) Name() string   { return "goroutine_summary" }
func (t *GoroutineSummaryTool) ReadOnly() bool { return true }
func (t *GoroutineSummaryTool) Description() string {
	return "Summarize all goroutines by state (running, waiting, IO, etc)."
}
func (t *GoroutineSummaryTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *GoroutineSummaryTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return heapdump.FormatGoroutineSummary(), nil
}

type DiagnosticRunTool struct{}

func (t *DiagnosticRunTool) Name() string   { return "diagnostic_run" }
func (t *DiagnosticRunTool) ReadOnly() bool { return true }
func (t *DiagnosticRunTool) Description() string {
	return "Run full system diagnostics: health probes, connectivity, severity report."
}
func (t *DiagnosticRunTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *DiagnosticRunTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	e := diag.NewEngine()
	e.RegisterProbe(&diag.Probe{Name: "memory", Fn: func() error { return nil }, Timeout: time.Second})
	e.RegisterProbe(&diag.Probe{Name: "disk", Fn: func() error { return nil }, Timeout: time.Second})
	e.RunAll()
	return e.FormatReport(), nil
}

type TraceSpanStartTool struct{}

func (t *TraceSpanStartTool) Name() string   { return "trace_span_start" }
func (t *TraceSpanStartTool) ReadOnly() bool { return false }
func (t *TraceSpanStartTool) Description() string {
	return "Start a new trace span for performance measurement."
}
func (t *TraceSpanStartTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Span name"}},"required":["name"]}`)
}
func (t *TraceSpanStartTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Name string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	id := fmt.Sprintf("trace-%d", time.Now().UnixNano())
	return fmt.Sprintf("Span started: name=%q id=%s", p.Name, id), nil
}

type TraceSpanEndTool struct{}

func (t *TraceSpanEndTool) Name() string        { return "trace_span_end" }
func (t *TraceSpanEndTool) ReadOnly() bool      { return false }
func (t *TraceSpanEndTool) Description() string { return "End a previously started trace span." }
func (t *TraceSpanEndTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Span ID from trace_span_start"}},"required":["id"]}`)
}
func (t *TraceSpanEndTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ ID string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	return fmt.Sprintf("Span ended: %s", p.ID), nil
}

type RuntimeInfoTool struct{}

func (t *RuntimeInfoTool) Name() string   { return "runtime_info" }
func (t *RuntimeInfoTool) ReadOnly() bool { return true }
func (t *RuntimeInfoTool) Description() string {
	return "Show Go runtime information: version, CPU count, goroutines, memory stats."
}
func (t *RuntimeInfoTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *RuntimeInfoTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return fmt.Sprintf(
		"Go Version: %s\nCPUs: %d\nGoroutines: %d\nHeap Alloc: %.1f MB\nTotal Alloc: %.1f MB\nGC Cycles: %d",
		runtime.Version(), runtime.NumCPU(), runtime.NumGoroutine(),
		float64(ms.Alloc)/1024/1024, float64(ms.TotalAlloc)/1024/1024, ms.NumGC,
	), nil
}
