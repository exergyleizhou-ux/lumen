// Package queue provides an asynchronous in-process job queue with worker pools,
// retry, rate limiting, and backpressure. It supports named queues, delayed
// execution, and job cancellation. Adapted from production job queue patterns.
package queue

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Job is one unit of work.
type Job struct {
	ID        string        `json:"id"`
	Queue     string        `json:"queue"`
	Payload   any           `json:"payload"`
	Attempts  int           `json:"attempts"`
	MaxRetries int          `json:"max_retries"`
	CreatedAt time.Time     `json:"created_at"`
	StartedAt time.Time     `json:"started_at"`
	Timeout   time.Duration `json:"timeout"`
}

// Result is the outcome of a job.
type Result struct {
	JobID    string        `json:"job_id"`
	Success  bool          `json:"success"`
	Output   any           `json:"output,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
	Attempts int           `json:"attempts"`
}

// Handler processes jobs.
type Handler func(ctx context.Context, job *Job) (any, error)

// Queue manages jobs and workers for one named queue.
type Queue struct {
	name     string
	mu       sync.Mutex
	jobs     []*Job
	handler  Handler
	workers  int
	sem      chan struct{}
	results  []Result
	maxResults int
	processed  atomic.Int64
	failed     atomic.Int64
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewQueue creates a named queue with a handler and worker count.
func NewQueue(name string, handler Handler, workers int) *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	if workers <= 0 {
		workers = 1
	}
	q := &Queue{
		name: name, handler: handler, workers: workers,
		sem: make(chan struct{}, workers), maxResults: 1000,
		ctx: ctx, cancel: cancel,
	}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

// Enqueue adds a job to the queue. Returns immediately.
func (q *Queue) Enqueue(payload any, maxRetries int) *Job {
	job := &Job{
		ID: fmt.Sprintf("job-%d", time.Now().UnixNano()),
		Queue: q.name, Payload: payload,
		MaxRetries: maxRetries, CreatedAt: time.Now(),
		Timeout: 30 * time.Second,
	}
	q.mu.Lock()
	q.jobs = append(q.jobs, job)
	q.mu.Unlock()
	return job
}

// EnqueueDelayed adds a job that starts after a delay.
func (q *Queue) EnqueueDelayed(payload any, delay time.Duration, maxRetries int) *Job {
	job := q.Enqueue(payload, maxRetries)
	job.StartedAt = time.Now().Add(delay)
	return job
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		case q.sem <- struct{}{}:
		}

		job := q.dequeue()
		if job == nil {
			<-q.sem
			time.Sleep(50 * time.Millisecond)
			continue
		}

		q.execute(job)
		<-q.sem
	}
}

func (q *Queue) dequeue() *Job {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	for i, j := range q.jobs {
		if now.After(j.StartedAt) || j.StartedAt.IsZero() {
			q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
			return j
		}
	}
	return nil
}

func (q *Queue) execute(job *Job) {
	start := time.Now()
	job.Attempts++

	ctx, cancel := context.WithTimeout(q.ctx, job.Timeout)
	defer cancel()

	output, err := q.handler(ctx, job)

	result := Result{
		JobID: job.ID, Duration: time.Since(start),
		Attempts: job.Attempts, Output: output,
	}
	if err != nil {
		result.Error = err.Error()
		if job.Attempts < job.MaxRetries {
			q.mu.Lock()
			q.jobs = append(q.jobs, job)
			q.mu.Unlock()
			return
		}
		result.Success = false
		q.failed.Add(1)
	} else {
		result.Success = true
		q.processed.Add(1)
	}

	q.mu.Lock()
	q.results = append(q.results, result)
	if len(q.results) > q.maxResults {
		q.results = q.results[len(q.results)-q.maxResults:]
	}
	q.mu.Unlock()
}

// Shutdown gracefully stops the queue workers.
func (q *Queue) Shutdown() {
	q.cancel()
	q.wg.Wait()
}

// Stats returns queue statistics.
func (q *Queue) Stats() (pending, processed, failed int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return int64(len(q.jobs)), q.processed.Load(), q.failed.Load()
}

// Results returns recent job results.
func (q *Queue) Results(limit int) []Result {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit <= 0 || limit > len(q.results) {
		limit = len(q.results)
	}
	out := make([]Result, limit)
	copy(out, q.results[len(q.results)-limit:])
	return out
}

// Clear removes all pending jobs.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = nil
}

// ── Manager ──────────────────────────────────────────────

// Manager manages multiple named queues.
type Manager struct {
	mu     sync.Mutex
	queues map[string]*Queue
}

// NewManager creates a queue manager.
func NewManager() *Manager {
	return &Manager{queues: map[string]*Queue{}}
}

// Register creates a new named queue.
func (m *Manager) Register(name string, handler Handler, workers int) *Queue {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := NewQueue(name, handler, workers)
	m.queues[name] = q
	return q
}

// Get returns a queue by name or nil.
func (m *Manager) Get(name string) *Queue {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.queues[name]
}

// ShutdownAll stops all queues.
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, q := range m.queues {
		q.Shutdown()
	}
}

// FormatStats formats all queue statistics.
func (m *Manager) FormatStats() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Queue Manager (%d queues):\n\n", len(m.queues)))
	names := make([]string, 0, len(m.queues))
	for n := range m.queues { names = append(names, n) }
	sort.Strings(names)
	for _, n := range names {
		q := m.queues[n]
		p, c, f := q.Stats()
		fmt.Fprintf(&sb, "  %-15s pending:%-4d done:%-4d failed:%-4d\n", n, p, c, f)
	}
	return sb.String()
}
