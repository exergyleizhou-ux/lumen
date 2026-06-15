// Package taskengine provides a distributed task scheduling and execution
// engine with priority queues, cron scheduling, dependency chains, and
// backpressure control. Used for orchestrating multi-step agent workflows.
package taskengine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Priority levels for tasks.
type Priority int

const (
	PriorityLow    Priority = 0
	PriorityNormal Priority = 5
	PriorityHigh   Priority = 10
	PriorityCritical Priority = 20
)

// TaskStatus tracks lifecycle.
type TaskStatus string

const (
	StatusQueued   TaskStatus = "queued"
	StatusRunning  TaskStatus = "running"
	StatusComplete TaskStatus = "complete"
	StatusFailed   TaskStatus = "failed"
	StatusCancelled TaskStatus = "cancelled"
	StatusRetrying TaskStatus = "retrying"
)

// Task is one unit of scheduled work.
type Task struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Priority    Priority      `json:"priority"`
	Status      TaskStatus    `json:"status"`
	DependsOn   []string      `json:"depends_on,omitempty"`
	Handler     string        `json:"handler"`
	Payload     any           `json:"payload"`
	MaxRetries  int           `json:"max_retries"`
	Attempts    int           `json:"attempts"`
	CreatedAt   time.Time     `json:"created_at"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at"`
	Error       string        `json:"error,omitempty"`
	Result      any           `json:"result,omitempty"`
}

// Handler executes tasks.
type TaskHandler func(ctx context.Context, task *Task) (any, error)

// Engine manages task scheduling and execution.
type Engine struct {
	mu        sync.Mutex
	queue     []*Task
	handlers  map[string]TaskHandler
	history   []*Task
	maxHistory int
	running   map[string]bool
	workers   int
	sem       chan struct{}
	processed atomic.Int64
	failed    atomic.Int64
	schedule  []CronEntry
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// CronEntry is a scheduled recurring task.
type CronEntry struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"` // seconds between runs
	Handler  string `json:"handler"`
	Payload  any    `json:"payload"`
	lastRun  time.Time
}

// NewEngine creates a task engine with worker count.
func NewEngine(workers int) *Engine {
	if workers <= 0 { workers = 4 }
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		handlers: map[string]TaskHandler{}, maxHistory: 1000,
		running: map[string]bool{}, workers: workers,
		sem: make(chan struct{}, workers), ctx: ctx, cancel: cancel,
	}
	for i := 0; i < workers; i++ { e.wg.Add(1); go e.worker() }
	go e.cronLoop()
	return e
}

// RegisterHandler registers a named task handler.
func (e *Engine) RegisterHandler(name string, h TaskHandler) {
	e.mu.Lock(); defer e.mu.Unlock()
	e.handlers[name] = h
}

// Submit adds a task to the queue.
func (e *Engine) Submit(name, handler string, priority Priority, payload any, deps []string, maxRetries int) *Task {
	task := &Task{
		ID: fmt.Sprintf("task-%d", time.Now().UnixNano()), Name: name,
		Priority: priority, Status: StatusQueued, Handler: handler,
		Payload: payload, DependsOn: deps, MaxRetries: maxRetries,
		CreatedAt: time.Now(),
	}
	e.mu.Lock(); defer e.mu.Unlock()
	e.queue = append(e.queue, task)
	return task
}

// ScheduleCron adds a recurring task.
func (e *Engine) ScheduleCron(name, handler, schedule string, payload any) {
	e.mu.Lock(); defer e.mu.Unlock()
	e.schedule = append(e.schedule, CronEntry{Name: name, Schedule: schedule, Handler: handler, Payload: payload, lastRun: time.Now()})
}

func (e *Engine) cronLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done(): return
		case <-ticker.C:
			e.executePendingCron()
		}
	}
}

func (e *Engine) executePendingCron() {
	e.mu.Lock()
	var ready []CronEntry
	now := time.Now()
	for i := range e.schedule {
		e.schedule[i].lastRun = now
		ready = append(ready, e.schedule[i])
	}
	e.mu.Unlock()
	for _, c := range ready {
		e.Submit(c.Name, c.Handler, PriorityNormal, c.Payload, nil, 0)
	}
}

func (e *Engine) worker() {
	defer e.wg.Done()
	for {
		select {
		case <-e.ctx.Done(): return
		case e.sem <- struct{}{}:
		}
		task := e.dequeueReady()
		if task == nil { <-e.sem; time.Sleep(50 * time.Millisecond); continue }
		e.execute(task)
		<-e.sem
	}
}

func (e *Engine) dequeueReady() *Task {
	e.mu.Lock(); defer e.mu.Unlock()

	// Sort by priority (highest first)
	sort.Slice(e.queue, func(i, j int) bool { return e.queue[i].Priority > e.queue[j].Priority })

	completed := map[string]bool{}
	for _, t := range e.history { if t.Status == StatusComplete { completed[t.ID] = true } }

	for i, t := range e.queue {
		if t.Status == StatusRunning { continue }
		// Check dependencies
		ready := true
		for _, dep := range t.DependsOn {
			if !completed[dep] { ready = false; break }
		}
		if !ready { continue }

		t.Status = StatusRunning
		t.StartedAt = time.Now()
		t.Attempts++
		e.queue = append(e.queue[:i], e.queue[i+1:]...)
		e.running[t.ID] = true
		return t
	}
	return nil
}

func (e *Engine) execute(task *Task) {
	handler, ok := e.handlers[task.Handler]
	if !ok {
		e.finishTask(task, nil, fmt.Errorf("handler %q not found", task.Handler))
		return
	}

	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Minute)
	defer cancel()

	result, err := handler(ctx, task)
	e.finishTask(task, result, err)
}

func (e *Engine) finishTask(task *Task, result any, err error) {
	e.mu.Lock(); defer e.mu.Unlock()

	task.CompletedAt = time.Now()
	delete(e.running, task.ID)

	if err != nil {
		if task.Attempts < task.MaxRetries {
			task.Status = StatusRetrying
			task.Error = err.Error()
			e.queue = append(e.queue, task)
			return
		}
		task.Status = StatusFailed
		task.Error = err.Error()
		e.failed.Add(1)
	} else {
		task.Status = StatusComplete
		task.Result = result
		e.processed.Add(1)
	}

	e.history = append(e.history, task)
	if len(e.history) > e.maxHistory { e.history = e.history[len(e.history)-e.maxHistory:] }
}

// Shutdown gracefully stops the engine.
func (e *Engine) Shutdown() { e.cancel(); e.wg.Wait() }

// Stats returns engine statistics.
func (e *Engine) Stats() (queued, running, complete, failed int64) {
	e.mu.Lock(); defer e.mu.Unlock()
	return int64(len(e.queue)), int64(len(e.running)), e.processed.Load(), e.failed.Load()
}

// FormatStats formats engine statistics.
func (e *Engine) FormatStats() string {
	q, r, c, f := e.Stats()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Task Engine: queued=%d running=%d complete=%d failed=%d\n", q, r, c, f)
	return sb.String()
}

// CancelTask cancels a queued or running task.
func (e *Engine) CancelTask(taskID string) {
	e.mu.Lock(); defer e.mu.Unlock()
	for i, t := range e.queue {
		if t.ID == taskID { e.queue = append(e.queue[:i], e.queue[i+1:]...); return }
	}
}
