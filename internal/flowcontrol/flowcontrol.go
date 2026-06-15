// Package flowcontrol provides rate limiting, concurrency control,
// backpressure management, and admission control for agent request
// processing pipelines.
package flowcontrol

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Limiter is a rate limiter interface.
type Limiter interface {
	Allow() bool
	AllowN(n int) bool
	Reset()
}

// FixedWindow limits requests within fixed time windows.
type FixedWindow struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	counter     int
	windowStart time.Time
}

// NewFixedWindow creates a fixed-window rate limiter.
func NewFixedWindow(limit int, window time.Duration) *FixedWindow {
	return &FixedWindow{limit: limit, window: window, windowStart: time.Now()}
}

func (fw *FixedWindow) Allow() bool { return fw.AllowN(1) }
func (fw *FixedWindow) AllowN(n int) bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if time.Since(fw.windowStart) > fw.window {
		fw.counter = 0
		fw.windowStart = time.Now()
	}
	if fw.counter+n <= fw.limit {
		fw.counter += n
		return true
	}
	return false
}
func (fw *FixedWindow) Reset() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.counter = 0
	fw.windowStart = time.Now()
}

// SlidingWindow limits requests in a sliding time window.
type SlidingWindow struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	timestamps []time.Time
}

// NewSlidingWindow creates a sliding-window rate limiter.
func NewSlidingWindow(limit int, window time.Duration) *SlidingWindow {
	return &SlidingWindow{limit: limit, window: window}
}

func (sw *SlidingWindow) Allow() bool { return sw.AllowN(1) }
func (sw *SlidingWindow) AllowN(n int) bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-sw.window)
	var valid []time.Time
	for _, t := range sw.timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	sw.timestamps = valid
	if len(sw.timestamps)+n <= sw.limit {
		for i := 0; i < n; i++ {
			sw.timestamps = append(sw.timestamps, now)
		}
		return true
	}
	return false
}
func (sw *SlidingWindow) Reset() { sw.mu.Lock(); defer sw.mu.Unlock(); sw.timestamps = nil }

// Semaphore is a concurrency limiter.
type Semaphore struct {
	ch chan struct{}
}

// NewSemaphore creates a semaphore with max concurrency.
func NewSemaphore(max int) *Semaphore { return &Semaphore{ch: make(chan struct{}, max)} }

// Acquire blocks until a slot is available.
func (s *Semaphore) Acquire() { s.ch <- struct{}{} }

// Release frees a slot.
func (s *Semaphore) Release() { <-s.ch }

// TryAcquire attempts non-blocking acquisition.
func (s *Semaphore) TryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

// Available returns current free slots.
func (s *Semaphore) Available() int { return cap(s.ch) - len(s.ch) }

// AdmissionController decides whether to admit a request.
type AdmissionController struct {
	mu       sync.Mutex
	limiters []Limiter
	priority map[string]int
}

// NewAdmissionController creates an admission controller.
func NewAdmissionController() *AdmissionController {
	return &AdmissionController{priority: map[string]int{}}
}

// AddLimiter adds a rate limiter check.
func (ac *AdmissionController) AddLimiter(l Limiter) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.limiters = append(ac.limiters, l)
}

// SetPriority sets the priority for a request class.
func (ac *AdmissionController) SetPriority(class string, pri int) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.priority[class] = pri
}

// Admit checks whether a request should be processed.
func (ac *AdmissionController) Admit(class string) bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	for _, l := range ac.limiters {
		if !l.Allow() {
			return false
		}
	}
	_ = class
	return true
}

// ── Backpressure ──────────────────────────────────────────

// Backpressure tracks system load for backpressure signaling.
type Backpressure struct {
	mu         sync.Mutex
	queueDepth int
	maxDepth   int
	loadAvg    float64
	throttle   bool
}

// NewBackpressure creates a backpressure controller.
func NewBackpressure(maxDepth int) *Backpressure {
	return &Backpressure{maxDepth: maxDepth}
}

// SetDepth updates the current queue depth.
func (bp *Backpressure) SetDepth(depth int) bool {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.queueDepth = depth
	bp.loadAvg = float64(depth) / float64(bp.maxDepth)
	bp.throttle = depth > bp.maxDepth
	return bp.throttle
}

// Throttled reports whether backpressure is active.
func (bp *Backpressure) Throttled() bool { bp.mu.Lock(); defer bp.mu.Unlock(); return bp.throttle }

// Load returns the current load ratio (0-1).
func (bp *Backpressure) Load() float64 { bp.mu.Lock(); defer bp.mu.Unlock(); return bp.loadAvg }

// ── Governor ───────────────────────────────────────────────

// Governor combines rate limiting, concurrency, and backpressure.
type Governor struct {
	mu           sync.Mutex
	limiter      Limiter
	semaphore    *Semaphore
	backpressure *Backpressure
}

// NewGovernor creates a governor.
func NewGovernor(rateLimit int, window time.Duration, maxConcurrency, maxDepth int) *Governor {
	return &Governor{
		limiter:      NewSlidingWindow(rateLimit, window),
		semaphore:    NewSemaphore(maxConcurrency),
		backpressure: NewBackpressure(maxDepth),
	}
}

// TryProcess attempts to admit and process a request.
func (g *Governor) TryProcess(fn func()) bool {
	if g.backpressure.Throttled() {
		return false
	}
	if !g.limiter.Allow() {
		return false
	}
	if !g.semaphore.TryAcquire() {
		return false
	}
	go func() { defer g.semaphore.Release(); fn() }()
	return true
}

// Stats returns governor statistics.
func (g *Governor) Stats() map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return map[string]any{
		"concurrency_available": g.semaphore.Available(),
		"backpressure_active":   g.backpressure.Throttled(),
		"load":                  g.backpressure.Load(),
	}
}

// FormatStats formats governor statistics.
func FormatStats(stats map[string]any) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Flow Control Stats:\n%s\n\n", strings.Repeat("─", 40))
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "  %-25s %v\n", k, stats[k])
	}
	return sb.String()
}
