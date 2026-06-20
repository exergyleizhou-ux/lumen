package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"lumen/internal/event"
	"lumen/internal/provider"
)

// perfUsageProvider streams a little text, then a usage chunk, then done — and
// inserts a small delay before the first chunk so TTFT is measurably positive.
type perfUsageProvider struct{}

func (perfUsageProvider) Name() string { return "perf" }
func (perfUsageProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 4)
	go func() {
		defer close(ch)
		time.Sleep(10 * time.Millisecond) // measurable TTFT
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens: 30, CompletionTokens: 12, TotalTokens: 42,
		}}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func TestAgentEmitsPerfEvent(t *testing.T) {
	a := New(perfUsageProvider{}, testRegistry(), NewSession(""), Options{MaxSteps: 1})

	var mu sync.Mutex
	var perfs []event.Perf
	a.SetSink(event.FuncSink(func(e event.Event) {
		if e.Kind == event.PerfKind && e.Perf != nil {
			mu.Lock()
			perfs = append(perfs, *e.Perf)
			mu.Unlock()
		}
	}))

	if err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(perfs) == 0 {
		t.Fatal("no perf event emitted")
	}
	p := perfs[0]
	if p.CompletionTokens != 12 {
		t.Errorf("CompletionTokens = %d, want 12 (real usage)", p.CompletionTokens)
	}
	if p.TurnMs <= 0 {
		t.Errorf("TurnMs = %d, want > 0", p.TurnMs)
	}
	if p.TTFTMs <= 0 {
		t.Errorf("TTFTMs = %d, want > 0 (provider delayed the first chunk)", p.TTFTMs)
	}
}
