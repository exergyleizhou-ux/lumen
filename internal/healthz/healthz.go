// Package healthz provides Kubernetes-style health check endpoints with
// dependency checking: liveness, readiness, and startup probes. Each check
// runs an independent function; failures propagate to the health endpoint.
package healthz

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status is a health check result.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
)

// Check is one health check function.
type Check struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Timeout     time.Duration `json:"timeout"`
	Check       func() error  `json:"-"`
}

// Result is the outcome of one health check.
type Result struct {
	Name      string        `json:"name"`
	Status    Status        `json:"status"`
	Message   string        `json:"message,omitempty"`
	Duration  time.Duration `json:"duration_ms"`
	Timestamp time.Time     `json:"timestamp"`
}

// Handler provides health check endpoints.
type Handler struct {
	mu        sync.RWMutex
	checks    map[string]*Check
	results   map[string]Result
	listeners []func(results []Result)
	liveness  func() error
	readiness func() error
}

// NewHandler creates a health check handler.
func NewHandler() *Handler {
	return &Handler{checks: map[string]*Check{}, results: map[string]Result{}}
}

// AddCheck registers a named health check.
func (h *Handler) AddCheck(name, description string, timeout time.Duration, fn func() error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = &Check{Name: name, Description: description, Timeout: timeout, Check: fn}
}

// SetLiveness sets a custom liveness probe.
func (h *Handler) SetLiveness(fn func() error) { h.liveness = fn }

// SetReadiness sets a custom readiness probe.
func (h *Handler) SetReadiness(fn func() error) { h.readiness = fn }

// RunAll executes all registered checks and stores results.
func (h *Handler) RunAll() []Result {
	h.mu.Lock()
	defer h.mu.Unlock()

	var results []Result
	for name, check := range h.checks {
		start := time.Now()
		err := check.Check()
		result := Result{
			Name: name, Timestamp: time.Now(),
			Duration: time.Since(start),
		}
		if err != nil {
			result.Status = StatusFail
			result.Message = err.Error()
		} else {
			result.Status = StatusPass
		}
		h.results[name] = result
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })

	for _, fn := range h.listeners {
		fn(results)
	}
	return results
}

// OnChange registers a listener for health status changes.
func (h *Handler) OnChange(fn func(results []Result)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listeners = append(h.listeners, fn)
}

// IsHealthy returns true when all checks pass.
func (h *Handler) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, r := range h.results {
		if r.Status == StatusFail {
			return false
		}
	}
	return true
}

// FailedChecks returns the names of failed checks.
func (h *Handler) FailedChecks() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var failed []string
	for _, r := range h.results {
		if r.Status == StatusFail {
			failed = append(failed, r.Name)
		}
	}
	return failed
}

// ServeHTTP implements http.Handler for /healthz and /readyz.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	endpoint := strings.TrimPrefix(r.URL.Path, "/")
	results := h.RunAll()

	healthy := true
	for _, r := range results {
		if r.Status == StatusFail {
			healthy = false
		}
	}

	switch endpoint {
	case "healthz", "livez":
		if healthy {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok\n")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "unhealthy\n")
		}
	case "readyz":
		if healthy {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ready\n")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "not ready\n")
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		formatResultsJSON(w, results)
	}
}

// FormatResults returns a human-readable health summary.
func (h *Handler) FormatResults() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.results) == 0 {
		return "No health checks registered.\n"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Health Check (%d checks):\n\n", len(h.results)))
	keys := make([]string, 0, len(h.results))
	for k := range h.results {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r := h.results[k]
		icon := "✅"
		if r.Status == StatusFail {
			icon = "❌"
		} else if r.Status == StatusWarn {
			icon = "⚠️"
		}
		fmt.Fprintf(&sb, "%s %-20s %s", icon, r.Name, r.Duration)
		if r.Message != "" {
			fmt.Fprintf(&sb, " — %s", r.Message)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func formatResultsJSON(w http.ResponseWriter, results []Result) {
	fmt.Fprint(w, "{\"status\":\"ok\",\"checks\":[")
	for i, r := range results {
		if i > 0 {
			fmt.Fprint(w, ",")
		}
		fmt.Fprintf(w, `{"name":%q,"status":%q,"duration_ms":%d}`, r.Name, r.Status, r.Duration.Milliseconds())
	}
	fmt.Fprint(w, "]}\n")
}

// CheckFunc creates a Check from a simple function.
func CheckFunc(name, description string, fn func() error) *Check {
	return &Check{Name: name, Description: description, Timeout: 5 * time.Second, Check: fn}
}
