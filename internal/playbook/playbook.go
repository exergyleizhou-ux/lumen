// Package playbook provides a YAML-defined agent behavior sequencer.
// Playbooks define agent actions, conditions, loops, and variable
// substitution — enabling repeatable, auditable agent runs without code.
package playbook

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Step is one action in a playbook.
type Step struct {
	Name      string         `json:"name"`
	Action    string         `json:"action"`
	With      map[string]any `json:"with,omitempty"`
	Condition string         `json:"condition,omitempty"`
	Loop      string         `json:"loop,omitempty"`
	Retry     int            `json:"retry,omitempty"`
	Timeout   string         `json:"timeout,omitempty"`
	DependsOn []string       `json:"depends_on,omitempty"`
}

// Playbook is a named sequence of steps.
type Playbook struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Steps       []Step         `json:"steps"`
	Variables   map[string]any `json:"variables,omitempty"`
}

// Status is the state of a step in an execution.
type StepStatus struct {
	StepName  string        `json:"step_name"`
	Action    string        `json:"action"`
	State     string        `json:"state"` // pending, running, success, failed, skipped
	Error     string        `json:"error,omitempty"`
	Output    string        `json:"output,omitempty"`
	Duration  time.Duration `json:"duration"`
	StartedAt time.Time     `json:"started_at,omitempty"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
}

// RunStatus is the outcome of a playbook run.
type RunStatus struct {
	Playbook  string        `json:"playbook"`
	Version   string        `json:"version"`
	Steps     []StepStatus  `json:"steps"`
	State     string        `json:"state"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
	Duration  time.Duration `json:"duration"`
}

// Executor runs playbooks against an action handler.
type Executor struct {
	mu      sync.Mutex
	handler func(action string, with map[string]any) (string, error)
	history []*RunStatus
	vars    map[string]any
	maxHist int
}

// NewExecutor creates a playbook executor.
func NewExecutor(handler func(action string, with map[string]any) (string, error)) *Executor {
	return &Executor{handler: handler, vars: map[string]any{}, maxHist: 100}
}

// SetVar sets a global variable.
func (e *Executor) SetVar(name string, value any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vars[name] = value
}

// GetVar retrieves a global variable.
func (e *Executor) GetVar(name string) (any, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.vars[name]
	return v, ok
}

// Run executes a playbook.
func (e *Executor) Run(pb *Playbook) (*RunStatus, error) {
	e.mu.Lock()
	e.mu.Unlock()

	run := &RunStatus{Playbook: pb.Name, Version: pb.Version, State: "running", StartedAt: time.Now()}
	stepResults := map[string]StepStatus{}

	for _, step := range pb.Steps {
		// Check dependencies
		skip := false
		for _, dep := range step.DependsOn {
			if r, ok := stepResults[dep]; ok && r.State != "success" {
				skip = true
				break
			}
		}
		if skip {
			stepResults[step.Name] = StepStatus{StepName: step.Name, State: "skipped"}
			continue
		}

		// Resolve variables
		resolved := map[string]any{}
		for k, v := range step.With {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "${{") && strings.HasSuffix(s, "}}") {
				varName := strings.TrimSpace(s[3 : len(s)-2])
				if val, ok := e.vars[varName]; ok {
					resolved[k] = val
				} else {
					resolved[k] = v
				}
			} else {
				resolved[k] = v
			}
		}
		if len(resolved) == 0 {
			resolved = step.With
		}

		ss := StepStatus{StepName: step.Name, Action: step.Action, State: "running", StartedAt: time.Now()}

		output, err := e.handler(step.Action, resolved)
		ss.EndedAt = time.Now()
		ss.Duration = ss.EndedAt.Sub(ss.StartedAt)
		if err != nil {
			ss.State = "failed"
			ss.Error = err.Error()
		} else {
			ss.State = "success"
			ss.Output = output
		}
		stepResults[step.Name] = ss
		run.Steps = append(run.Steps, ss)

		if ss.State == "failed" {
			break
		}
	}

	allSuccess := true
	for _, ss := range run.Steps {
		if ss.State == "failed" {
			allSuccess = false
			break
		}
	}
	if allSuccess {
		run.State = "success"
	} else {
		run.State = "failed"
	}
	run.EndedAt = time.Now()
	run.Duration = run.EndedAt.Sub(run.StartedAt)

	e.mu.Lock()
	e.history = append(e.history, run)
	if len(e.history) > e.maxHist {
		e.history = e.history[1:]
	}
	e.mu.Unlock()

	return run, nil
}

// History returns recent run statuses.
func (e *Executor) History() []*RunStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*RunStatus, len(e.history))
	copy(out, e.history)
	return out
}

// FormatRunStatus formats a run status for display.
func FormatRunStatus(r *RunStatus) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Playbook: %s v%s [%s] (%v)\n%s\n\n", r.Playbook, r.Version, r.State, r.Duration, strings.Repeat("─", 60))
	for _, ss := range r.Steps {
		icon := "⬜"
		switch ss.State {
		case "success":
			icon = "✅"
		case "failed":
			icon = "🔴"
		case "skipped":
			icon = "⏭️"
		case "running":
			icon = "🟡"
		}
		fmt.Fprintf(&sb, "  %s %-20s %-10s %v", icon, ss.StepName, ss.State, ss.Duration)
		if ss.Error != "" {
			fmt.Fprintf(&sb, "  err=%s", trunc(ss.Error, 60))
		}
		if ss.Output != "" {
			fmt.Fprintf(&sb, "  out=%s", trunc(ss.Output, 60))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// ── Library ────────────────────────────────────────────────

// Library holds reusable playbooks.
type Library struct {
	mu        sync.Mutex
	playbooks map[string]*Playbook
}

// NewLibrary creates a playbook library.
func NewLibrary() *Library { return &Library{playbooks: map[string]*Playbook{}} }

// Register adds a playbook.
func (l *Library) Register(pb *Playbook) { l.mu.Lock(); defer l.mu.Unlock(); l.playbooks[pb.Name] = pb }

// Get returns a playbook by name.
func (l *Library) Get(name string) (*Playbook, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	pb, ok := l.playbooks[name]
	return pb, ok
}

// Names returns all playbook names.
func (l *Library) Names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for n := range l.playbooks {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Validate checks a playbook for structural issues.
func (l *Library) Validate(pb *Playbook) []error {
	var errs []error
	if pb.Name == "" {
		errs = append(errs, fmt.Errorf("name required"))
	}
	if len(pb.Steps) == 0 {
		errs = append(errs, fmt.Errorf("at least one step required"))
	}
	for i, s := range pb.Steps {
		if s.Name == "" {
			errs = append(errs, fmt.Errorf("step %d: name required", i))
		}
		if s.Action == "" {
			errs = append(errs, fmt.Errorf("step %d: action required", i))
		}
	}
	return errs
}
