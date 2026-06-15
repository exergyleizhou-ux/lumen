// Package ratelimit provides advanced rate limiting algorithms:
// token bucket, leaky bucket, and hierarchical rate limiting with
// parent-child budget allocation.
package ratelimit

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type TokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func NewTokenBucket(rate, burst float64) *TokenBucket {
	return &TokenBucket{rate: rate, burst: burst, tokens: burst, last: time.Now()}
}
func (tb *TokenBucket) Allow() bool { return tb.AllowN(1) }
func (tb *TokenBucket) AllowN(n float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.last).Seconds()
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.burst {
		tb.tokens = tb.burst
	}
	tb.last = now
	if tb.tokens >= n {
		tb.tokens -= n
		return true
	}
	return false
}
func (tb *TokenBucket) Tokens() float64      { tb.mu.Lock(); defer tb.mu.Unlock(); return tb.tokens }
func (tb *TokenBucket) SetRate(rate float64) { tb.mu.Lock(); defer tb.mu.Unlock(); tb.rate = rate }

type LeakyBucket struct {
	mu       sync.Mutex
	capacity float64
	leakRate float64
	water    float64
	last     time.Time
}

func NewLeakyBucket(capacity, leakRate float64) *LeakyBucket {
	return &LeakyBucket{capacity: capacity, leakRate: leakRate, last: time.Now()}
}
func (lb *LeakyBucket) Add(n float64) bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(lb.last).Seconds()
	lb.water -= elapsed * lb.leakRate
	if lb.water < 0 {
		lb.water = 0
	}
	lb.last = now
	if lb.water+n > lb.capacity {
		return false
	}
	lb.water += n
	return true
}
func (lb *LeakyBucket) Level() float64 { lb.mu.Lock(); defer lb.mu.Unlock(); return lb.water }

type HierarchicalLimiter struct {
	mu       sync.Mutex
	name     string
	parent   *HierarchicalLimiter
	children map[string]*HierarchicalLimiter
	bucket   *TokenBucket
	limit    float64
}

func NewHierarchicalLimiter(name string, rate float64) *HierarchicalLimiter {
	return &HierarchicalLimiter{name: name, bucket: NewTokenBucket(rate, rate), children: map[string]*HierarchicalLimiter{}, limit: rate}
}
func (hl *HierarchicalLimiter) AddChild(name string, rate float64) *HierarchicalLimiter {
	c := NewHierarchicalLimiter(name, rate)
	c.parent = hl
	hl.mu.Lock()
	defer hl.mu.Unlock()
	hl.children[name] = c
	return c
}
func (hl *HierarchicalLimiter) Allow() bool {
	hl.mu.Lock()
	if !hl.bucket.Allow() {
		hl.mu.Unlock()
		return false
	}
	hl.mu.Unlock()
	if hl.parent != nil && !hl.parent.Allow() {
		return false
	}
	return true
}
func (hl *HierarchicalLimiter) SetLimit(rate float64) {
	hl.mu.Lock()
	defer hl.mu.Unlock()
	hl.limit = rate
	hl.bucket.SetRate(rate)
}
func (hl *HierarchicalLimiter) FormatTree() string {
	hl.mu.Lock()
	defer hl.mu.Unlock()
	var sb strings.Builder
	hl.formatTreeRec(&sb, 0)
	return sb.String()
}
func (hl *HierarchicalLimiter) formatTreeRec(sb *strings.Builder, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(sb, "%s%s (%.1f/s)\n", indent, hl.name, hl.limit)
	for _, c := range hl.children {
		c.mu.Lock()
		c.formatTreeRec(sb, depth+1)
		c.mu.Unlock()
	}
}
