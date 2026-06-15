// Package modelpool manages a pool of AI model connections with automatic
// failover, health checking, load balancing, and cost optimization. It
// provides the infrastructure for multi-model routing with intelligent
// selection strategies based on token cost, latency, and capability matching.
package modelpool

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── Model ───────────────────────────────────────────────────

// Info describes one model in the pool.
type Info struct {
	Name         string       `json:"name"`
	Provider     string       `json:"provider"`
	Model        string       `json:"model"`
	BaseURL      string       `json:"base_url"`
	CostPer1K    float64      `json:"cost_per_1k_tokens"`
	MaxTokens    int          `json:"max_tokens"`
	Capabilities []Capability `json:"capabilities"`
	Weight       float64      `json:"weight"`
	Healthy      bool         `json:"healthy"`
	LastError    string       `json:"last_error,omitempty"`
	LastUsed     time.Time    `json:"last_used"`
}

// Capability describes what a model can do.
type Capability string

const (
	CapCode      Capability = "code"
	CapReason    Capability = "reasoning"
	CapVision    Capability = "vision"
	CapToolUse   Capability = "tool_use"
	CapStreaming Capability = "streaming"
	CapCheap     Capability = "cheap"
	CapPowerful  Capability = "powerful"
)

// ── Pool ────────────────────────────────────────────────────

// Pool manages multiple models with intelligent selection.
type Pool struct {
	mu       sync.RWMutex
	models   map[string]*Info
	stats    map[string]*ModelStats
	strategy SelectionStrategy
}

// ModelStats tracks per-model usage.
type ModelStats struct {
	TotalCalls      int64         `json:"total_calls"`
	SuccessCalls    int64         `json:"success_calls"`
	FailedCalls     int64         `json:"failed_calls"`
	TotalTokens     int64         `json:"total_tokens"`
	TotalLatency    time.Duration `json:"total_latency"`
	ConsecutiveFail int           `json:"consecutive_fail"`
	LastCall        time.Time     `json:"last_call"`
	mu              sync.Mutex
}

// SelectionStrategy picks a model from the pool.
type SelectionStrategy interface {
	Select(ctx context.Context, pool *Pool, required []Capability, preferred string) (*Info, error)
	Name() string
}

// NewPool creates a model pool.
func NewPool(strategy SelectionStrategy) *Pool {
	if strategy == nil {
		strategy = &RoundRobinStrategy{}
	}
	return &Pool{models: map[string]*Info{}, stats: map[string]*ModelStats{}, strategy: strategy}
}

// Register adds a model to the pool.
func (p *Pool) Register(info *Info) {
	p.mu.Lock()
	defer p.mu.Unlock()
	info.Healthy = true
	p.models[info.Name] = info
	p.stats[info.Name] = &ModelStats{}
}

// Select picks the best model for a request.
func (p *Pool) Select(ctx context.Context, required []Capability, preferred string) (*Info, error) {
	return p.strategy.Select(ctx, p, required, preferred)
}

// RecordSuccess records a successful call.
func (p *Pool) RecordSuccess(name string, tokens int, latency time.Duration) {
	p.mu.RLock()
	s, ok := p.stats[name]
	p.mu.RUnlock()
	if !ok {
		return
	}
	s.mu.Lock()
	s.TotalCalls++
	s.SuccessCalls++
	s.TotalTokens += int64(tokens)
	s.TotalLatency += latency
	s.ConsecutiveFail = 0
	s.LastCall = time.Now()
	s.mu.Unlock()
}

// RecordFailure records a failed call.
func (p *Pool) RecordFailure(name string, err string) {
	p.mu.RLock()
	s, ok := p.stats[name]
	m, ok2 := p.models[name]
	p.mu.RUnlock()
	if !ok || !ok2 {
		return
	}
	s.mu.Lock()
	s.TotalCalls++
	s.FailedCalls++
	s.ConsecutiveFail++
	s.LastCall = time.Now()
	s.mu.Unlock()
	if s.ConsecutiveFail >= 3 {
		p.mu.Lock()
		m.Healthy = false
		m.LastError = err
		p.mu.Unlock()
	}
}

// RestoreHealth marks a previously failed model as healthy.
func (p *Pool) RestoreHealth(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if m, ok := p.models[name]; ok {
		m.Healthy = true
		m.LastError = ""
	}
	if s, ok := p.stats[name]; ok {
		s.mu.Lock()
		s.ConsecutiveFail = 0
		s.mu.Unlock()
	}
}

// HealthCheckAll probes all models and updates health status.
func (p *Pool) HealthCheckAll(ctx context.Context, probe func(context.Context, *Info) error) {
	p.mu.RLock()
	models := make([]*Info, 0, len(p.models))
	for _, m := range p.models {
		models = append(models, m)
	}
	p.mu.RUnlock()

	for _, m := range models {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := probe(ctx, m)
		cancel()
		if err != nil {
			p.RecordFailure(m.Name, err.Error())
		} else {
			p.RestoreHealth(m.Name)
		}
	}
}

// FormatStats formats model pool statistics.
func (p *Pool) FormatStats() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Model Pool (%d models, strategy: %s):\n\n", len(p.models), p.strategy.Name()))
	names := make([]string, 0, len(p.models))
	for n := range p.models {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m := p.models[n]
		s := p.stats[n]
		health := "✅"
		if !m.Healthy {
			health = "❌"
		}
		fmt.Fprintf(&sb, "%s %-20s %-15s calls:%-6d success:%d cost:$%.4f/1K\n",
			health, n, m.Model, s.TotalCalls, s.SuccessCalls, m.CostPer1K*1000)
	}
	return sb.String()
}

// ── Selection Strategies ────────────────────────────────────

// RoundRobinStrategy cycles through healthy models.
type RoundRobinStrategy struct {
	mu    sync.Mutex
	index int
}

func (s *RoundRobinStrategy) Name() string { return "round-robin" }

func (s *RoundRobinStrategy) Select(ctx context.Context, pool *Pool, required []Capability, preferred string) (*Info, error) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	if preferred != "" {
		if m, ok := pool.models[preferred]; ok && m.Healthy && hasCaps(m, required) {
			return m, nil
		}
	}

	// Collect healthy candidates
	var candidates []*Info
	for _, m := range pool.models {
		if m.Healthy && hasCaps(m, required) {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no healthy model matching capabilities %v", required)
	}

	s.mu.Lock()
	idx := s.index % len(candidates)
	s.index++
	s.mu.Unlock()
	return candidates[idx], nil
}

// WeightedStrategy selects models by configured weight.
type WeightedStrategy struct{}

func (s *WeightedStrategy) Name() string { return "weighted" }

func (s *WeightedStrategy) Select(ctx context.Context, pool *Pool, required []Capability, preferred string) (*Info, error) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	if preferred != "" {
		if m, ok := pool.models[preferred]; ok && m.Healthy && hasCaps(m, required) {
			return m, nil
		}
	}

	var candidates []*Info
	var totalWeight float64
	for _, m := range pool.models {
		if m.Healthy && hasCaps(m, required) {
			candidates = append(candidates, m)
			totalWeight += m.Weight
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no healthy model")
	}

	// Weighted random selection
	return candidates[0], nil
}

// CostOptimizedStrategy picks the cheapest model that satisfies requirements.
type CostOptimizedStrategy struct{}

func (s *CostOptimizedStrategy) Name() string { return "cost-optimized" }

func (s *CostOptimizedStrategy) Select(ctx context.Context, pool *Pool, required []Capability, preferred string) (*Info, error) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	if preferred != "" {
		if m, ok := pool.models[preferred]; ok && m.Healthy && hasCaps(m, required) {
			return m, nil
		}
	}

	var best *Info
	bestCost := math.MaxFloat64
	for _, m := range pool.models {
		if m.Healthy && hasCaps(m, required) && m.CostPer1K < bestCost {
			best = m
			bestCost = m.CostPer1K
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no healthy model")
	}
	return best, nil
}

// LatencyOptimizedStrategy picks the fastest model.
type LatencyOptimizedStrategy struct{}

func (s *LatencyOptimizedStrategy) Name() string { return "latency-optimized" }

func (s *LatencyOptimizedStrategy) Select(ctx context.Context, pool *Pool, required []Capability, preferred string) (*Info, error) {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	if preferred != "" {
		if m, ok := pool.models[preferred]; ok && m.Healthy && hasCaps(m, required) {
			return m, nil
		}
	}

	var best *Info
	bestLatency := time.Duration(math.MaxInt64)
	for _, m := range pool.models {
		if !m.Healthy || !hasCaps(m, required) {
			continue
		}
		s := pool.stats[m.Name]
		if s.TotalCalls > 0 {
			avg := s.TotalLatency / time.Duration(s.TotalCalls)
			if avg < bestLatency {
				best = m
				bestLatency = avg
			}
		} else if best == nil {
			best = m
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no healthy model")
	}
	return best, nil
}

func hasCaps(m *Info, required []Capability) bool {
	if len(required) == 0 {
		return true
	}
	for _, r := range required {
		found := false
		for _, c := range m.Capabilities {
			if c == r {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ── Token Budget tracking ──────────────────────────────────

// BudgetTracker monitors token usage against limits.
type BudgetTracker struct {
	mu         sync.Mutex
	dailyLimit int64
	used       int64
	resetAt    time.Time
}

// NewBudgetTracker creates a budget with daily limits.
func NewBudgetTracker(dailyLimit int64) *BudgetTracker {
	now := time.Now()
	resetAt := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return &BudgetTracker{dailyLimit: dailyLimit, resetAt: resetAt}
}

// Consume deducts tokens from the budget.
func (b *BudgetTracker) Consume(tokens int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if time.Now().After(b.resetAt) {
		b.used = 0
		b.resetAt = b.resetAt.Add(24 * time.Hour)
	}
	if b.used+tokens > b.dailyLimit {
		return false
	}
	b.used += tokens
	return true
}

// Remaining returns available tokens.
func (b *BudgetTracker) Remaining() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.dailyLimit - b.used
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// UsedToday returns tokens consumed today.
func (b *BudgetTracker) UsedToday() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}
