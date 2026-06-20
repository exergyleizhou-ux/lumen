package agent

import (
	"context"
	"testing"
	"time"

	"lumen/internal/provider"
	"lumen/internal/tool"
)

// ttBlockingProvider's Stream never produces output; it waits for the turn
// deadline, then surfaces the context error. Used to prove the configured
// per-turn timeout actually fires (and fast).
type ttBlockingProvider struct{}

func (ttBlockingProvider) Name() string { return "blocking" }
func (ttBlockingProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	go func() {
		<-ctx.Done()
		ch <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
		close(ch)
	}()
	return ch, nil
}

func TestNewDefaultsTurnTimeout(t *testing.T) {
	a := New(ttBlockingProvider{}, tool.NewRegistry(), NewSession(""), Options{})
	if a.turnTimeout != 5*time.Minute {
		t.Errorf("default turnTimeout = %v, want 5m", a.turnTimeout)
	}
	a2 := New(ttBlockingProvider{}, tool.NewRegistry(), NewSession(""), Options{TurnTimeout: 30 * time.Second})
	if a2.turnTimeout != 30*time.Second {
		t.Errorf("explicit turnTimeout = %v, want 30s", a2.turnTimeout)
	}
}

func TestRunHonorsConfiguredTurnTimeout(t *testing.T) {
	a := New(ttBlockingProvider{}, tool.NewRegistry(), NewSession(""), Options{MaxSteps: 3, TurnTimeout: 50 * time.Millisecond})
	start := time.Now()
	err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected an error when the configured turn deadline fires")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("turn timeout not honored: took %v, configured 50ms (would be ~5m with the old hardcode)", elapsed)
	}
}
