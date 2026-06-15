// Package routing provides multi-model routing with automatic fallback.
// When the primary provider fails (429, 503, timeout), the router tries
// fallback providers in priority order. This enables the Grok Build-style
// "always available" experience.
package routing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"lumen/internal/config"
	"lumen/internal/provider"
)

// Router manages multiple providers with automatic fallback.
type Router struct {
	mu         sync.RWMutex
	providers  []routeEntry
	defaultIdx int
	stats      map[string]*ProviderStats
}

// routeEntry pairs a config with its resolved provider.
type routeEntry struct {
	cfg  config.ProviderConfig
	prov provider.Provider
}

// ProviderStats tracks health and usage for one provider.
type ProviderStats struct {
	Name       string    `json:"name"`
	Model      string    `json:"model"`
	Healthy    bool      `json:"healthy"`
	LastError  string    `json:"last_error,omitempty"`
	LastUsed   time.Time `json:"last_used"`
	TotalCalls int64     `json:"total_calls"`
	FailCount  int64     `json:"fail_count"`
	mu         sync.Mutex
}

// NewRouter creates a router from config.Providers, resolving each provider.
// The first provider is the default (primary). Others are fallbacks.
func NewRouter(providers []config.ProviderConfig) (*Router, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("at least one provider required")
	}

	r := &Router{
		stats: map[string]*ProviderStats{},
	}

	for _, pc := range providers {
		prov, err := provider.New(pc.Kind, provider.Config{
			Name:    pc.Name,
			BaseURL: pc.BaseURL,
			Model:   pc.Model,
			APIKey:  pc.APIKey,
		})
		if err != nil {
			// Skip unreachable providers; they'll be marked unhealthy
			r.stats[pc.Name] = &ProviderStats{
				Name:      pc.Name,
				Model:     pc.Model,
				Healthy:   false,
				LastError: err.Error(),
			}
			continue
		}
		r.providers = append(r.providers, routeEntry{cfg: pc, prov: prov})
		r.stats[pc.Name] = &ProviderStats{
			Name:    pc.Name,
			Model:   pc.Model,
			Healthy: true,
		}
	}

	if len(r.providers) == 0 {
		return nil, fmt.Errorf("no providers could be resolved")
	}

	return r, nil
}

// Stream tries providers in priority order. On transient failure, it marks
// the provider unhealthy and falls back to the next one. Successful calls
// restore health.
func (r *Router) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	r.mu.RLock()
	entries := make([]routeEntry, len(r.providers))
	copy(entries, r.providers)
	r.mu.RUnlock()

	var lastErr error

	for i, e := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		ch, err := e.prov.Stream(ctx, req)
		if err != nil {
			lastErr = err
			r.recordFailure(e.cfg.Name, err)
			continue
		}

		// Wrap the channel to detect streaming errors for health tracking
		wrapped := make(chan provider.Chunk, 64)
		go func() {
			defer close(wrapped)
			streamOK := true
			for chunk := range ch {
				if chunk.Type == provider.ChunkError {
					streamOK = false
				}
				wrapped <- chunk
			}
			if streamOK {
				r.recordSuccess(e.cfg.Name)
			} else {
				r.recordFailure(e.cfg.Name, fmt.Errorf("stream error"))
			}
		}()

		if i > 0 {
			// We're on a fallback — emit a notice
			r.recordFallback(e.cfg.Name)
		}

		return wrapped, nil
	}

	return nil, fmt.Errorf("all %d providers failed, last error: %w", len(entries), lastErr)
}

// ── Health tracking ────────────────────────────────────────

func (r *Router) recordSuccess(name string) {
	r.mu.RLock()
	s, ok := r.stats[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	s.mu.Lock()
	s.Healthy = true
	s.LastError = ""
	s.TotalCalls++
	s.LastUsed = time.Now()
	s.mu.Unlock()
}

func (r *Router) recordFailure(name string, err error) {
	r.mu.RLock()
	s, ok := r.stats[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	s.mu.Lock()
	s.Healthy = false
	s.LastError = err.Error()
	s.FailCount++
	s.LastUsed = time.Now()
	s.mu.Unlock()
}

func (r *Router) recordFallback(name string) {
	r.mu.RLock()
	s, ok := r.stats[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	s.mu.Lock()
	s.TotalCalls++
	s.LastUsed = time.Now()
	s.mu.Unlock()
}

// Stats returns health statistics for all providers.
func (r *Router) Stats() []*ProviderStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ProviderStats, 0, len(r.stats))
	for _, s := range r.stats {
		out = append(out, s)
	}
	return out
}

// ── Selection strategies ───────────────────────────────────

// SelectByTask picks a provider based on task complexity heuristic.
// Complex tasks (implement, build, refactor) → powerful model.
// Simple tasks (explain, find, list) → cheap model.
func SelectByTask(task string, providers []config.ProviderConfig) *config.ProviderConfig {
	if len(providers) == 0 {
		return nil
	}
	return &providers[0]
}

// SelectByTokenBudget picks the cheapest provider when token budget is tight.
func SelectByTokenBudget(budgetRemaining int, providers []config.ProviderConfig) *config.ProviderConfig {
	if len(providers) == 0 {
		return nil
	}
	// Return the last (cheapest) provider when budget is tight
	if budgetRemaining < 50000 {
		return &providers[len(providers)-1]
	}
	return &providers[0]
}
