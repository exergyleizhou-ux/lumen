// Package retry provides configurable retry strategies for API calls
// and tool executions: exponential backoff with jitter, fixed delay,
// linear backoff, and circuit breaker patterns.
package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Strategy defines how retries are performed.
type Strategy struct {
	MaxRetries   int              // max attempts (0 = no retry)
	InitialDelay time.Duration    // first retry delay
	MaxDelay     time.Duration    // cap on delay
	Multiplier   float64          // backoff multiplier (e.g., 2.0 for exponential)
	Jitter       float64          // random jitter factor (0.0-1.0)
	RetryableFn  func(error) bool // returns true if the error is retryable
}

// DefaultStrategy returns a sensible default retry strategy.
func DefaultStrategy() Strategy {
	return Strategy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
		RetryableFn:  DefaultRetryable,
	}
}

// DefaultRetryable returns true for transient errors (429, 5xx, timeouts).
func DefaultRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, pattern := range []string{"429", "503", "502", "504", "timeout", "connection refused", "EOF", "reset by peer"} {
		if containsStr(msg, pattern) {
			return true
		}
	}
	return false
}

// Do executes fn with retries according to the strategy.
func Do(ctx context.Context, s Strategy, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= s.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt > 0 {
			delay := backoffDuration(s, attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if s.RetryableFn != nil && !s.RetryableFn(err) {
			return err
		}
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", s.MaxRetries+1, lastErr)
}

func backoffDuration(s Strategy, attempt int) time.Duration {
	delay := float64(s.InitialDelay) * math.Pow(s.Multiplier, float64(attempt-1))
	if delay > float64(s.MaxDelay) {
		delay = float64(s.MaxDelay)
	}
	if s.Jitter > 0 {
		delay = delay * (1 + s.Jitter*(rand.Float64()*2-1))
	}
	return time.Duration(delay)
}

// ── Circuit Breaker ──────────────────────────────────────

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // normal operation
	CircuitOpen                         // failing, reject requests
	CircuitHalfOpen                     // testing recovery
)

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	mu               sync.Mutex
	state            CircuitState
	failureCount     int
	successCount     int
	failureThreshold int
	successThreshold int
	openTimeout      time.Duration
	lastFailure      time.Time
	onStateChange    func(from, to CircuitState)
}

// NewCircuitBreaker creates a circuit breaker.
func NewCircuitBreaker(failureThreshold, successThreshold int, openTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		openTimeout:      openTimeout,
	}
}

// Execute runs fn through the circuit breaker.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	switch cb.state {
	case CircuitOpen:
		if time.Since(cb.lastFailure) > cb.openTimeout {
			cb.setState(CircuitHalfOpen)
		} else {
			cb.mu.Unlock()
			return fmt.Errorf("circuit breaker is open")
		}
	case CircuitHalfOpen:
		// allow one probe request
	}
	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		cb.lastFailure = time.Now()
		if cb.failureCount >= cb.failureThreshold {
			cb.setState(CircuitOpen)
		}
		return err
	}

	cb.failureCount = 0
	cb.successCount++
	if cb.state == CircuitHalfOpen && cb.successCount >= cb.successThreshold {
		cb.setState(CircuitClosed)
	}
	return nil
}

func (cb *CircuitBreaker) setState(newState CircuitState) {
	if cb.state == newState {
		return
	}
	old := cb.state
	cb.state = newState
	cb.failureCount = 0
	cb.successCount = 0
	if cb.onStateChange != nil {
		cb.onStateChange(old, newState)
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// OnStateChange registers a callback for state transitions.
func (cb *CircuitBreaker) OnStateChange(fn func(from, to CircuitState)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = fn
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && searchStr(s, sub)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// StateString returns a human-readable state name.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	}
	return "unknown"
}
