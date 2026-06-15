// Package orchestrator coordinates multi-agent workflows with parallel
// execution, dependency resolution, and result aggregation. It manages
// agent pools, dispatches tasks across agents, and handles failure recovery.
// This is the production multi-agent coordination layer.
package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── Types ──────────────────────────────────────────────────

// Plan is a directed acyclic graph of tasks to execute.
type Plan struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Tasks     []*Task   `json:"tasks"`
	CreatedAt time.Time `json:"created_at"`
	Status    PlanStatus `json:"status"`
}

// PlanStatus tracks the lifecycle of a plan.
type PlanStatus string

const (
	PlanPending   PlanStatus = "pending"
	PlanRunning   PlanStatus = "running"
	PlanCompleted PlanStatus = "completed"
	PlanFailed    PlanStatus = "failed"
	PlanCancelled PlanStatus = "cancelled"
)

// Task is one unit of work in a plan.
type Task struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Agent       string        `json:"agent"`
	Prompt      string        `json:"prompt"`
	DependsOn   []string      `json:"depends_on,omitempty"`
	Status      TaskStatus    `json:"status"`
	Result      string        `json:"result,omitempty"`
	Error       string        `json:"error,omitempty"`
	StartedAt   time.Time     `json:"started_at,omitempty"`
	CompletedAt time.Time     `json:"completed_at,omitempty"`
	Attempts    int           `json:"attempts"`
	MaxRetries  int           `json:"max_retries"`
	Timeout     time.Duration `json:"timeout"`
	Priority    int           `json:"priority"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// TaskStatus tracks one task's lifecycle.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskDone      TaskStatus = "done"
	TaskFailed    TaskStatus = "failed"
	TaskSkipped   TaskStatus = "skipped"
	TaskCancelled TaskStatus = "cancelled"
)

// ── Agent interface ─────────────────────────────────────────

// Agent is any entity that can execute a task.
type Agent interface {
	Name() string
	Execute(ctx context.Context, prompt string) (string, error)
	Capabilities() []string
	IsAvailable() bool
	MaxConcurrency() int
}

// ── Agent Pool ─────────────────────────────────────────────

// AgentPool manages a collection of agents with load balancing.
type AgentPool struct {
	mu      sync.RWMutex
	agents  map[string]Agent
	busy    map[string]int
	stats   map[string]*AgentStats
}

// AgentStats tracks per-agent usage metrics.
type AgentStats struct {
	TotalTasks    int64 `json:"total_tasks"`
	SuccessTasks  int64 `json:"success_tasks"`
	FailedTasks   int64 `json:"failed_tasks"`
	TotalDuration time.Duration `json:"total_duration"`
	mu            sync.Mutex
}

// NewAgentPool creates an empty agent pool.
func NewAgentPool() *AgentPool {
	return &AgentPool{
		agents: map[string]Agent{},
		busy:   map[string]int{},
		stats:  map[string]*AgentStats{},
	}
}

// Register adds an agent to the pool.
func (p *AgentPool) Register(a Agent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agents[a.Name()] = a
	p.stats[a.Name()] = &AgentStats{}
}

// Remove deregisters an agent.
func (p *AgentPool) Remove(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.agents, name)
	delete(p.stats, name)
}

// Select picks the best available agent for a task based on load and
// capability matching.
func (p *AgentPool) Select(preferred string, requiredCaps []string) Agent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Preferred agent first
	if a, ok := p.agents[preferred]; ok && a.IsAvailable() && p.canHandle(a, requiredCaps) {
		return a
	}

	// Least busy capable agent
	var best Agent
	bestBusy := int(^uint(0) >> 1)
	for name, a := range p.agents {
		if !a.IsAvailable() || a == nil { continue }
		if !p.canHandle(a, requiredCaps) { continue }
		busy := p.busy[name]
		if busy < bestBusy {
			best = a
			bestBusy = busy
		}
	}
	return best
}

func (p *AgentPool) canHandle(a Agent, caps []string) bool {
	if len(caps) == 0 { return true }
	ac := a.Capabilities()
	for _, required := range caps {
		found := false
		for _, c := range ac {
			if c == required { found = true; break }
		}
		if !found { return false }
	}
	return true
}

// Acquire marks an agent as busy. Returns false if no capacity.
func (p *AgentPool) Acquire(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.agents[name]
	if !ok { return false }
	max := a.MaxConcurrency()
	if max <= 0 { max = 1 }
	if p.busy[name] >= max { return false }
	p.busy[name]++
	return true
}

// Release marks an agent as free.
func (p *AgentPool) Release(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.busy[name] > 0 { p.busy[name]-- }
}

// RecordSuccess records a successful task execution.
func (p *AgentPool) RecordSuccess(name string, d time.Duration) {
	p.mu.RLock()
	s, ok := p.stats[name]
	p.mu.RUnlock()
	if !ok { return }
	s.mu.Lock()
	s.TotalTasks++
	s.SuccessTasks++
	s.TotalDuration += d
	s.mu.Unlock()
}

// RecordFailure records a failed task.
func (p *AgentPool) RecordFailure(name string, d time.Duration) {
	p.mu.RLock()
	s, ok := p.stats[name]
	p.mu.RUnlock()
	if !ok { return }
	s.mu.Lock()
	s.TotalTasks++
	s.FailedTasks++
	s.TotalDuration += d
	s.mu.Unlock()
}

// Stats returns pool-wide statistics.
func (p *AgentPool) Stats() map[string]*AgentStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]*AgentStats)
	for n, s := range p.stats { out[n] = s }
	return out
}

// FormatStats formats pool statistics.
func (p *AgentPool) FormatStats() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Agent Pool (%d agents):\n\n", len(p.agents)))
	stats := p.Stats()
	names := make([]string, 0, len(stats))
	for n := range stats { names = append(names, n) }
	sort.Strings(names)
	for _, n := range names {
		s := stats[n]
		fmt.Fprintf(&sb, "  %-15s total:%4d success:%4d fail:%4d\n",
			n, s.TotalTasks, s.SuccessTasks, s.FailedTasks)
	}
	return sb.String()
}

// ── Executor ───────────────────────────────────────────────

// Config tunes the orchestrator.
type Config struct {
	MaxParallel    int           `json:"max_parallel"`
	DefaultTimeout time.Duration `json:"default_timeout"`
	MaxRetries     int           `json:"max_retries"`
	RetryDelay     time.Duration `json:"retry_delay"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxParallel:    8,
		DefaultTimeout: 5 * time.Minute,
		MaxRetries:     2,
		RetryDelay:     2 * time.Second,
	}
}

// Executor coordinates multi-agent plan execution.
type Executor struct {
	cfg   Config
	pool  *AgentPool
	mu    sync.Mutex
	plans map[string]*Plan
	seq   atomic.Int64
}

// NewExecutor creates a plan executor.
func NewExecutor(cfg Config, pool *AgentPool) *Executor {
	return &Executor{cfg: cfg, pool: pool, plans: map[string]*Plan{}}
}

// NewPlan creates an execution plan from a set of tasks.
func (e *Executor) NewPlan(name string, tasks []*Task) *Plan {
	id := fmt.Sprintf("plan-%d", e.seq.Add(1))
	plan := &Plan{ID: id, Name: name, Tasks: tasks, CreatedAt: time.Now(), Status: PlanPending}
	e.mu.Lock()
	e.plans[id] = plan
	e.mu.Unlock()
	return plan
}

// Execute runs a plan, respecting task dependencies and parallelism limits.
func (e *Executor) Execute(ctx context.Context, plan *Plan) error {
	plan.Status = PlanRunning
	e.mu.Lock()
	e.plans[plan.ID] = plan
	e.mu.Unlock()

	// Track task completion status
	completed := map[string]bool{}

	for len(completed) < len(plan.Tasks) {
		if ctx.Err() != nil {
			plan.Status = PlanCancelled
			return ctx.Err()
		}

		// Find ready tasks (all dependencies completed)
		var ready []*Task
		for _, t := range plan.Tasks {
			if completed[t.ID] { continue }
			if t.Status == TaskRunning { continue }
			allDepsDone := true
			for _, dep := range t.DependsOn {
				if !completed[dep] { allDepsDone = false; break }
			}
			if allDepsDone { ready = append(ready, t) }
		}

		if len(ready) == 0 && len(completed) < len(plan.Tasks) {
			return fmt.Errorf("deadlock detected: %d tasks blocked", len(plan.Tasks)-len(completed))
		}

		// Execute ready tasks in parallel (up to maxParallel)
		sem := make(chan struct{}, e.cfg.MaxParallel)
		var wg sync.WaitGroup
		var firstErr error
		var errMu sync.Mutex

		for _, task := range ready {
			task.Status = TaskRunning
			task.StartedAt = time.Now()
			wg.Add(1)
			sem <- struct{}{}
			go func(t *Task) {
				defer wg.Done()
				defer func() { <-sem }()
				e.executeTask(ctx, t)
				if t.Status == TaskFailed {
					errMu.Lock()
					if firstErr == nil { firstErr = fmt.Errorf("%s: %s", t.Name, t.Error) }
					errMu.Unlock()
				}
			}(task)
		}
		wg.Wait()

		// Mark completed tasks
		for _, t := range ready {
			if t.Status == TaskDone || t.Status == TaskFailed || t.Status == TaskSkipped {
				completed[t.ID] = true
			}
		}

		if firstErr != nil && allDoneOrFailed(completed, plan.Tasks) {
			plan.Status = PlanFailed
			return firstErr
		}
	}

	allSuccess := true
	for _, t := range plan.Tasks {
		if t.Status != TaskDone { allSuccess = false; break }
	}
	if allSuccess {
		plan.Status = PlanCompleted
	}
	return nil
}

func (e *Executor) executeTask(ctx context.Context, task *Task) {
	// Select agent
	agent := e.pool.Select(task.Agent, nil)
	if agent == nil {
		task.Status = TaskFailed
		task.Error = "no agent available"
		return
	}

	// Acquire the agent
	if !e.pool.Acquire(agent.Name()) {
		task.Status = TaskFailed
		task.Error = "agent at capacity"
		return
	}
	defer e.pool.Release(agent.Name())

	// Execute with retries
	timeout := task.Timeout
	if timeout <= 0 { timeout = e.cfg.DefaultTimeout }

	var result string
	var err error
	for attempt := 0; attempt <= task.MaxRetries; attempt++ {
		if attempt > 0 { time.Sleep(e.cfg.RetryDelay) }

		taskCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		result, err = agent.Execute(taskCtx, task.Prompt)
		cancel()

		if err == nil {
			e.pool.RecordSuccess(agent.Name(), time.Since(start))
			task.Result = result
			task.Status = TaskDone
			task.CompletedAt = time.Now()
			return
		}
		task.Attempts = attempt + 1
	}

	e.pool.RecordFailure(agent.Name(), time.Since(task.StartedAt))
	task.Error = err.Error()
	task.Status = TaskFailed
	task.CompletedAt = time.Now()
}

func buildDepGraph(tasks []*Task) map[string][]string {
	g := map[string][]string{}
	for _, t := range tasks {
		g[t.ID] = t.DependsOn
	}
	return g
}

func allDoneOrFailed(completed map[string]bool, tasks []*Task) bool {
	for _, t := range tasks {
		if !completed[t.ID] { return false }
	}
	return true
}

// GetPlan returns a plan by ID.
func (e *Executor) GetPlan(id string) *Plan {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.plans[id]
}

// CancelPlan cancels a running plan.
func (e *Executor) CancelPlan(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p, ok := e.plans[id]; ok && p.Status == PlanRunning {
		p.Status = PlanCancelled
	}
}

// FormatPlan formats a plan for display.
func (e *Executor) FormatPlan(plan *Plan) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Plan: %s (%s)\n", plan.Name, plan.Status)
	fmt.Fprintf(&sb, "%s\n\n", strings.Repeat("─", 50))
	for _, t := range plan.Tasks {
		icon := iconForStatus(t.Status)
		fmt.Fprintf(&sb, "%s %-25s [%s]", icon, t.Name, t.Status)
		if t.Agent != "" { fmt.Fprintf(&sb, " via %s", t.Agent) }
		if t.Result != "" {
			result := t.Result
			if len(result) > 60 { result = result[:57] + "..." }
			fmt.Fprintf(&sb, " → %s", result)
		}
		if t.Error != "" { fmt.Fprintf(&sb, " ✗ %s", t.Error) }
		sb.WriteByte('\n')
	}
	return sb.String()
}

func iconForStatus(s TaskStatus) string {
	switch s {
	case TaskDone: return "✅"
	case TaskFailed: return "❌"
	case TaskRunning: return "🔄"
	case TaskPending: return "○"
	case TaskSkipped: return "⏭"
	case TaskCancelled: return "⊘"
	default: return "·"
	}
}

// ── AgentPool usage tracking ───────────────────────────────

// UsageSnapshot is a point-in-time usage report.
type UsageSnapshot struct {
	Timestamp time.Time              `json:"timestamp"`
	Agents    map[string]AgentStats  `json:"agents"`
	TotalBusy int                    `json:"total_busy"`
}

// Snapshot takes a usage snapshot.
func (p *AgentPool) Snapshot() *UsageSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := &UsageSnapshot{Timestamp: time.Now(), Agents: map[string]AgentStats{}}
	for n, st := range p.stats {
		s.Agents[n] = *st
		s.TotalBusy += p.busy[n]
	}
	return s
}
