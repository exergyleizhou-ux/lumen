// Package gateway implements API gateway extensions: request aggregation (scatter-gather),
// response caching with TTL, request deduplication, and circuit-breaking integration.
package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/circuitbreaker"
)

// CacheEntry holds a cached response with metadata.
type CacheEntry struct {
	Key       string            `json:"key"`
	Response  json.RawMessage   `json:"response"`
	Headers   map[string]string `json:"headers"`
	Status    int               `json:"status"`
	CachedAt  time.Time         `json:"cached_at"`
	ExpiresAt time.Time         `json:"expires_at"`
	Hits      int64             `json:"hits"`
	Size      int               `json:"size"`
}

// IsExpired returns true if the entry has passed its TTL.
func (ce *CacheEntry) IsExpired() bool { return time.Now().After(ce.ExpiresAt) }

// TTL returns the remaining time-to-live.
func (ce *CacheEntry) TTL() time.Duration {
	d := time.Until(ce.ExpiresAt)
	if d < 0 {
		return 0
	}
	return d
}

// ResponseCache is a TTL-based response cache with LRU-like eviction.
type ResponseCache struct {
	mu             sync.RWMutex
	entries        map[string]*CacheEntry
	maxSize        int
	totalHits      int64
	totalMisses    int64
	totalEvictions int64
}

// NewResponseCache creates a cache with the given maximum number of entries.
func NewResponseCache(maxSize int) *ResponseCache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	rc := &ResponseCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
	}
	go rc.evictionLoop()
	return rc
}

func (rc *ResponseCache) evictionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		rc.evictExpired()
	}
}

func (rc *ResponseCache) evictExpired() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for k, v := range rc.entries {
		if v.IsExpired() {
			delete(rc.entries, k)
			atomic.AddInt64(&rc.totalEvictions, 1)
		}
	}
}

// Get retrieves a cached entry. Returns nil if not found or expired.
func (rc *ResponseCache) Get(key string) *CacheEntry {
	rc.mu.RLock()
	entry, ok := rc.entries[key]
	rc.mu.RUnlock()
	if !ok {
		atomic.AddInt64(&rc.totalMisses, 1)
		return nil
	}
	if entry.IsExpired() {
		rc.mu.Lock()
		delete(rc.entries, key)
		rc.mu.Unlock()
		atomic.AddInt64(&rc.totalMisses, 1)
		atomic.AddInt64(&rc.totalEvictions, 1)
		return nil
	}
	atomic.AddInt64(&entry.Hits, 1)
	atomic.AddInt64(&rc.totalHits, 1)
	return entry
}

// Set stores a response in the cache.
func (rc *ResponseCache) Set(key string, response json.RawMessage, headers map[string]string, status int, ttl time.Duration) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Evict oldest if at capacity.
	for len(rc.entries) >= rc.maxSize {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, v := range rc.entries {
			if first || v.CachedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.CachedAt
				first = false
			}
		}
		if oldestKey != "" {
			delete(rc.entries, oldestKey)
			atomic.AddInt64(&rc.totalEvictions, 1)
		}
	}

	now := time.Now()
	rc.entries[key] = &CacheEntry{
		Key:       key,
		Response:  response,
		Headers:   headers,
		Status:    status,
		CachedAt:  now,
		ExpiresAt: now.Add(ttl),
		Size:      len(response),
	}
}

// Invalidate removes a specific key.
func (rc *ResponseCache) Invalidate(key string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	delete(rc.entries, key)
}

// InvalidatePattern removes all keys matching a prefix.
func (rc *ResponseCache) InvalidatePattern(prefix string) int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	count := 0
	for k := range rc.entries {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(rc.entries, k)
			count++
		}
	}
	return count
}

// Stats returns cache statistics.
func (rc *ResponseCache) Stats() map[string]int64 {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return map[string]int64{
		"entries":   int64(len(rc.entries)),
		"hits":      atomic.LoadInt64(&rc.totalHits),
		"misses":    atomic.LoadInt64(&rc.totalMisses),
		"evictions": atomic.LoadInt64(&rc.totalEvictions),
	}
}

// Size returns the current number of entries.
func (rc *ResponseCache) Size() int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return len(rc.entries)
}

// --- Request Deduplication ---

// DedupKey computes a deterministic key for a request.
type DedupKey struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body"`
}

// String returns a hash of the dedup key.
func (dk DedupKey) String() string {
	data, _ := json.Marshal(dk)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:16])
}

// DedupTracker tracks in-flight requests to prevent duplicates.
type DedupTracker struct {
	mu       sync.Mutex
	inFlight map[string]chan struct{} // key -> completion signal
	ttl      time.Duration
}

// NewDedupTracker creates a new deduplication tracker.
func NewDedupTracker(ttl time.Duration) *DedupTracker {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &DedupTracker{
		inFlight: make(map[string]chan struct{}),
		ttl:      ttl,
	}
}

// TryAcquire attempts to register a request. Returns (promise, acquired).
// If acquired is true, caller must call Release when done.
// If acquired is false, caller should wait on the channel for the result.
func (dt *DedupTracker) TryAcquire(key string) (<-chan struct{}, bool) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if ch, ok := dt.inFlight[key]; ok {
		return ch, false
	}
	ch := make(chan struct{})
	dt.inFlight[key] = ch
	return ch, true
}

// Release signals completion and removes the key from tracking.
func (dt *DedupTracker) Release(key string) {
	dt.mu.Lock()
	ch, ok := dt.inFlight[key]
	if ok {
		delete(dt.inFlight, key)
	}
	dt.mu.Unlock()
	if ok {
		close(ch)
	}
}

// Cleanup removes stale entries.
func (dt *DedupTracker) Cleanup() {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	// In a real implementation, we'd track timestamps; simplified here.
	for k, ch := range dt.inFlight {
		select {
		case <-ch:
			delete(dt.inFlight, k)
		default:
		}
	}
}

// --- Scatter-Gather ---

// ScatterRequest represents a single sub-request in a scatter-gather pattern.
type ScatterRequest struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Timeout time.Duration     `json:"timeout"`
}

// ScatterResponse is the response from a single scattered request.
type ScatterResponse struct {
	RequestID string            `json:"request_id"`
	Status    int               `json:"status"`
	Body      json.RawMessage   `json:"body"`
	Headers   map[string]string `json:"headers"`
	Error     string            `json:"error,omitempty"`
	Latency   time.Duration     `json:"latency"`
}

// ScatterGatherResult aggregates all responses.
type ScatterGatherResult struct {
	Responses []ScatterResponse `json:"responses"`
	TotalTime time.Duration     `json:"total_time"`
	Successes int               `json:"successes"`
	Failures  int               `json:"failures"`
	Partial   bool              `json:"partial"`
}

// AggregateFunc is a user-supplied function that performs a single scatter request.
type AggregateFunc func(ctx context.Context, req ScatterRequest) ScatterResponse

// ScatterGather executes multiple requests concurrently and aggregates results.
func ScatterGather(ctx context.Context, requests []ScatterRequest, fn AggregateFunc) *ScatterGatherResult {
	if len(requests) == 0 {
		return &ScatterGatherResult{}
	}

	start := time.Now()
	respCh := make(chan ScatterResponse, len(requests))
	var wg sync.WaitGroup

	for _, req := range requests {
		wg.Add(1)
		go func(r ScatterRequest) {
			defer wg.Done()
			reqCtx := ctx
			if r.Timeout > 0 {
				var cancel context.CancelFunc
				reqCtx, cancel = context.WithTimeout(ctx, r.Timeout)
				defer cancel()
			}
			resp := fn(reqCtx, r)
			resp.RequestID = r.ID
			respCh <- resp
		}(req)
	}

	wg.Wait()
	close(respCh)

	result := &ScatterGatherResult{
		TotalTime: time.Since(start),
	}
	for resp := range respCh {
		result.Responses = append(result.Responses, resp)
		if resp.Error != "" || resp.Status >= 400 {
			result.Failures++
		} else {
			result.Successes++
		}
	}
	result.Partial = result.Failures > 0 && result.Successes > 0
	return result
}

// --- Circuit-breaker Integration ---

// ProtectedCall wraps a function call with circuit breaker protection.
// It checks the breaker before calling, and records success/failure.
func ProtectedCall[T any](breaker *circuitbreaker.CircuitBreaker, fn func() (T, error)) (T, error) {
	if err := breaker.Allow(); err != nil {
		var zero T
		return zero, fmt.Errorf("circuit breaker blocked: %w", err)
	}
	result, err := fn()
	if err != nil {
		breaker.Failure()
		return result, err
	}
	breaker.Success()
	return result, nil
}

// ProtectedCallContext is like ProtectedCall but with context support.
func ProtectedCallContext[T any](ctx context.Context, breaker *circuitbreaker.CircuitBreaker, fn func(context.Context) (T, error)) (T, error) {
	if err := breaker.Allow(); err != nil {
		var zero T
		return zero, fmt.Errorf("circuit breaker blocked: %w", err)
	}
	// Check context before proceeding.
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	default:
	}
	result, err := fn(ctx)
	if err != nil {
		breaker.Failure()
		return result, err
	}
	breaker.Success()
	return result, nil
}

// --- Gateway ---

// Gateway ties together caching, deduplication, and circuit breaking.
type Gateway struct {
	cache    *ResponseCache
	dedup    *DedupTracker
	registry *circuitbreaker.BreakerRegistry
}

// NewGateway creates a new gateway extension bundle.
func NewGateway(cacheSize int, dedupTTL time.Duration, cbCfg circuitbreaker.Config) *Gateway {
	return &Gateway{
		cache:    NewResponseCache(cacheSize),
		dedup:    NewDedupTracker(dedupTTL),
		registry: circuitbreaker.NewRegistry(cbCfg),
	}
}

// Cache returns the response cache.
func (g *Gateway) Cache() *ResponseCache { return g.cache }

// Dedup returns the deduplication tracker.
func (g *Gateway) Dedup() *DedupTracker { return g.dedup }

// Registry returns the circuit breaker registry.
func (g *Gateway) Registry() *circuitbreaker.BreakerRegistry { return g.registry }

// ExecuteRequest is a high-level helper that applies dedup, circuit-breaking, and caching.
func (g *Gateway) ExecuteRequest(ctx context.Context, method, path string, body []byte, ttl time.Duration, fn func(context.Context) (json.RawMessage, error)) (json.RawMessage, bool, error) {
	dk := DedupKey{Method: method, Path: path, Body: string(body)}
	key := dk.String()

	// Check cache.
	if entry := g.cache.Get(key); entry != nil {
		return entry.Response, true, nil
	}

	// Deduplication.
	ch, acquired := g.dedup.TryAcquire(key)
	if !acquired {
		select {
		case <-ch:
			// Retry cache.
			if entry := g.cache.Get(key); entry != nil {
				return entry.Response, true, nil
			}
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
		// Fall through to execute.
		ch2, acquired2 := g.dedup.TryAcquire(key)
		if !acquired2 {
			select {
			case <-ch2:
			case <-ctx.Done():
				return nil, false, ctx.Err()
			}
			if entry := g.cache.Get(key); entry != nil {
				return entry.Response, true, nil
			}
		} else {
			defer g.dedup.Release(key)
		}
	} else {
		defer g.dedup.Release(key)
	}

	// Execute with circuit breaker.
	eb := g.registry.GetOrCreate(circuitbreaker.EndpointKey{
		Service: "gateway",
		Method:  method,
		Path:    path,
	})

	result, err := ProtectedCallContext(ctx, eb.Breaker, fn)
	if err != nil {
		return nil, false, err
	}

	// Cache the result.
	if ttl > 0 {
		g.cache.Set(key, result, nil, 200, ttl)
	}
	return result, false, nil
}

// FormatStatus returns a human-readable status of all gateway components.
func (g *Gateway) FormatStatus() string {
	s := fmt.Sprintf("Gateway Status:\n")
	s += fmt.Sprintf("  Cache: %d entries, hits=%d misses=%d evictions=%d\n",
		g.cache.Size(),
		g.cache.Stats()["hits"],
		g.cache.Stats()["misses"],
		g.cache.Stats()["evictions"],
	)
	s += "  Circuit Breakers:\n"
	s += g.registry.FormatAll()
	return s
}

// --- Rate Limiter ---

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	rate     float64 // tokens per second.
	burst    int     // max tokens.
	tokens   float64
	lastFill time.Time
}

// NewRateLimiter creates a token bucket rate limiter.
func NewRateLimiter(ratePerSec float64, burst int) *RateLimiter {
	return &RateLimiter{
		rate:     ratePerSec,
		burst:    burst,
		tokens:   float64(burst),
		lastFill: time.Now(),
	}
}

// Allow checks if a request is allowed. Returns true if a token is consumed.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(rl.lastFill).Seconds()
	rl.tokens += elapsed * rl.rate
	if rl.tokens > float64(rl.burst) {
		rl.tokens = float64(rl.burst)
	}
	rl.lastFill = now
	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// SetRate updates the rate at runtime.
func (rl *RateLimiter) SetRate(ratePerSec float64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.rate = ratePerSec
}

// --- Per-Key Rate Limiter ---

// KeyRateLimiter maps rate limiters by key (e.g. IP, user ID).
type KeyRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*RateLimiter
	rate     float64
	burst    int
}

// NewKeyRateLimiter creates a per-key rate limiter.
func NewKeyRateLimiter(ratePerSec float64, burst int) *KeyRateLimiter {
	return &KeyRateLimiter{
		limiters: make(map[string]*RateLimiter),
		rate:     ratePerSec,
		burst:    burst,
	}
}

// Allow checks if the given key is allowed.
func (krl *KeyRateLimiter) Allow(key string) bool {
	krl.mu.Lock()
	rl, ok := krl.limiters[key]
	if !ok {
		rl = NewRateLimiter(krl.rate, krl.burst)
		krl.limiters[key] = rl
	}
	krl.mu.Unlock()
	return rl.Allow()
}

// Cleanup removes stale limiters (keys not seen in `age` duration).
func (krl *KeyRateLimiter) Cleanup(age time.Duration) {
	// Simple: just remove all. A production version would track last access.
	krl.mu.Lock()
	defer krl.mu.Unlock()
	krl.limiters = make(map[string]*RateLimiter)
}

// --- Load Balancer ---

// LoadBalancer distributes requests across backends.
type LoadBalancer struct {
	mu       sync.RWMutex
	backends []string
	index    int
	weights  map[string]int
}

// NewLoadBalancer creates a load balancer with round-robin.
func NewLoadBalancer(backends []string) *LoadBalancer {
	return &LoadBalancer{
		backends: backends,
		weights:  make(map[string]int),
	}
}

// Next returns the next backend (round-robin).
func (lb *LoadBalancer) Next() string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if len(lb.backends) == 0 {
		return ""
	}
	backend := lb.backends[lb.index%len(lb.backends)]
	lb.index++
	return backend
}

// AddBackend adds a backend.
func (lb *LoadBalancer) AddBackend(addr string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.backends = append(lb.backends, addr)
}

// RemoveBackend removes a backend.
func (lb *LoadBalancer) RemoveBackend(addr string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for i, b := range lb.backends {
		if b == addr {
			lb.backends = append(lb.backends[:i], lb.backends[i+1:]...)
			return
		}
	}
}

// HealthCheck is a function that checks backend health.
type HealthCheck func(backend string) bool

// HealthyBackends returns only healthy backends.
func (lb *LoadBalancer) HealthyBackends(check HealthCheck) []string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	var out []string
	for _, b := range lb.backends {
		if check(b) {
			out = append(out, b)
		}
	}
	return out
}

// --- Cache Warmup ---

// CacheWarmup pre-populates the cache with common keys.
type CacheWarmup struct {
	cache *ResponseCache
}

// NewCacheWarmup creates a cache warmup helper.
func NewCacheWarmup(cache *ResponseCache) *CacheWarmup {
	return &CacheWarmup{cache: cache}
}

// WarmUp pre-fetches and caches the given keys.
func (cw *CacheWarmup) WarmUp(keys []string, fetcher func(key string) (json.RawMessage, error), ttl time.Duration) {
	for _, key := range keys {
		data, err := fetcher(key)
		if err != nil {
			continue
		}
		cw.cache.Set(key, data, nil, 200, ttl)
	}
}

// --- Stats Collector ---

// GatewayStats collects aggregate statistics.
type GatewayStats struct {
	TotalRequests  int64
	CacheHits      int64
	CacheMisses    int64
	RateLimited    int64
	CircuitOpens   int64
	TotalLatencyMs float64
	mu             sync.Mutex
}

// Record adds a request to stats.
func (gs *GatewayStats) Record(latency time.Duration, cached, rateLimited, circuitOpen bool) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.TotalRequests++
	gs.TotalLatencyMs += float64(latency.Microseconds()) / 1000.0
	if cached {
		gs.CacheHits++
	} else {
		gs.CacheMisses++
	}
	if rateLimited {
		gs.RateLimited++
	}
	if circuitOpen {
		gs.CircuitOpens++
	}
}

// Snapshot returns current stats.
func (gs *GatewayStats) Snapshot() map[string]int64 {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	return map[string]int64{
		"total_requests": gs.TotalRequests,
		"cache_hits":     gs.CacheHits,
		"cache_misses":   gs.CacheMisses,
		"rate_limited":   gs.RateLimited,
		"circuit_opens":  gs.CircuitOpens,
	}
}
