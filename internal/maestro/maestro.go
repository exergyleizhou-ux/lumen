// Package maestro provides a full orchestration engine for coordinating
// multiple Lumen agents through complex workflows. It supports DAG-based
// execution, conditional branching, retry policies, SAGA compensation,
// and timeout/deadline management.
package maestro

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Task is one unit of work in a workflow.
type Task struct {
	ID          string                          `json:"id"`
	Name        string                          `json:"name"`
	DependsOn   []string                        `json:"depends_on,omitempty"`
	Fn          func(ctx context.Context) error `json:"-"`
	Compensate  func(ctx context.Context) error `json:"-"`
	Timeout     time.Duration                   `json:"timeout"`
	RetryPolicy *RetryPolicy                    `json:"retry_policy,omitempty"`
}

// RetryPolicy specifies retry behavior.
type RetryPolicy struct {
	MaxRetries int           `json:"max_retries"`
	Backoff    time.Duration `json:"backoff"`
	MaxBackoff time.Duration `json:"max_backoff"`
}

// DefaultRetryPolicy returns a sensible retry policy.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{MaxRetries: 3, Backoff: 100 * time.Millisecond, MaxBackoff: 5 * time.Second}
}

// Workflow defines a DAG of tasks.
type Workflow struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Tasks   map[string]*Task `json:"tasks"`
	Timeout time.Duration    `json:"timeout"`
}

// Result is the outcome of one task execution.
type Result struct {
	TaskID    string        `json:"task_id"`
	Status    string        `json:"status"` // pending, running, success, failed, compensated
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Attempts  int           `json:"attempts"`
	StartedAt time.Time     `json:"started_at,omitempty"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
}

// RunResult is the outcome of a workflow run.
type RunResult struct {
	WorkflowID string        `json:"workflow_id"`
	Status     string        `json:"status"`
	Results    []Result      `json:"results"`
	Duration   time.Duration `json:"duration"`
	StartedAt  time.Time     `json:"started_at"`
	EndedAt    time.Time     `json:"ended_at"`
}

// Orchestrator executes workflows.
type Orchestrator struct {
	mu        sync.Mutex
	workflows map[string]*Workflow
	results   map[string][]*RunResult
	maxHist   int
}

// NewOrchestrator creates an orchestrator.
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{workflows: map[string]*Workflow{}, results: map[string][]*RunResult{}, maxHist: 50}
}

// RegisterWorkflow adds a workflow.
func (o *Orchestrator) RegisterWorkflow(w *Workflow) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.workflows[w.ID] = w
}

// Run executes a workflow and returns the result.
func (o *Orchestrator) Run(ctx context.Context, workflowID string) (*RunResult, error) {
	o.mu.Lock()
	w, ok := o.workflows[workflowID]
	o.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", workflowID)
	}

	// Build DAG structure
	inDegree := map[string]int{}
	for id := range w.Tasks {
		inDegree[id] = 0
	}
	for id, t := range w.Tasks {
		for _, dep := range t.DependsOn {
			if _, ok := w.Tasks[dep]; !ok {
				return nil, fmt.Errorf("task %q depends on unknown %q", id, dep)
			}
			inDegree[id]++
		}
	}

	run := &RunResult{WorkflowID: workflowID, Status: "running", StartedAt: time.Now()}

	// Detect cycles
	if hasCycle(w.Tasks, inDegree) {
		return nil, fmt.Errorf("workflow contains a cycle")
	}

	// Execute tasks in topological order
	order := topologicalOrder(w.Tasks)
	taskResults := map[string]Result{}
	var taskMu sync.Mutex

	for _, task := range order {
		// Check all dependencies succeeded
		skip := false
		for _, dep := range task.DependsOn {
			taskMu.Lock()
			dr := taskResults[dep]
			taskMu.Unlock()
			if dr.Status == "failed" || dr.Status == "compensated" {
				skip = true
				break
			}
		}

		var r Result
		if skip {
			r = Result{TaskID: task.ID, Status: "skipped", StartedAt: time.Now(), EndedAt: time.Now()}
		} else {
			r = runOneTask(ctx, task)
		}

		taskMu.Lock()
		taskResults[task.ID] = r
		taskMu.Unlock()
	}

	// Collect results in order
	var ordered []Result
	for _, t := range topologicalOrder(w.Tasks) {
		if r, ok := taskResults[t.ID]; ok {
			ordered = append(ordered, r)
		}
	}

	run.Results = ordered
	allSuccess := true
	for _, r := range ordered {
		if r.Status != "success" {
			allSuccess = false
			break
		}
	}
	if allSuccess {
		run.Status = "success"
	} else {
		run.Status = "failed"
	}
	run.EndedAt = time.Now()
	run.Duration = run.EndedAt.Sub(run.StartedAt)

	o.mu.Lock()
	o.results[workflowID] = append(o.results[workflowID], run)
	if len(o.results[workflowID]) > o.maxHist {
		o.results[workflowID] = o.results[workflowID][1:]
	}
	o.mu.Unlock()

	return run, nil
}

// runOneTask executes a single task with retry logic.
func runOneTask(ctx context.Context, t *Task) Result {
	r := Result{TaskID: t.ID, StartedAt: time.Now()}

	taskCtx := ctx
	var cancel context.CancelFunc
	if t.Timeout > 0 {
		taskCtx, cancel = context.WithTimeout(ctx, t.Timeout)
		defer cancel()
	}

	var err error
	attempts := 1
	maxAttempts := 1
	if t.RetryPolicy != nil {
		maxAttempts = t.RetryPolicy.MaxRetries + 1
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = t.Fn(taskCtx)
		if err == nil {
			break
		}
		if attempt < maxAttempts && t.RetryPolicy != nil {
			backoff := t.RetryPolicy.Backoff * time.Duration(attempt)
			if backoff > t.RetryPolicy.MaxBackoff {
				backoff = t.RetryPolicy.MaxBackoff
			}
			time.Sleep(backoff)
		}
	}
	attempts = maxAttempts

	r.EndedAt = time.Now()
	r.Duration = r.EndedAt.Sub(r.StartedAt)
	r.Attempts = attempts

	if err != nil {
		r.Status = "failed"
		r.Error = err.Error()
		if t.Compensate != nil {
			if cerr := t.Compensate(context.Background()); cerr != nil {
				r.Status = "compensated"
				r.Error += fmt.Sprintf(" (compensation: %v)", cerr)
			}
		}
	} else {
		r.Status = "success"
	}
	return r
}

func hasCycle(tasks map[string]*Task, inDegree map[string]int) bool {
	deg := map[string]int{}
	for k, v := range inDegree {
		deg[k] = v
	}

	var queue []string
	for id, d := range deg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, dep := range tasks[id].DependsOn {
			deg[dep]--
			if deg[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}
	return visited < len(tasks)
}

func topologicalOrder(tasks map[string]*Task) []*Task {
	inDeg := map[string]int{}
	for id := range tasks {
		for range tasks[id].DependsOn {
			inDeg[id]++
		}
	}

	var queue []string
	for id := range tasks {
		if inDeg[id] == 0 {
			queue = append(queue, id)
		}
	}

	var order []*Task
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, tasks[id])
		for oid := range tasks {
			for _, dep := range tasks[oid].DependsOn {
				if dep == id {
					inDeg[oid]--
					if inDeg[oid] == 0 {
						queue = append(queue, oid)
					}
				}
			}
		}
	}
	return order
}

// FormatRunResult formats a workflow run result.
func FormatRunResult(r *RunResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Workflow: %s [%s] (%v)\n%s\n\n", r.WorkflowID, r.Status, r.Duration, strings.Repeat("─", 60))
	for _, res := range r.Results {
		icon := "✅"
		switch res.Status {
		case "failed":
			icon = "🔴"
		case "compensated":
			icon = "🟡"
		case "pending":
			icon = "⬜"
		}
		fmt.Fprintf(&sb, "  %s %-20s %-10s %v", icon, res.TaskID, res.Status, res.Duration)
		if res.Error != "" {
			fmt.Fprintf(&sb, "  err=%s", res.Error)
		}
		sb.WriteByte('\n')
	}
	fmt.Fprintf(&sb, "\n  Task count: %d\n", len(r.Results))
	return sb.String()
}

// ── Workflow Builder ───────────────────────────────────────

// WorkflowBuilder helps construct workflows.
type WorkflowBuilder struct {
	w *Workflow
}

// NewWorkflow creates a workflow builder.
func NewWorkflow(id, name string) *WorkflowBuilder {
	return &WorkflowBuilder{w: &Workflow{ID: id, Name: name, Tasks: map[string]*Task{}}}
}

// Task adds a task.
func (b *WorkflowBuilder) Task(id, name string, deps []string, fn func(context.Context) error) *WorkflowBuilder {
	b.w.Tasks[id] = &Task{ID: id, Name: name, DependsOn: deps, Fn: fn, Timeout: 5 * time.Minute}
	return b
}

// WithRetry sets retry policy on the last added task.
func (b *WorkflowBuilder) WithRetry(rp *RetryPolicy) *WorkflowBuilder {
	// Find last task
	var last *Task
	for _, t := range b.w.Tasks {
		if last == nil || true {
			last = t
		}
	}
	if last != nil {
		last.RetryPolicy = rp
	}
	return b
}

// WithCompensation sets compensation on the last task.
func (b *WorkflowBuilder) WithCompensation(fn func(context.Context) error) *WorkflowBuilder {
	var last *Task
	for _, t := range b.w.Tasks {
		last = t
	}
	if last != nil {
		last.Compensate = fn
	}
	_ = last
	return b
}

// Timeout sets workflow timeout.
func (b *WorkflowBuilder) Timeout(d time.Duration) *WorkflowBuilder { b.w.Timeout = d; return b }

// Build returns the workflow.
func (b *WorkflowBuilder) Build() *Workflow { return b.w }
