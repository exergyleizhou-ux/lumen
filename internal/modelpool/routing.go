package modelpool

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"lumen/internal/provider"
)

// Backend is one routable model endpoint behind a RoutingProvider.
type Backend struct {
	Name     string
	Provider provider.Provider
	IsLocal  bool
}

// RoutingProvider implements provider.Provider over an ordered set of backends
// with latency-aware, local-first routing and real failover. It is the bridge
// that connects the modelpool's health/latency bookkeeping (Pool) to the actual
// provider stack the agent uses.
//
// Failover is at the stream layer and does NOT replay produced output: a backend
// is only abandoned if it fails to set up or errors BEFORE emitting any content.
// Once a backend has streamed any text/tool call, a later cut is surfaced as-is
// (the agent's own stream-recovery handles mid-stream interruptions) — re-running
// a different backend there would replay the whole prompt, which we deliberately
// avoid.
type RoutingProvider struct {
	backends []Backend
	pool     *Pool
	mu       sync.Mutex
}

// NewRoutingProvider builds a router over the given backends. Local backends are
// preferred (cheap + fast); cloud backends act as failover for hard tasks or
// when local is unhealthy.
func NewRoutingProvider(backends []Backend) *RoutingProvider {
	pool := NewPool(&LatencyOptimizedStrategy{})
	for _, b := range backends {
		caps := []Capability{CapStreaming, CapToolUse}
		if b.IsLocal {
			caps = append(caps, CapCheap)
		} else {
			caps = append(caps, CapPowerful)
		}
		pool.Register(&Info{Name: b.Name, Capabilities: caps})
	}
	return &RoutingProvider{backends: backends, pool: pool}
}

func (r *RoutingProvider) Name() string {
	if len(r.backends) > 0 {
		return r.backends[0].Name
	}
	return "router"
}

// order returns the backends to try, healthy first, local preferred, then by
// lowest observed average latency.
func (r *RoutingProvider) order() []Backend {
	r.pool.mu.RLock()
	type ranked struct {
		b       Backend
		healthy bool
		avg     time.Duration
	}
	rs := make([]ranked, 0, len(r.backends))
	for _, b := range r.backends {
		info := r.pool.models[b.Name]
		st := r.pool.stats[b.Name]
		healthy := info == nil || info.Healthy
		var avg time.Duration
		if st != nil && st.TotalCalls > 0 {
			avg = st.TotalLatency / time.Duration(st.TotalCalls)
		}
		rs = append(rs, ranked{b: b, healthy: healthy, avg: avg})
	}
	r.pool.mu.RUnlock()

	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].healthy != rs[j].healthy {
			return rs[i].healthy // healthy first
		}
		if rs[i].b.IsLocal != rs[j].b.IsLocal {
			return rs[i].b.IsLocal // local preferred
		}
		// Among same locality, prefer the one with lower observed latency.
		// Unmeasured (avg==0) sorts first so a fresh backend gets a chance.
		if (rs[i].avg == 0) != (rs[j].avg == 0) {
			return rs[i].avg == 0
		}
		return rs[i].avg < rs[j].avg
	})

	out := make([]Backend, len(rs))
	for i, x := range rs {
		out[i] = x.b
	}
	return out
}

// Stream routes the request, failing over across backends until one produces
// output (or all are exhausted).
func (r *RoutingProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	out := make(chan provider.Chunk, 64)
	go func() {
		defer close(out)
		ordered := r.order()
		var lastErr error
		for _, b := range ordered {
			start := time.Now()
			ch, err := b.Provider.Stream(ctx, req)
			if err != nil {
				// Setup failure (e.g. local endpoint down) → fail over.
				r.pool.RecordFailure(b.Name, err.Error())
				lastErr = err
				continue
			}

			produced := false
			failed := false
			for chunk := range ch {
				switch chunk.Type {
				case provider.ChunkText, provider.ChunkReasoning,
					provider.ChunkToolCall, provider.ChunkToolCallStart:
					produced = true
					out <- chunk
				case provider.ChunkError:
					if !produced {
						// No output yet → safe to fail over to the next backend
						// without replaying anything.
						r.pool.RecordFailure(b.Name, errString(chunk.Err))
						lastErr = chunk.Err
						failed = true
					} else {
						// Output already streamed; do NOT replay on another
						// backend. Surface the error and let the agent's
						// stream-recovery decide.
						out <- chunk
					}
				default:
					out <- chunk
				}
				if failed {
					break
				}
			}

			if failed {
				continue // try the next backend
			}
			// Backend completed (with or without a post-output error) — record
			// its latency and stop. We don't re-route after first output.
			r.pool.RecordSuccess(b.Name, 0, time.Since(start))
			return
		}

		// All backends exhausted.
		if lastErr == nil {
			lastErr = fmt.Errorf("no routable backend available")
		}
		out <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("all backends failed: %w", lastErr)}
	}()
	return out, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
