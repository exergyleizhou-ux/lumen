// Package registry provides Task and Team registries for multi-agent
// coordination. Adapted from claw-code's task_registry.rs and
// team_cron_registry.rs.
package registry

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// TaskStatus represents the lifecycle of a task.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Task is one unit of work tracked by the registry.
type Task struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	AssignedTo  string     `json:"assigned_to,omitempty"` // team name
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Output      string     `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// Team is a named group that can be assigned tasks.
type Team struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"` // agent names
}

// CronJob is a scheduled recurring task.
type CronJob struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Schedule  string    `json:"schedule"` // cron expression
	Task      string    `json:"task"`     // task template
	LastRun   time.Time `json:"last_run"`
	NextRun   time.Time `json:"next_run"`
	Enabled   bool      `json:"enabled"`
}

// TaskRegistry manages tasks with thread-safe operations.
type TaskRegistry struct {
	mu    sync.RWMutex
	tasks map[string]*Task
	seq   int64
}

// NewTaskRegistry creates an empty task registry.
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{tasks: map[string]*Task{}}
}

// Create adds a new task and returns it.
func (r *TaskRegistry) Create(name, description string) *Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	now := time.Now()
	task := &Task{
		ID:          fmt.Sprintf("task-%d", r.seq),
		Name:        name,
		Description: description,
		Status:      TaskPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	r.tasks[task.ID] = task
	return task
}

// Get returns a task by ID.
func (r *TaskRegistry) Get(id string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	return t, ok
}

// List returns all tasks.
func (r *TaskRegistry) List() []*Task {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Task, 0, len(r.tasks))
	for _, t := range r.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// UpdateStatus changes a task's status.
func (r *TaskRegistry) UpdateStatus(id string, status TaskStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	t.Status = status
	t.UpdatedAt = time.Now()
	return nil
}

// AppendOutput adds to a task's output buffer.
func (r *TaskRegistry) AppendOutput(id, output string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	t.Output += output
	t.UpdatedAt = time.Now()
	return nil
}

// SetError records a task error.
func (r *TaskRegistry) SetError(id, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	t.Error = errMsg
	t.Status = TaskFailed
	t.UpdatedAt = time.Now()
	return nil
}

// AssignTo assigns a task to a team.
func (r *TaskRegistry) AssignTo(id, teamName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	t.AssignedTo = teamName
	t.UpdatedAt = time.Now()
	return nil
}

// Stop cancels a running or pending task.
func (r *TaskRegistry) Stop(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	if t.Status == TaskRunning || t.Status == TaskPending {
		t.Status = TaskCancelled
		t.UpdatedAt = time.Now()
	}
	return nil
}

// ── Team Registry ──────────────────────────────────────────

// TeamRegistry manages teams.
type TeamRegistry struct {
	mu    sync.RWMutex
	teams map[string]*Team
}

// NewTeamRegistry creates an empty team registry.
func NewTeamRegistry() *TeamRegistry {
	return &TeamRegistry{teams: map[string]*Team{}}
}

// Create adds a new team.
func (r *TeamRegistry) Create(name, description string, members []string) *Team {
	r.mu.Lock()
	defer r.mu.Unlock()
	team := &Team{Name: name, Description: description, Members: members}
	r.teams[name] = team
	return team
}

// Get returns a team by name.
func (r *TeamRegistry) Get(name string) (*Team, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.teams[name]
	return t, ok
}

// Delete removes a team.
func (r *TeamRegistry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.teams[name]; !ok {
		return fmt.Errorf("team %q not found", name)
	}
	delete(r.teams, name)
	return nil
}

// List returns all teams.
func (r *TeamRegistry) List() []*Team {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Team, 0, len(r.teams))
	for _, t := range r.teams {
		out = append(out, t)
	}
	return out
}

// ── Cron Registry ──────────────────────────────────────────

// CronRegistry manages scheduled tasks.
type CronRegistry struct {
	mu   sync.RWMutex
	jobs map[string]*CronJob
	seq  int64
}

// NewCronRegistry creates an empty cron registry.
func NewCronRegistry() *CronRegistry {
	return &CronRegistry{jobs: map[string]*CronJob{}}
}

// Create adds a new cron job.
func (r *CronRegistry) Create(name, schedule, task string) *CronJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	now := time.Now()
	job := &CronJob{
		ID:       fmt.Sprintf("cron-%d", r.seq),
		Name:     name,
		Schedule: schedule,
		Task:     task,
		LastRun:  now,
		Enabled:  true,
	}
	r.jobs[job.ID] = job
	return job
}

// Get returns a cron job by ID.
func (r *CronRegistry) Get(id string) (*CronJob, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	j, ok := r.jobs[id]
	return j, ok
}

// Delete removes a cron job.
func (r *CronRegistry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[id]; !ok {
		return fmt.Errorf("cron job %q not found", id)
	}
	delete(r.jobs, id)
	return nil
}

// List returns all cron jobs.
func (r *CronRegistry) List() []*CronJob {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*CronJob, 0, len(r.jobs))
	for _, j := range r.jobs {
		out = append(out, j)
	}
	return out
}

// Enable toggles a cron job on/off.
func (r *CronRegistry) Enable(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return fmt.Errorf("cron job %q not found", id)
	}
	j.Enabled = enabled
	return nil
}
