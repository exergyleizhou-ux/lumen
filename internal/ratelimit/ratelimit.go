// Package ratelimit provides token-bucket rate limiting for API calls
// and tool executions. It supports per-provider limits, burst control,
// and automatic backoff when limits are exceeded.
package ratelimit

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Bucket is a token-bucket rate limiter.
type Bucket struct {
	mu       sync.Mutex
	rate     float64 // tokens per second
	burst    int     // max tokens (burst capacity)
	tokens   float64
	lastTime time.Time
	waitCount int64
	allowCount int64
	denyCount  int64
}

// NewBucket creates a token bucket with the given rate and burst.
func NewBucket(ratePerSec float64, burst int) *Bucket {
	return &Bucket{
		rate:     ratePerSec,
		burst:    burst,
		tokens:   float64(burst),
		lastTime: time.Now(),
	}
}

// Allow reports whether one token can be consumed without waiting.
func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens >= 1 {
		b.tokens--
		b.allowCount++
		return true
	}
	b.denyCount++
	return false
}

// Wait blocks until a token is available or ctx is cancelled.
func (b *Bucket) Wait(ctx context.Context) error {
	for {
		if b.Allow() {
			return nil
		}
		b.mu.Lock()
		waitTime := time.Duration(float64(time.Second) / b.rate)
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}
}

func (b *Bucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > float64(b.burst) {
		b.tokens = float64(b.burst)
	}
	b.lastTime = now
}

// Stats returns usage statistics.
func (b *Bucket) Stats() (allowed, denied int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.allowCount, b.denyCount
}

// SetRate changes the token rate dynamically.
func (b *Bucket) SetRate(ratePerSec float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rate = ratePerSec
}

// ── Multilimiter ──────────────────────────────────────────

// Limiter manages multiple rate limit buckets for different keys.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*Bucket
	defaultRate float64
	defaultBurst int
}

// NewLimiter creates a multi-key rate limiter.
func NewLimiter(defaultRate float64, defaultBurst int) *Limiter {
	return &Limiter{
		buckets:    make(map[string]*Bucket),
		defaultRate: defaultRate,
		defaultBurst: defaultBurst,
	}
}

// Allow checks if a request for the given key is allowed.
func (l *Limiter) Allow(key string) bool {
	b := l.getBucket(key)
	return b.Allow()
}

// Wait blocks until a request for key is allowed.
func (l *Limiter) Wait(ctx context.Context, key string) error {
	b := l.getBucket(key)
	return b.Wait(ctx)
}

func (l *Limiter) getBucket(key string) *Bucket {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok := l.buckets[key]; ok {
		return b
	}
	b := NewBucket(l.defaultRate, l.defaultBurst)
	l.buckets[key] = b
	return b
}

// Stats returns stats for a specific key.
func (l *Limiter) Stats(key string) (allowed, denied int64) {
	l.mu.Lock()
	b, ok := l.buckets[key]
	l.mu.Unlock()
	if !ok {
		return 0, 0
	}
	return b.Stats()
}

// FormatStats formats limiter stats for all keys.
func (l *Limiter) FormatStats() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Rate limiter (%d buckets):\n", len(l.buckets)))
	for k, b := range l.buckets {
		a, d := b.Stats()
		total := a + d
		rate := float64(0)
		if total > 0 {
			rate = float64(a) / float64(total) * 100
		}
		fmt.Fprintf(&sb, "  %s: %.0f%% allowed (%d/%d)\n", k, rate, a, total)
	}
	return sb.String()
}

