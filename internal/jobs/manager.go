// Package jobs manages background tasks (bash run_in_background, task
// run_in_background) across turns. The agent holds one Manager per session;
// background tools register jobs, bash_output/wait/kill_shell operate on
// the manager through context.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Manager holds running background jobs for one session. It is safe for
// concurrent use (tools may start/kill jobs in parallel).
type Manager struct {
	mu   sync.Mutex
	jobs map[string]*Job
	seq  atomic.Int64
}

// Job is one running background task.
type Job struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "bash" or "task"
	Label     string    `json:"label"`
	StartedAt time.Time `json:"started_at"`
	// mu guards the mutable fields below: the completion goroutine writes them
	// while bash_output/kill_shell read/write them from other goroutines.
	mu       sync.Mutex
	Status   JobStatus `json:"status"`
	Result   string    `json:"result,omitempty"`
	Err      string    `json:"err,omitempty"`
	resultCh chan jobResult
	cancel   func()
}

// JobStatus is the lifecycle state of a background job.
type JobStatus string

const (
	StatusRunning JobStatus = "running"
	StatusDone    JobStatus = "done"
	StatusFailed  JobStatus = "failed"
	StatusKilled  JobStatus = "killed"
)

type jobResult struct {
	output string
	err    error
}

// ctxKey is the context key for the jobs Manager.
type ctxKey struct{}

// WithManager stamps ctx with the session's job manager.
func WithManager(ctx context.Context, m *Manager) context.Context {
	return context.WithValue(ctx, ctxKey{}, m)
}

// FromContext returns the job manager from ctx, or nil.
func FromContext(ctx context.Context) *Manager {
	m, _ := ctx.Value(ctxKey{}).(*Manager)
	return m
}

// NewManager creates an empty job manager.
func NewManager() *Manager {
	return &Manager{jobs: map[string]*Job{}}
}

// Start launches a background function as a named job and returns its ID.
// The fn receives a context that is cancelled when the job is killed.
func (m *Manager) Start(jobType, label string, fn func(ctx context.Context) (string, error)) *Job {
	return m.StartContext(context.Background(), jobType, label, fn)
}

// StartContext launches a background job while preserving immutable run-scoped
// context values. The parent's cancellation/deadline is intentionally detached;
// the returned job owns cancellation through Kill.
func (m *Manager) StartContext(parent context.Context, jobType, label string, fn func(ctx context.Context) (string, error)) *Job {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))

	id := fmt.Sprintf("%s-%d", jobType, m.seq.Add(1))

	job := &Job{
		ID:        id,
		Type:      jobType,
		Label:     label,
		StartedAt: time.Now(),
		Status:    StatusRunning,
		resultCh:  make(chan jobResult, 1),
		cancel:    cancel,
	}

	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()

	go func() {
		output, err := fn(ctx)
		job.resultCh <- jobResult{output: output, err: err}
		job.mu.Lock()
		if job.Status == StatusRunning {
			if err != nil {
				job.Status = StatusFailed
				job.Err = err.Error()
			} else {
				job.Status = StatusDone
			}
			job.Result = output
		}
		job.mu.Unlock()
	}()

	return job
}

// Get returns a job by ID, or nil if not found.
func (m *Manager) Get(id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

// List returns all jobs, ordered by start time.
func (m *Manager) List() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j)
	}
	return out
}

// Kill stops a running job and removes it. Returns the job, or nil.
func (m *Manager) Kill(id string) *Job {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.jobs, id)
	m.mu.Unlock()

	job.mu.Lock()
	wasRunning := job.Status == StatusRunning
	if wasRunning {
		job.Status = StatusKilled
	}
	job.mu.Unlock()
	if wasRunning {
		job.cancel() // outside job.mu; the completion goroutine only writes when StatusRunning, so it won't clobber StatusKilled
	}
	return job
}

// Wait blocks until the job finishes (or ctx is done), then removes it.
// Returns the result and error.
func (m *Manager) Wait(ctx context.Context, id string) (string, error) {
	job := m.Get(id)
	if job == nil {
		return "", fmt.Errorf("job %q not found", id)
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-job.resultCh:
		m.mu.Lock()
		delete(m.jobs, id)
		m.mu.Unlock()
		return r.output, r.err
	}
}

// OutputWait collects new output from a running job. This is a placeholder
// — in a real implementation, the job's fn would write to a buffer that
// OutputWait reads. For now, it returns the final result if the job is done.
func (m *Manager) OutputWait(id string) (string, bool) {
	job := m.Get(id)
	if job == nil {
		return "", false
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.Status != StatusRunning {
		return job.Result, true
	}
	return "", false
}

// ReadNew returns output produced since the last ReadNew call for this job.
// For now, returns the full result if the job is done, or empty string if still running.
func (j *Job) ReadNew() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.Status != StatusRunning {
		return j.Result
	}
	return ""
}

// Snapshot returns a consistent copy of the job's mutable state under the lock,
// so readers (bash_output / kill_shell) never touch the fields directly while
// the completion goroutine is writing them.
func (j *Job) Snapshot() (status JobStatus, result, errStr string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status, j.Result, j.Err
}

func (j *Job) statusSafe() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status
}

// MarshalJSON serializes the job under its lock, so json.Marshal-ing a *Job
// (e.g. from List()) never races the completion goroutine writing its fields.
func (j *Job) MarshalJSON() ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	type alias struct {
		ID        string    `json:"id"`
		Type      string    `json:"type"`
		Label     string    `json:"label"`
		StartedAt time.Time `json:"started_at"`
		Status    JobStatus `json:"status"`
		Result    string    `json:"result,omitempty"`
		Err       string    `json:"err,omitempty"`
	}
	return json.Marshal(alias{j.ID, j.Type, j.Label, j.StartedAt, j.Status, j.Result, j.Err})
}

// WaitAll blocks until all running jobs finish, or timeout expires.
func (m *Manager) WaitAll(timeout time.Duration) map[string]string {
	results := make(map[string]string)
	m.mu.Lock()
	allJobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		allJobs = append(allJobs, j)
	}
	m.mu.Unlock()

	var jobIDs []string
	for _, j := range allJobs {
		if j.statusSafe() == StatusRunning {
			jobIDs = append(jobIDs, j.ID)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for _, id := range jobIDs {
		out, err := m.Wait(ctx, id)
		if err != nil {
			results[id] = fmt.Sprintf("timeout or error: %v", err)
		} else {
			results[id] = out
		}
	}
	return results
}

// Clean removes all finished/killed jobs.
func (m *Manager) Clean() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, j := range m.jobs {
		if j.statusSafe() != StatusRunning {
			delete(m.jobs, id)
		}
	}
}
