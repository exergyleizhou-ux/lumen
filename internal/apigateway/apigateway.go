// Package apigateway provides an HTTP API gateway with routing, rate
// limiting, authentication, request/response transformation, logging,
// and CORS support. It wraps the agent's HTTP serve layer with production-
// grade middleware for secure and observable API access.
package apigateway

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Middleware wraps an http.Handler with additional behavior.
type Middleware func(http.Handler) http.Handler

// Router is an HTTP request router with middleware support.
type Router struct {
	mu         sync.RWMutex
	routes     []*Route
	middleware []Middleware
	notFound   http.Handler
}

// Route is one registered path with handler.
type Route struct {
	Method  string       `json:"method"`
	Path    string       `json:"path"`
	Handler http.Handler `json:"-"`
	Name    string       `json:"name"`
}

// NewRouter creates a router.
func NewRouter() *Router {
	return &Router{notFound: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})}
}

// Use adds global middleware.
func (r *Router) Use(mw Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middleware = append(r.middleware, mw)
}

// Handle registers a route.
func (r *Router) Handle(method, path, name string, handler http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = append(r.routes, &Route{Method: method, Path: path, Handler: handler, Name: name})
}

// HandleFunc registers a handler function.
func (r *Router) HandleFunc(method, path, name string, fn func(http.ResponseWriter, *http.Request)) {
	r.Handle(method, path, name, http.HandlerFunc(fn))
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	handler := r.matchRoute(req.Method, req.URL.Path)
	middleware := make([]Middleware, len(r.middleware))
	copy(middleware, r.middleware)
	r.mu.RUnlock()

	if handler == nil {
		r.notFound.ServeHTTP(w, req)
		return
	}

	// Apply middleware chain
	wrapped := handler
	for i := len(middleware) - 1; i >= 0; i-- {
		wrapped = middleware[i](wrapped)
	}
	wrapped.ServeHTTP(w, req)
}

func (r *Router) matchRoute(method, path string) http.Handler {
	for _, route := range r.routes {
		if method == route.Method && matchPath(route.Path, path) {
			return route.Handler
		}
	}
	// Also try matching with HEAD for GET
	if method == "HEAD" {
		return r.matchRoute("GET", path)
	}
	return nil
}

func matchPath(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if strings.HasSuffix(pattern, "/") && strings.HasPrefix(path, pattern) {
		return true
	}
	return false
}

// ── Middleware ────────────────────────────────────────────

// CORSMiddleware adds CORS headers.
func CORSMiddleware(origins []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(origins) > 0 {
				w.Header().Set("Access-Control-Allow-Origin", strings.Join(origins, ","))
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LoggingMiddleware logs each request.
func LoggingMiddleware(logger func(string, ...any)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			if logger != nil {
				logger("%s %s %v", r.Method, r.URL.Path, time.Since(start))
			}
		})
	}
}

// AuthMiddleware validates a bearer token.
func AuthMiddleware(validateToken func(string) bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") || !validateToken(strings.TrimPrefix(auth, "Bearer ")) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryMiddleware catches panics.
func RecoveryMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					http.Error(w, fmt.Sprintf("internal error: %v", rec), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// TimeoutMiddleware adds a request timeout.
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			done := make(chan struct{})
			go func() { next.ServeHTTP(w, r); close(done) }()
			select {
			case <-done:
			case <-time.After(timeout):
				http.Error(w, "request timeout", http.StatusGatewayTimeout)
			}
		})
	}
}

// ── Rate Limiter ──────────────────────────────────────────

// RateLimiter tracks request rates per key.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64
	burst   int
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter creates a rate limiter.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	return &RateLimiter{buckets: map[string]*tokenBucket{}, rate: rate, burst: burst}
}

// Allow reports whether a request is allowed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: float64(rl.burst), lastTime: time.Now()}
		rl.buckets[key] = b
	}
	now := time.Now()
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastTime = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RateLimitMiddleware creates a rate-limiting middleware.
func RateLimitMiddleware(limiter *RateLimiter, keyFn func(*http.Request) string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if !limiter.Allow(key) {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── Stats ─────────────────────────────────────────────────

// APIMetrics tracks API usage statistics.
type APIMetrics struct {
	mu        sync.Mutex
	counters  map[string]int64
	latencies map[string]time.Duration
	callCount map[string]int
}

// NewAPIMetrics creates API metrics.
func NewAPIMetrics() *APIMetrics {
	return &APIMetrics{counters: map[string]int64{}, latencies: map[string]time.Duration{}, callCount: map[string]int{}}
}

// Record logs an API call.
func (m *APIMetrics) Record(path string, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[path]++
	m.latencies[path] += latency
	m.callCount[path]++
}

// FormatStats formats API statistics.
func (m *APIMetrics) FormatStats() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("API Gateway Stats:\n\n"))
	paths := make([]string, 0, len(m.counters))
	for p := range m.counters {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		count := m.counters[p]
		avg := time.Duration(0)
		if m.callCount[p] > 0 {
			avg = m.latencies[p] / time.Duration(m.callCount[p])
		}
		fmt.Fprintf(&sb, "  %-30s %6d calls  avg:%v\n", p, count, avg)
	}
	return sb.String()
}

// MetricsMiddleware creates a metrics-collecting middleware.
func MetricsMiddleware(metrics *APIMetrics) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			metrics.Record(r.URL.Path, time.Since(start))
		})
	}
}
