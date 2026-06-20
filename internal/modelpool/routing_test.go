package modelpool

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/provider"
)

// fakeProvider is a provider.Provider stub driven by a script of chunks.
type fakeProvider struct {
	name    string
	calls   int64
	emit    func(ch chan<- provider.Chunk) // what to stream
	setuErr error                          // if set, Stream() returns this immediately
	delay   time.Duration
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	atomic.AddInt64(&f.calls, 1)
	if f.setuErr != nil {
		return nil, f.setuErr
	}
	ch := make(chan provider.Chunk, 8)
	go func() {
		defer close(ch)
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		f.emit(ch)
	}()
	return ch, nil
}

func okStream(text string) func(chan<- provider.Chunk) {
	return func(ch chan<- provider.Chunk) {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{CompletionTokens: 3}}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}
}

func errBeforeOutput() func(chan<- provider.Chunk) {
	return func(ch chan<- provider.Chunk) {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: errors.New("backend down")}
	}
}

func errAfterOutput() func(chan<- provider.Chunk) {
	return func(ch chan<- provider.Chunk) {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "partial "}
		ch <- provider.Chunk{Type: provider.ChunkError, Err: errors.New("cut mid-stream")}
	}
}

func drain(t *testing.T, rp *RoutingProvider, req provider.Request) (text string, lastErr error, gotDone bool) {
	t.Helper()
	ch, err := rp.Stream(context.Background(), req)
	if err != nil {
		return "", err, false
	}
	for c := range ch {
		switch c.Type {
		case provider.ChunkText:
			text += c.Text
		case provider.ChunkError:
			lastErr = c.Err
		case provider.ChunkDone:
			gotDone = true
		}
	}
	return text, lastErr, gotDone
}

func TestRoutingFailsOverWhenSetupFails(t *testing.T) {
	cloud := &fakeProvider{name: "cloud", emit: okStream("from cloud")}
	local := &fakeProvider{name: "local", setuErr: errors.New("connection refused")}
	rp := NewRoutingProvider([]Backend{
		{Name: "local", Provider: local, IsLocal: true},
		{Name: "cloud", Provider: cloud, IsLocal: false},
	})
	text, lastErr, done := drain(t, rp, provider.Request{})
	if lastErr != nil {
		t.Fatalf("unexpected error after failover: %v", lastErr)
	}
	if !done || text != "from cloud" {
		t.Errorf("got text=%q done=%v, want failover to cloud", text, done)
	}
	if atomic.LoadInt64(&cloud.calls) != 1 {
		t.Errorf("cloud called %d times, want 1", cloud.calls)
	}
}

func TestRoutingFailsOverOnErrorBeforeOutput(t *testing.T) {
	local := &fakeProvider{name: "local", emit: errBeforeOutput()}
	cloud := &fakeProvider{name: "cloud", emit: okStream("cloud answer")}
	rp := NewRoutingProvider([]Backend{
		{Name: "local", Provider: local, IsLocal: true},
		{Name: "cloud", Provider: cloud, IsLocal: false},
	})
	text, lastErr, done := drain(t, rp, provider.Request{})
	if lastErr != nil || !done || text != "cloud answer" {
		t.Errorf("got text=%q err=%v done=%v, want clean failover to cloud", text, lastErr, done)
	}
}

func TestRoutingDoesNotReplayAfterOutput(t *testing.T) {
	// Once a backend has produced output, a mid-stream error must NOT trigger
	// failover (that would replay the whole prompt). The partial output + error
	// are surfaced; the second backend is never called.
	local := &fakeProvider{name: "local", emit: errAfterOutput()}
	cloud := &fakeProvider{name: "cloud", emit: okStream("should not be used")}
	rp := NewRoutingProvider([]Backend{
		{Name: "local", Provider: local, IsLocal: true},
		{Name: "cloud", Provider: cloud, IsLocal: false},
	})
	text, lastErr, _ := drain(t, rp, provider.Request{})
	if text != "partial " {
		t.Errorf("text = %q, want the partial output preserved", text)
	}
	if lastErr == nil {
		t.Error("expected the mid-stream error to be surfaced, not swallowed")
	}
	if atomic.LoadInt64(&cloud.calls) != 0 {
		t.Errorf("cloud called %d times, want 0 (no replay after output)", cloud.calls)
	}
}

func TestRoutingPrefersLocalFirst(t *testing.T) {
	local := &fakeProvider{name: "local", emit: okStream("local answer")}
	cloud := &fakeProvider{name: "cloud", emit: okStream("cloud answer")}
	rp := NewRoutingProvider([]Backend{
		{Name: "cloud", Provider: cloud, IsLocal: false},
		{Name: "local", Provider: local, IsLocal: true},
	})
	text, _, _ := drain(t, rp, provider.Request{})
	if text != "local answer" {
		t.Errorf("text = %q, want local preferred", text)
	}
	if atomic.LoadInt64(&cloud.calls) != 0 {
		t.Errorf("cloud called %d times, want 0 (local healthy)", cloud.calls)
	}
}

func TestRoutingAllBackendsFail(t *testing.T) {
	a := &fakeProvider{name: "a", emit: errBeforeOutput()}
	b := &fakeProvider{name: "b", setuErr: errors.New("nope")}
	rp := NewRoutingProvider([]Backend{
		{Name: "a", Provider: a, IsLocal: true},
		{Name: "b", Provider: b, IsLocal: false},
	})
	_, lastErr, done := drain(t, rp, provider.Request{})
	if lastErr == nil || done {
		t.Errorf("want a terminal error when all backends fail; got err=%v done=%v", lastErr, done)
	}
}
