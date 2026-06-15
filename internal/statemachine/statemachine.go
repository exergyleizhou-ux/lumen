// Package statemachine provides finite state machines for agent workflow
// orchestration: tool call state tracking, permission escalation flows,
// and session lifecycle states.
package statemachine

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// State is a named state in a state machine.
type State string

// Transition is a named transition between states.
type Transition struct {
	Name      string `json:"name"`
	From      State  `json:"from"`
	To        State  `json:"to"`
	Guard     func(ctx *Context) bool `json:"-"`
	OnEnter   func(ctx *Context)      `json:"-"`
	OnExit    func(ctx *Context)      `json:"-"`
}

// Context carries mutable data through state transitions.
type Context struct {
	mu       sync.RWMutex
	data     map[string]any
	history  []StateRecord
	started  time.Time
}

// StateRecord is one state change in history.
type StateRecord struct {
	From      State     `json:"from"`
	To        State     `json:"to"`
	Transition string   `json:"transition"`
	Timestamp  time.Time `json:"timestamp"`
}

// NewContext creates a state machine context.
func NewContext() *Context {
	return &Context{data: map[string]any{}, started: time.Now()}
}

// Set stores a value in the context.
func (c *Context) Set(key string, value any) {
	c.mu.Lock(); defer c.mu.Unlock()
	c.data[key] = value
}

// Get retrieves a value from the context.
func (c *Context) Get(key string) (any, bool) {
	c.mu.RLock(); defer c.mu.RUnlock()
	v, ok := c.data[key]; return v, ok
}

// History returns all state transitions.
func (c *Context) History() []StateRecord {
	c.mu.RLock(); defer c.mu.RUnlock()
	out := make([]StateRecord, len(c.history))
	copy(out, c.history); return out
}

// Machine is a finite state machine with named states and transitions.
type Machine struct {
	mu           sync.RWMutex
	states       map[State]bool
	transitions  []Transition
	current      State
	initial      State
	finalStates  map[State]bool
	name         string
}

// NewMachine creates a state machine.
func NewMachine(name string, initial State) *Machine {
	return &Machine{
		name: name, initial: initial, current: initial,
		states: map[State]bool{initial: true},
		finalStates: map[State]bool{},
	}
}

// AddState registers a new state.
func (m *Machine) AddState(s State) *Machine {
	m.mu.Lock(); defer m.mu.Unlock()
	m.states[s] = true; return m
}

// AddFinalState registers a final (terminal) state.
func (m *Machine) AddFinalState(s State) *Machine {
	m.mu.Lock(); defer m.mu.Unlock()
	m.states[s] = true; m.finalStates[s] = true; return m
}

// AddTransition registers a transition between states.
func (m *Machine) AddTransition(name string, from, to State, guard func(*Context) bool) *Machine {
	m.mu.Lock(); defer m.mu.Unlock()
	m.states[from] = true; m.states[to] = true
	m.transitions = append(m.transitions, Transition{Name: name, From: from, To: to, Guard: guard})
	return m
}

// Current returns the current state.
func (m *Machine) Current() State {
	m.mu.RLock(); defer m.mu.RUnlock(); return m.current
}

// IsFinal reports whether the current state is terminal.
func (m *Machine) IsFinal() bool {
	m.mu.RLock(); defer m.mu.RUnlock()
	return m.finalStates[m.current]
}

// AvailableTransitions returns transitions that can fire from the current state.
func (m *Machine) AvailableTransitions(ctx *Context) []Transition {
	m.mu.RLock(); defer m.mu.RUnlock()
	var available []Transition
	for _, t := range m.transitions {
		if t.From == m.current {
			if t.Guard == nil || t.Guard(ctx) {
				available = append(available, t)
			}
		}
	}
	return available
}

// TransitionTo attempts to move to a new state via the named transition.
func (m *Machine) TransitionTo(name string, ctx *Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.transitions {
		if t.Name == name && t.From == m.current {
			if t.Guard != nil && !t.Guard(ctx) {
				return fmt.Errorf("guard prevented transition %s from %s to %s", name, m.current, t.To)
			}
			old := m.current
			m.current = t.To
			ctx.mu.Lock()
			ctx.history = append(ctx.history, StateRecord{
				From: old, To: t.To, Transition: name, Timestamp: time.Now(),
			})
			ctx.mu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("no transition %q from state %q", name, m.current)
}

// CanTransitionTo reports whether a transition is possible.
func (m *Machine) CanTransitionTo(name string, ctx *Context) bool {
	m.mu.RLock(); defer m.mu.RUnlock()
	for _, t := range m.transitions {
		if t.Name == name && t.From == m.current {
			return t.Guard == nil || t.Guard(ctx)
		}
	}
	return false
}

// Reset returns the machine to its initial state.
func (m *Machine) Reset() {
	m.mu.Lock(); defer m.mu.Unlock()
	m.current = m.initial
}

// ── Pre-built state machines for agent workflows ──────────

// ToolCallMachine tracks the lifecycle of a tool call.
func ToolCallMachine() *Machine {
	return NewMachine("tool_call", "idle").
		AddState("idle").AddState("pending_approval").AddState("running").
		AddFinalState("completed").AddFinalState("failed").AddFinalState("blocked").
		AddTransition("request", "idle", "pending_approval", nil).
		AddTransition("approve", "pending_approval", "running", nil).
		AddTransition("deny", "pending_approval", "blocked", nil).
		AddTransition("succeed", "running", "completed", nil).
		AddTransition("fail", "running", "failed", nil).
		AddTransition("timeout", "running", "failed", nil)
}

// SessionMachine tracks the lifecycle of an agent session.
func SessionMachine() *Machine {
	return NewMachine("session", "created").
		AddState("created").AddState("initializing").AddState("ready").
		AddState("running").AddState("compacting").AddState("paused").
		AddFinalState("completed").AddFinalState("error").AddFinalState("cancelled").
		AddTransition("init", "created", "initializing", nil).
		AddTransition("ready", "initializing", "ready", nil).
		AddTransition("run", "ready", "running", nil).
		AddTransition("compact", "running", "compacting", nil).
		AddTransition("resume", "compacting", "running", nil).
		AddTransition("pause", "running", "paused", nil).
		AddTransition("resume_pause", "paused", "running", nil).
		AddTransition("finish", "running", "completed", nil).
		AddTransition("error", "running", "error", nil)
}

// FormatHistory formats state transition history.
func FormatHistory(records []StateRecord) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "State history (%d transitions):\n", len(records))
	byTime := make([]StateRecord, len(records))
	copy(byTime, records)
	sort.Slice(byTime, func(i, j int) bool { return byTime[i].Timestamp.Before(byTime[j].Timestamp) })
	for _, r := range byTime {
		fmt.Fprintf(&sb, "  %s: %s → %s (%s)\n",
			r.Timestamp.Format("15:04:05"), r.From, r.To, r.Transition)
	}
	return sb.String()
}

// ── Workflow Orchestrator ─────────────────────────────────

// Workflow defines a sequence of states with branching logic.
type Workflow struct {
	Name    string           `json:"name"`
	Steps   []WorkflowStep   `json:"steps"`
	onComplete func(*Context) `json:"-"`
}

// WorkflowStep is one step in a workflow.
type WorkflowStep struct {
	Name      string                 `json:"name"`
	Condition func(*Context) bool     `json:"-"`
	Action    func(*Context) error    `json:"-"`
	OnError   func(*Context, error)   `json:"-"`
	Retries   int                    `json:"retries"`
}

// NewWorkflow creates a workflow.
func NewWorkflow(name string) *Workflow {
	return &Workflow{Name: name}
}

// AddStep appends a workflow step.
func (w *Workflow) AddStep(step WorkflowStep) *Workflow {
	w.Steps = append(w.Steps, step); return w
}

// Execute runs the workflow.
func (w *Workflow) Execute(ctx *Context) error {
	for _, step := range w.Steps {
		if step.Condition != nil && !step.Condition(ctx) {
			continue
		}
		var err error
		for attempt := 0; attempt <= step.Retries; attempt++ {
			err = step.Action(ctx)
			if err == nil { break }
		}
		if err != nil && step.OnError != nil {
			step.OnError(ctx, err)
			return fmt.Errorf("workflow %s step %s: %w", w.Name, step.Name, err)
		}
	}
	if w.onComplete != nil { w.onComplete(ctx) }
	return nil
}

// OnComplete registers a completion callback.
func (w *Workflow) OnComplete(fn func(*Context)) *Workflow {
	w.onComplete = fn; return w
}
