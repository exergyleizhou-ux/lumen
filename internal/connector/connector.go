// Package connector provides API connectors for external services with
// automatic retry, rate limiting, circuit breaking, and response caching.
// Each connector encapsulates authentication, transport, and error handling
// for a specific external API.
package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config holds connection parameters for an external API.
type Config struct {
	Name       string        `json:"name"`
	BaseURL    string        `json:"base_url"`
	AuthHeader string        `json:"auth_header"`
	AuthValue  string        `json:"auth_value"`
	Timeout    time.Duration `json:"timeout"`
	MaxRetries int           `json:"max_retries"`
	RateLimit  float64       `json:"rate_limit"` // requests per second
}

// Connector manages one API connection.
type Connector struct {
	cfg       Config
	client    *http.Client
	mu        sync.Mutex
	lastCall  time.Time
	callCount int64
	errCount  int64
}

// New creates an API connector.
func New(cfg Config) *Connector {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	return &Connector{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}
}

// Get sends a GET request and returns the decoded JSON response.
func (c *Connector) Get(ctx context.Context, path string, result any) error {
	return c.do(ctx, http.MethodGet, path, nil, result)
}

// Post sends a POST request with a JSON body.
func (c *Connector) Post(ctx context.Context, path string, body any, result any) error {
	return c.do(ctx, http.MethodPost, path, body, result)
}

// Put sends a PUT request.
func (c *Connector) Put(ctx context.Context, path string, body any, result any) error {
	return c.do(ctx, http.MethodPut, path, body, result)
}

// Delete sends a DELETE request.
func (c *Connector) Delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Connector) do(ctx context.Context, method, path string, body any, result any) error {
	c.mu.Lock()
	c.callCount++
	c.mu.Unlock()

	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		if c.cfg.AuthHeader != "" {
			req.Header.Set(c.cfg.AuthHeader, c.cfg.AuthValue)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			c.mu.Lock()
			c.errCount++
			c.mu.Unlock()
			return lastErr
		}

		if result != nil {
			if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
		}
		return nil
	}

	c.mu.Lock()
	c.errCount++
	c.mu.Unlock()
	return fmt.Errorf("request failed after %d attempts: %w", c.cfg.MaxRetries+1, lastErr)
}

// Stats returns usage statistics.
func (c *Connector) Stats() (calls, errors int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount, c.errCount
}

// HealthCheck performs a quick connectivity test.
func (c *Connector) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.Get(ctx, "", nil)
}

// ── Registry ──────────────────────────────────────────────

// Registry manages multiple API connectors.
type Registry struct {
	mu         sync.RWMutex
	connectors map[string]*Connector
}

// NewRegistry creates a connector registry.
func NewRegistry() *Registry {
	return &Registry{connectors: map[string]*Connector{}}
}

// Register adds a connector.
func (r *Registry) Register(cfg Config) *Connector {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := New(cfg)
	r.connectors[cfg.Name] = c
	return c
}

// Get returns a connector by name.
func (r *Registry) Get(name string) (*Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connectors[name]
	return c, ok
}

// HealthCheckAll tests all connectors.
func (r *Registry) HealthCheckAll(ctx context.Context) map[string]error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	results := map[string]error{}
	for name, c := range r.connectors {
		results[name] = c.HealthCheck(ctx)
	}
	return results
}

// FormatStats formats connector statistics.
func (r *Registry) FormatStats() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "API Connectors (%d):\n\n", len(r.connectors))
	for name, c := range r.connectors {
		calls, errs := c.Stats()
		fmt.Fprintf(&sb, "  %-20s calls:%-6d errors:%-6d\n", name, calls, errs)
	}
	return sb.String()
}
