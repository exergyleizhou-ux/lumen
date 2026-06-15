package circuitbreaker

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailureThreshold = 3
	cfg.Timeout = 100 * time.Millisecond
	cb := New("test-closed-to-open", cfg)

	if cb.State() != StateClosed {
		t.Fatal("expected closed state")
	}

	// Should allow requests.
	if err := cb.Allow(); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
	cb.Success()

	// Trip the breaker.
	for i := 0; i < 3; i++ {
		if err := cb.Allow(); err != nil {
			t.Fatalf("expected allow, got %v (i=%d)", err, i)
		}
		cb.Failure()
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected open state, got %v", cb.State())
	}

	// Should reject in open state.
	if err := cb.Allow(); err == nil {
		t.Fatal("expected rejection in open state")
	}
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailureThreshold = 2
	cfg.SuccessThreshold = 2
	cfg.Timeout = 50 * time.Millisecond
	cfg.HalfOpenMaxReqs = 3
	cb := New("test-halfopen", cfg)

	// Trip to open.
	for i := 0; i < 2; i++ {
		cb.Allow()
		cb.Failure()
	}
	if cb.State() != StateOpen {
		t.Fatalf("expected open, got %v", cb.State())
	}

	// Wait for timeout.
	time.Sleep(100 * time.Millisecond)

	// Now should transition to half-open.
	if err := cb.Allow(); err != nil {
		t.Fatalf("expected allow in half-open, got %v", err)
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected half-open, got %v", cb.State())
	}

	// Succeed enough to close.
	cb.Success()
	cb.Allow()
	cb.Success()
	if cb.State() != StateClosed {
		t.Fatalf("expected closed after successes, got %v", cb.State())
	}
}

func TestCircuitBreaker_FailureRate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailureRateLimit = 0.3
	cfg.RequestVolumeMin = 5
	cfg.FailureThreshold = 100 // High threshold so rate is used.
	cfg.WindowDuration = 500 * time.Millisecond
	cb := New("test-rate", cfg)

	// Send 4 failures out of 6 = 66% > 30%.
	for i := 0; i < 6; i++ {
		cb.Allow()
		if i < 4 {
			cb.Failure()
		} else {
			cb.Success()
		}
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected open due to failure rate, got %v", cb.State())
	}
}

func TestBreakerRegistry(t *testing.T) {
	reg := NewRegistry(DefaultConfig())

	key1 := EndpointKey{Service: "api", Method: "GET", Path: "/users"}
	key2 := EndpointKey{Service: "api", Method: "POST", Path: "/users"}

	eb1 := reg.GetOrCreate(key1)
	eb2 := reg.GetOrCreate(key2)
	eb1Dup := reg.GetOrCreate(key1)

	if eb1 != eb1Dup {
		t.Fatal("expected same breaker for same key")
	}
	if eb1 == eb2 {
		t.Fatal("expected different breakers for different keys")
	}

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 breakers, got %d", len(list))
	}

	reg.Remove(key1)
	if reg.Get(key1) != nil {
		t.Fatal("expected nil after remove")
	}
}

func TestCircuitBreaker_FormatStatus(t *testing.T) {
	cb := New("status-test", DefaultConfig())
	cb.Allow()
	cb.Failure()
	status := cb.FormatStatus()
	if status == "" {
		t.Fatal("expected non-empty status")
	}
	if status[:7] != "[status" {
		t.Fatalf("unexpected status format: %s", status)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailureThreshold = 2
	cb := New("reset-test", cfg)

	for i := 0; i < 2; i++ {
		cb.Allow()
		cb.Failure()
	}
	if cb.State() != StateOpen {
		t.Fatal("expected open")
	}

	cb.Reset()
	if cb.State() != StateClosed {
		t.Fatalf("expected closed after reset, got %v", cb.State())
	}
	m := cb.Metrics()
	if m.TotalFailures != 0 {
		t.Fatalf("expected 0 failures after reset, got %d", m.TotalFailures)
	}
}

func TestCircuitBreaker_Concurrency(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FailureThreshold = 50
	cb := New("concurrent", cfg)

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := cb.Allow(); err == nil {
					cb.Success()
				}
			}
		}()
	}
	wg.Wait()

	m := cb.Metrics()
	if m.TotalRequests != 1000 {
		t.Fatalf("expected 1000 requests, got %d", m.TotalRequests)
	}
}
