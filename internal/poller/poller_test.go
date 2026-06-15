package poller

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestPoller_ChangeDetection(t *testing.T) {
	callCount := 0
	fetchFn := func(ctx context.Context, id, url string) (json.RawMessage, error) {
		callCount++
		if callCount == 1 {
			return json.RawMessage(`{"version":1}`), nil
		}
		return json.RawMessage(`{"version":2}`), nil
	}

	var changes []Change
	var mu sync.Mutex
	notifyFn := func(c Change) {
		mu.Lock()
		changes = append(changes, c)
		mu.Unlock()
	}

	cfg := DefaultConfig()
	cfg.Interval = time.Hour // Don't auto-poll.
	p := New(cfg, fetchFn, notifyFn)
	p.AddResource("res-1", "http://example.com/api")

	// First poll: no change (first fetch).
	p.PollOne("res-1")
	mu.Lock()
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes on first poll, got %d", len(changes))
	}
	mu.Unlock()

	// Second poll: change detected.
	p.PollOne("res-1")
	mu.Lock()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Type != ChangeUpdated {
		t.Fatalf("expected updated change type, got %v", changes[0].Type)
	}
	mu.Unlock()
}

func TestPoller_NoChange(t *testing.T) {
	fetchFn := func(ctx context.Context, id, url string) (json.RawMessage, error) {
		return json.RawMessage(`{"stable":true}`), nil
	}

	var changes []Change
	var mu sync.Mutex
	notifyFn := func(c Change) {
		mu.Lock()
		changes = append(changes, c)
		mu.Unlock()
	}

	cfg := DefaultConfig()
	cfg.Interval = time.Hour
	p := New(cfg, fetchFn, notifyFn)
	p.AddResource("stable", "http://example.com")

	p.PollOne("stable")
	p.PollOne("stable") // Same data, no change.

	mu.Lock()
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes, got %d", len(changes))
	}
	mu.Unlock()
}

func TestPoller_History(t *testing.T) {
	fetchFn := func(ctx context.Context, id, url string) (json.RawMessage, error) {
		return json.RawMessage(`{"x":` + string(rune('0'+time.Now().UnixNano()%10)) + `}`), nil
	}

	cfg := DefaultConfig()
	cfg.Interval = time.Hour
	p := New(cfg, fetchFn, nil)
	p.AddResource("hist", "http://example.com")

	// Force multiple polls to generate history.
	for i := 0; i < 4; i++ {
		p.PollOne("hist")
		time.Sleep(time.Millisecond) // Ensure data changes.
	}

	hist := p.HistoryFor("hist")
	if len(hist) == 0 {
		t.Fatal("expected some history entries")
	}
}

func TestPoller_Stats(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Interval = time.Hour
	p := New(cfg, func(ctx context.Context, id, url string) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}, nil)

	p.AddResource("s1", "http://a")
	p.AddResource("s2", "http://b")

	stats := p.Stats()
	if stats["resources"] != 2 {
		t.Fatalf("expected 2 resources, got %d", stats["resources"])
	}
}

func TestBackoff(t *testing.T) {
	base := time.Second
	max := 10 * time.Second

	d0 := Backoff(base, 0, max, 2.0, 0)
	if d0 != base {
		t.Fatalf("expected base for attempt 0, got %v", d0)
	}

	d1 := Backoff(base, 1, max, 2.0, 0)
	if d1 < base || d1 > 2*base {
		t.Fatalf("expected ~2s for attempt 1, got %v", d1)
	}

	d10 := Backoff(base, 10, max, 2.0, 0)
	if d10 > max {
		t.Fatalf("expected capped at max, got %v", d10)
	}
}

func TestComputeDiff(t *testing.T) {
	oldData := json.RawMessage(`{"a":1,"b":{"x":1}}`)
	newData := json.RawMessage(`{"a":2,"b":{"x":1,"y":2},"c":3}`)

	diffs := computeDiff(oldData, newData)
	if len(diffs) == 0 {
		t.Fatal("expected diffs")
	}

	// Should have diffs for /a (updated), /b/y (created), /c (created).
	paths := make(map[string]ChangeType)
	for _, d := range diffs {
		paths[d.Path] = d.Type
	}
	if paths["/a"] != ChangeUpdated {
		t.Fatalf("expected /a updated, got %v", paths["/a"])
	}
	if paths["/c"] != ChangeCreated {
		t.Fatalf("expected /c created, got %v", paths["/c"])
	}
}

func TestFormatHistory(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Interval = time.Hour
	p := New(cfg, func(ctx context.Context, id, url string) (json.RawMessage, error) {
		return json.RawMessage(`{"v":1}`), nil
	}, nil)
	p.AddResource("fmt-test", "http://x")

	out := p.FormatHistory()
	if out == "" {
		t.Fatal("expected non-empty format")
	}
}
