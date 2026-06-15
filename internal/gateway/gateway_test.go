package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"lumen/internal/circuitbreaker"
)

func TestResponseCache_SetAndGet(t *testing.T) {
	cache := NewResponseCache(10)

	key := "test-key"
	payload := json.RawMessage(`{"result":"ok"}`)

	cache.Set(key, payload, nil, 200, 5*time.Second)

	entry := cache.Get(key)
	if entry == nil {
		t.Fatal("expected cache hit")
	}
	if entry.Status != 200 {
		t.Fatalf("expected status 200, got %d", entry.Status)
	}
	if string(entry.Response) != `{"result":"ok"}` {
		t.Fatalf("unexpected response: %s", entry.Response)
	}
}

func TestResponseCache_Expiry(t *testing.T) {
	cache := NewResponseCache(10)

	cache.Set("exp", json.RawMessage(`"x"`), nil, 200, 10*time.Millisecond)

	// Should be available.
	if cache.Get("exp") == nil {
		t.Fatal("expected hit before expiry")
	}

	time.Sleep(20 * time.Millisecond)

	if cache.Get("exp") != nil {
		t.Fatal("expected nil after expiry")
	}
}

func TestResponseCache_Eviction(t *testing.T) {
	cache := NewResponseCache(3)

	for i := 0; i < 5; i++ {
		cache.Set(fmt.Sprintf("k%d", i), json.RawMessage(`"v"`), nil, 200, time.Hour)
	}

	if cache.Size() > 3 {
		t.Fatalf("expected at most 3 entries, got %d", cache.Size())
	}
}

func TestDedupTracker(t *testing.T) {
	dt := NewDedupTracker(time.Second)

	ch1, acquired1 := dt.TryAcquire("req-1")
	if !acquired1 {
		t.Fatal("expected to acquire new key")
	}

	ch2, acquired2 := dt.TryAcquire("req-1")
	if acquired2 {
		t.Fatal("expected duplicate to not be acquired")
	}
	if ch1 != ch2 {
		t.Fatal("expected same channel for duplicate")
	}

	dt.Release("req-1")

	// After release, should be able to acquire again.
	_, acquired3 := dt.TryAcquire("req-1")
	if !acquired3 {
		t.Fatal("expected to acquire after release")
	}
}

func TestProtectedCall(t *testing.T) {
	cfg := circuitbreaker.DefaultConfig()
	cb := circuitbreaker.New("protected-test", cfg)

	result, err := ProtectedCall(cb, func() (string, error) {
		return "hello", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Fatalf("expected 'hello', got %q", result)
	}
}

func TestScatterGather(t *testing.T) {
	requests := []ScatterRequest{
		{ID: "r1", Method: "GET", URL: "/api/1", Timeout: time.Second},
		{ID: "r2", Method: "GET", URL: "/api/2", Timeout: time.Second},
		{ID: "r3", Method: "GET", URL: "/api/3", Timeout: time.Second},
	}

	result := ScatterGather(context.Background(), requests, func(ctx context.Context, req ScatterRequest) ScatterResponse {
		return ScatterResponse{
			RequestID: req.ID,
			Status:    200,
			Body:      json.RawMessage(`{"id":"` + req.ID + `"}`),
		}
	})

	if len(result.Responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(result.Responses))
	}
	if result.Successes != 3 {
		t.Fatalf("expected 3 successes, got %d", result.Successes)
	}
	if result.Partial {
		t.Fatal("expected not partial")
	}
}

func TestGateway_ExecuteRequest(t *testing.T) {
	cfg := circuitbreaker.DefaultConfig()
	gw := NewGateway(100, time.Minute, cfg)

	ctx := context.Background()
	callCount := 0

	for i := 0; i < 3; i++ {
		resp, cached, err := gw.ExecuteRequest(ctx, "GET", "/test", nil, time.Second, func(ctx context.Context) (json.RawMessage, error) {
			callCount++
			return json.RawMessage(`{"call":` + fmt.Sprintf("%d", callCount) + `}`), nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if i == 0 {
			if cached {
				t.Fatal("first call should not be cached")
			}
		} else {
			if !cached {
				t.Fatal("subsequent calls should be cached")
			}
		}
		_ = resp
	}
	if callCount != 1 {
		t.Fatalf("expected 1 actual call, got %d", callCount)
	}
}
