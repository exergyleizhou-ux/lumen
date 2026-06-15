// Package connector provides data source connectors for Lumen agents:
// REST API, gRPC, database, and message queue connections with automatic
// retry, circuit breaking, and health checking.
package connector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Connector is a named data source connection.
type Connector struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"` // rest, grpc, db, mq
	Endpoint string            `json:"endpoint"`
	Config   map[string]string `json:"config,omitempty"`
	Status   string            `json:"status"` // connected, disconnected, error
	LastSeen time.Time         `json:"last_seen"`
}

// HealthStatus is the response from a health check.
type HealthStatus struct {
	Name      string        `json:"name"`
	Healthy   bool          `json:"healthy"`
	Latency   time.Duration `json:"latency"`
	Error     string        `json:"error,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

// CircuitState tracks circuit breaker state.
type CircuitState struct {
	Name     string    `json:"name"`
	Open     bool      `json:"open"`
	Failures int32     `json:"failures"`
	LastFail time.Time `json:"last_fail,omitempty"`
	OpenedAt time.Time `json:"opened_at,omitempty"`
}

// Registry manages all connectors.
type Registry struct {
	mu          sync.RWMutex
	connectors  map[string]*Connector
	circuits    map[string]*CircuitState
	healthFn    func(*Connector) error
	maxFailures int32
	resetAfter  time.Duration
}

// NewRegistry creates a connector registry.
func NewRegistry() *Registry {
	return &Registry{connectors: map[string]*Connector{}, circuits: map[string]*CircuitState{}, maxFailures: 5, resetAfter: 30 * time.Second}
}

// Register adds a connector.
func (r *Registry) Register(c *Connector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c.LastSeen = time.Now()
	c.Status = "connected"
	r.connectors[c.Name] = c
	r.circuits[c.Name] = &CircuitState{Name: c.Name}
}

// Remove deletes a connector.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.connectors, name)
	delete(r.circuits, name)
}

// Get returns a connector by name.
func (r *Registry) Get(name string) (*Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connectors[name]
	return c, ok
}

// SetHealthCheck sets the health check function.
func (r *Registry) SetHealthCheck(fn func(*Connector) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healthFn = fn
}

// HealthCheckAll runs health checks on all connectors.
func (r *Registry) HealthCheckAll() []HealthStatus {
	r.mu.RLock()
	conns := make([]*Connector, 0, len(r.connectors))
	for _, c := range r.connectors {
		conns = append(conns, c)
	}
	fn := r.healthFn
	r.mu.RUnlock()

	var results []HealthStatus
	for _, c := range conns {
		start := time.Now()
		var err error
		if fn != nil {
			err = fn(c)
		}
		hs := HealthStatus{Name: c.Name, Healthy: err == nil, Latency: time.Since(start), CheckedAt: time.Now()}
		if err != nil {
			hs.Error = err.Error()
		}
		r.updateCircuit(c.Name, err != nil)
		results = append(results, hs)
	}
	return results
}

func (r *Registry) updateCircuit(name string, failed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, ok := r.circuits[name]
	if !ok {
		return
	}
	if failed {
		atomic.AddInt32(&cs.Failures, 1)
		cs.LastFail = time.Now()
		if cs.Failures >= r.maxFailures && !cs.Open {
			cs.Open = true
			cs.OpenedAt = time.Now()
		}
	} else {
		atomic.StoreInt32(&cs.Failures, 0)
		cs.Open = false
	}
	if cs.Open && time.Since(cs.OpenedAt) > r.resetAfter {
		cs.Open = false
		atomic.StoreInt32(&cs.Failures, 0)
	}
}

// CircuitBroken reports whether a connector's circuit is open.
func (r *Registry) CircuitBroken(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if cs, ok := r.circuits[name]; ok {
		return cs.Open
	}
	return false
}

// FormatHealth formats health check results.
func FormatHealth(results []HealthStatus) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Connector Health (%d):\n%s\n\n", len(results), strings.Repeat("─", 40))
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	for _, h := range results {
		icon := "✅"
		if !h.Healthy {
			icon = "🔴"
		}
		fmt.Fprintf(&sb, "  %s %-25s %v", icon, h.Name, h.Latency)
		if h.Error != "" {
			fmt.Fprintf(&sb, "  %s", h.Error)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ── REST Connector ────────────────────────────────────────

// RESTConnector wraps HTTP connectivity.
type RESTConnector struct {
	BaseURL string
	client  *http.Client
	headers map[string]string
}

// NewRESTConnector creates a REST connector.
func NewRESTConnector(baseURL string) *RESTConnector {
	return &RESTConnector{BaseURL: baseURL, client: &http.Client{Timeout: 10 * time.Second}, headers: map[string]string{}}
}

// SetHeader adds a default header.
func (rc *RESTConnector) SetHeader(k, v string) { rc.headers[k] = v }

// Get performs a GET request.
func (rc *RESTConnector) Get(path string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", rc.BaseURL+path, nil)
	for k, v := range rc.headers {
		req.Header.Set(k, v)
	}
	return rc.client.Do(req)
}

// Post performs a POST request with JSON body.
func (rc *RESTConnector) Post(path string, body any) (*http.Response, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", rc.BaseURL+path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range rc.headers {
		req.Header.Set(k, v)
	}
	return rc.client.Do(req)
}
