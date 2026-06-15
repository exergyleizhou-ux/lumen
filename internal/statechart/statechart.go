// Package statechart implements hierarchical state machines (Harel statecharts)
// with nested states, enter/exit actions, transition guards, history pseudo-states,
// and parallel (orthogonal) regions. It supports DOT-format visualization via
// FormatStateChart.
//
// Usage:
//
//	sm := statechart.NewStateMachine("atm")
//	idle := sm.Root().AddChild("idle")
//	active := sm.Root().AddChild("active")
//	idle.AddTransition("CARD_INSERTED", active)
//	sm.Start()
//	sm.Send("CARD_INSERTED")
//	fmt.Println(sm.CurrentState().Name) // "active"
package statechart

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// State kind
// ---------------------------------------------------------------------------

// StateKind describes the kind of a state node.
type StateKind int

const (
	KindBasic       StateKind = iota // plain state
	KindComposite                    // has nested (child) states
	KindParallel                     // has orthogonal regions
	KindInitial                      // initial pseudostate
	KindFinal                        // terminal state
	KindHistory                      // shallow history pseudostate
	KindDeepHistory                  // deep history pseudostate
)

func (k StateKind) String() string {
	switch k {
	case KindBasic:
		return "basic"
	case KindComposite:
		return "composite"
	case KindParallel:
		return "parallel"
	case KindInitial:
		return "initial"
	case KindFinal:
		return "final"
	case KindHistory:
		return "history"
	case KindDeepHistory:
		return "deep_history"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Guard
// ---------------------------------------------------------------------------

// Guard is a condition that must be satisfied for a transition to fire.
// Evaluate receives the current state machine context and returns whether
// the guard permits the transition.
type Guard interface {
	Name() string
	Evaluate(sm *StateMachine) bool
}

// FuncGuard wraps a function as a Guard.
type FuncGuard struct {
	name string
	fn   func(sm *StateMachine) bool
}

// NewGuard creates a guard from a named function.
func NewGuard(name string, fn func(sm *StateMachine) bool) *FuncGuard {
	return &FuncGuard{name: name, fn: fn}
}

func (g FuncGuard) Name() string                   { return g.name }
func (g FuncGuard) Evaluate(sm *StateMachine) bool { return g.fn(sm) }
func (g FuncGuard) String() string                 { return g.name }

// ---------------------------------------------------------------------------
// Action
// ---------------------------------------------------------------------------

// Action is a side-effect executed on state entry, exit, or transition.
type Action func(sm *StateMachine)

// ---------------------------------------------------------------------------
// Transition
// ---------------------------------------------------------------------------

// Transition is a directed edge between two states, labeled with an event
// name and optional guard and actions.
type Transition struct {
	Event   string
	Source  *State
	Target  *State
	Guard   Guard
	Actions []Action
}

// String returns a brief description.
func (t *Transition) String() string {
	g := ""
	if t.Guard != nil {
		g = " [" + t.Guard.Name() + "]"
	}
	return fmt.Sprintf("%s --%s%s--> %s", t.Source.Name, t.Event, g, t.Target.Name)
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

// State is a node in the state hierarchy. A composite state has children;
// a parallel state has orthogonal regions (each region is a child state
// active simultaneously).
type State struct {
	Name        string
	Kind        StateKind
	Parent      *State
	Children    []*State
	Initial     *State // for composite states: which child starts active
	Transitions []*Transition

	// Actions executed when entering this state.
	OnEntry []Action
	// Actions executed when exiting this state.
	OnExit []Action

	// History state remembers which child was active (for history pseudostates).
	historyChild *State
}

// NewState creates a state with the given name and kind.
func NewState(name string, kind StateKind) *State {
	return &State{Name: name, Kind: kind}
}

// AddChild adds a child state and sets parent linkage.
func (s *State) AddChild(child *State) *State {
	child.Parent = s
	s.Children = append(s.Children, child)
	if s.Initial == nil && (child.Kind == KindBasic || child.Kind == KindComposite || child.Kind == KindParallel) {
		s.Initial = child
	}
	return child
}

// AddTransition creates a transition from this state to target on event.
func (s *State) AddTransition(event string, target *State) *Transition {
	t := &Transition{
		Event:  event,
		Source: s,
		Target: target,
	}
	s.Transitions = append(s.Transitions, t)
	return t
}

// AddTransitionWithGuard creates a guarded transition.
func (s *State) AddTransitionWithGuard(event string, target *State, guard Guard) *Transition {
	t := s.AddTransition(event, target)
	t.Guard = guard
	return t
}

// SetInitial sets the initial child for a composite state.
func (s *State) SetInitial(child *State) { s.Initial = child }

// IsActive returns true if this state is among the machine's current states.
func (s *State) IsActive(sm *StateMachine) bool {
	for _, cs := range sm.currentStates {
		if cs == s {
			return true
		}
	}
	return false
}

// FindChild finds a direct child by name.
func (s *State) FindChild(name string) *State {
	for _, c := range s.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// FindDescendant recursively searches for a state by name.
func (s *State) FindDescendant(name string) *State {
	if s.Name == name {
		return s
	}
	for _, c := range s.Children {
		if found := c.FindDescendant(name); found != nil {
			return found
		}
	}
	return nil
}

// Ancestors returns all ancestor states from root to parent.
func (s *State) Ancestors() []*State {
	var result []*State
	for p := s.Parent; p != nil; p = p.Parent {
		result = append([]*State{p}, result...)
	}
	return result
}

// Depth returns the nesting depth (0 for root).
func (s *State) Depth() int {
	d := 0
	for p := s.Parent; p != nil; p = p.Parent {
		d++
	}
	return d
}

// ---------------------------------------------------------------------------
// StateMachine
// ---------------------------------------------------------------------------

// StateMachine is the top-level runtime for a statechart.
type StateMachine struct {
	mu            sync.Mutex
	Name          string
	root          *State
	currentStates []*State // all currently active leaf states
	running       bool
	context       map[string]interface{}
}

// NewStateMachine creates a machine with a root composite state.
func NewStateMachine(name string) *StateMachine {
	root := NewState(name, KindComposite)
	sm := &StateMachine{
		Name:    name,
		root:    root,
		context: make(map[string]interface{}),
	}
	return sm
}

// Root returns the root state.
func (sm *StateMachine) Root() *State { return sm.root }

// Context returns the shared context map.
func (sm *StateMachine) Context() map[string]interface{} { return sm.context }

// Set stores a value in the machine context.
func (sm *StateMachine) Set(key string, value interface{}) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.context[key] = value
}

// Get retrieves a value from the machine context.
func (sm *StateMachine) Get(key string) interface{} {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.context[key]
}

// Start initializes the state machine by entering the root state recursively.
func (sm *StateMachine) Start() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.running {
		return errors.New("statechart: already started")
	}

	sm.running = true
	return sm.enterState(sm.root, true)
}

// CurrentState returns the current leaf-level state names.
func (sm *StateMachine) CurrentState() []*State {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	out := make([]*State, len(sm.currentStates))
	copy(out, sm.currentStates)
	return out
}

// IsRunning returns whether the machine has been started.
func (sm *StateMachine) IsRunning() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.running
}

// Send dispatches an event to the state machine. The event is offered to
// the active states, starting from the innermost (most deeply nested) states.
// If a transition fires, exit/entry actions are executed for the affected states.
func (sm *StateMachine) Send(event string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.running {
		return errors.New("statechart: not started")
	}

	// Collect active states sorted by depth (deepest first).
	active := make([]*State, len(sm.currentStates))
	copy(active, sm.currentStates)
	sort.Slice(active, func(i, j int) bool {
		return active[i].Depth() > active[j].Depth()
	})

	// Try each active state and its ancestors for a matching transition.
	for _, st := range active {
		for s := st; s != nil; s = s.Parent {
			for _, tr := range s.Transitions {
				if tr.Event != event {
					continue
				}
				if tr.Guard != nil && !tr.Guard.Evaluate(sm) {
					continue
				}
				return sm.fireTransition(tr)
			}
		}
	}

	return fmt.Errorf("statechart: no transition for event %q", event)
}

func (sm *StateMachine) fireTransition(tr *Transition) error {
	// Determine the LCA (least common ancestor) of source and target.
	lca := findLCA(tr.Source, tr.Target)

	// 1. Exit from source up to (but not including) LCA.
	exitSet := collectUpTo(tr.Source, lca)
	for _, s := range exitSet {
		if err := sm.exitState(s); err != nil {
			return err
		}
	}

	// 2. Execute transition actions.
	for _, a := range tr.Actions {
		a(sm)
	}

	// 3. Enter from just below LCA down to target.
	enterSet := collectPathFrom(lca, tr.Target)
	for _, s := range enterSet {
		if err := sm.enterState(s, true); err != nil {
			return err
		}
	}

	return nil
}

// enterState enters a state and its initial descendants.
func (sm *StateMachine) enterState(s *State, recurse bool) error {
	// Execute entry actions.
	for _, a := range s.OnEntry {
		a(sm)
	}

	switch s.Kind {
	case KindBasic, KindFinal:
		sm.currentStates = append(sm.currentStates, s)
	case KindComposite:
		if !recurse {
			sm.currentStates = append(sm.currentStates, s)
			return nil
		}
		if s.Initial != nil {
			return sm.enterState(s.Initial, true)
		}
		sm.currentStates = append(sm.currentStates, s)
	case KindParallel:
		for _, child := range s.Children {
			if err := sm.enterState(child, true); err != nil {
				return err
			}
		}
	case KindHistory:
		// Shallow history: re-enter the last active child.
		if s.historyChild != nil {
			return sm.enterState(s.historyChild, true)
		}
		if s.Parent != nil && s.Parent.Initial != nil {
			return sm.enterState(s.Parent.Initial, true)
		}
	case KindDeepHistory:
		if s.historyChild != nil {
			return sm.enterState(s.historyChild, true)
		}
		if s.Parent != nil && s.Parent.Initial != nil {
			return sm.enterState(s.Parent.Initial, true)
		}
	}

	return nil
}

// exitState exits a state, running its exit actions and removing from active.
func (sm *StateMachine) exitState(s *State) error {
	// Execute exit actions.
	for _, a := range s.OnExit {
		a(sm)
	}

	// Record history for parent if applicable.
	if s.Parent != nil {
		for _, c := range s.Parent.Children {
			if c.Kind == KindHistory || c.Kind == KindDeepHistory {
				c.historyChild = s
			}
		}
	}

	// Remove from current states.
	for i, cs := range sm.currentStates {
		if cs == s {
			sm.currentStates = append(sm.currentStates[:i], sm.currentStates[i+1:]...)
			break
		}
	}

	return nil
}

// findLCA finds the least common ancestor of a and b.
func findLCA(a, b *State) *State {
	aAnc := make(map[*State]bool)
	for s := a; s != nil; s = s.Parent {
		aAnc[s] = true
	}
	for s := b; s != nil; s = s.Parent {
		if aAnc[s] {
			return s
		}
	}
	return nil
}

// collectUpTo collects states from s upward, stopping before stop.
func collectUpTo(s, stop *State) []*State {
	var result []*State
	for cur := s; cur != stop && cur != nil; cur = cur.Parent {
		result = append(result, cur)
	}
	return result
}

// collectPathFrom collects states from stop (exclusive) down to target.
func collectPathFrom(lca, target *State) []*State {
	// Build path from target up to lca, then reverse.
	var path []*State
	for s := target; s != lca && s != nil; s = s.Parent {
		path = append([]*State{s}, path...)
	}
	return path
}

// ---------------------------------------------------------------------------
// DOT formatting
// ---------------------------------------------------------------------------

// FormatStateChartOptions controls DOT output.
type FormatStateChartOptions struct {
	ShowActions bool
	ShowGuards  bool
	RankDir     string // "TB" (top-bottom) or "LR" (left-right)
}

// DefaultFormatOptions returns sensible defaults.
func DefaultFormatOptions() FormatStateChartOptions {
	return FormatStateChartOptions{
		ShowActions: true,
		ShowGuards:  true,
		RankDir:     "TB",
	}
}

// FormatStateChart produces a DOT digraph representation of the statechart.
func FormatStateChart(sm *StateMachine, opts FormatStateChartOptions) string {
	var sb strings.Builder
	sb.WriteString("digraph ")
	sb.WriteString(quoteDOT(sm.Name))
	sb.WriteString(" {\n")
	sb.WriteString(fmt.Sprintf("  rankdir=%s;\n", opts.RankDir))
	sb.WriteString("  node [shape=box, style=rounded];\n\n")

	// Collect all states and transitions.
	states := collectAllStates(sm.root)
	transitions := collectAllTransitions(sm.root)

	// Write state nodes.
	for _, s := range states {
		sb.WriteString("  ")
		sb.WriteString(quoteDOT(s.Name))
		shape := "box"
		switch s.Kind {
		case KindInitial:
			shape = "circle"
		case KindFinal:
			shape = "doublecircle"
		case KindHistory, KindDeepHistory:
			shape = "circle"
		case KindParallel:
			shape = "box, style=\"rounded,dashed\""
		case KindComposite:
			shape = "box, style=rounded"
		}
		label := s.Name
		if opts.ShowActions && (len(s.OnEntry) > 0 || len(s.OnExit) > 0) {
			parts := []string{s.Name}
			if len(s.OnEntry) > 0 {
				parts = append(parts, "entry/")
			}
			if len(s.OnExit) > 0 {
				parts = append(parts, "exit/")
			}
			label = strings.Join(parts, "\\n")
		}
		sb.WriteString(fmt.Sprintf("[label=%q, shape=%s];\n", label, shape))
	}

	// Write cluster subgraphs for composite states.
	for _, s := range states {
		if s.Kind == KindComposite && len(s.Children) > 0 {
			sb.WriteString(fmt.Sprintf("  subgraph cluster_%s {\n", sanitizeID(s.Name)))
			sb.WriteString(fmt.Sprintf("    label=%q;\n", s.Name))
			for _, c := range s.Children {
				sb.WriteString(fmt.Sprintf("    %s;\n", quoteDOT(c.Name)))
			}
			sb.WriteString("  }\n")
		}
	}

	// Write transitions.
	for _, tr := range transitions {
		sb.WriteString("  ")
		sb.WriteString(quoteDOT(tr.Source.Name))
		sb.WriteString(" -> ")
		sb.WriteString(quoteDOT(tr.Target.Name))
		label := tr.Event
		if opts.ShowGuards && tr.Guard != nil {
			label += " [" + tr.Guard.Name() + "]"
		}
		sb.WriteString(fmt.Sprintf(" [label=%q];\n", label))
	}

	sb.WriteString("}\n")
	return sb.String()
}

func collectAllStates(s *State) []*State {
	var result []*State
	collectStates(s, &result)
	return result
}

func collectStates(s *State, out *[]*State) {
	*out = append(*out, s)
	for _, c := range s.Children {
		collectStates(c, out)
	}
}

func collectAllTransitions(s *State) []*Transition {
	var result []*Transition
	collectTransitions(s, &result)
	return result
}

func collectTransitions(s *State, out *[]*Transition) {
	*out = append(*out, s.Transitions...)
	for _, c := range s.Children {
		collectTransitions(c, out)
	}
}

func quoteDOT(s string) string {
	return fmt.Sprintf("%q", s)
}

func sanitizeID(s string) string {
	return strings.ReplaceAll(s, " ", "_")
}

// ---------------------------------------------------------------------------
// Convenience constructors
// ---------------------------------------------------------------------------

// Basic creates a basic state.
func Basic(name string) *State { return NewState(name, KindBasic) }

// Composite creates a composite state.
func Composite(name string) *State { return NewState(name, KindComposite) }

// Parallel creates a parallel (orthogonal) state.
func Parallel(name string) *State { return NewState(name, KindParallel) }

// Initial creates an initial pseudostate.
func Initial() *State { return NewState("", KindInitial) }

// Final creates a final state.
func Final(name string) *State { return NewState(name, KindFinal) }

// History creates a shallow history pseudostate.
func History() *State { return NewState("H", KindHistory) }

// DeepHistory creates a deep history pseudostate.
func DeepHistory() *State { return NewState("H*", KindDeepHistory) }

// ---------------------------------------------------------------------------
// ATM example builder
// ---------------------------------------------------------------------------

// BuildATM creates a simple ATM state machine:
// idle -> (card inserted) -> active -> (valid pin) -> menu -> (select) -> transaction -> done -> idle
func BuildATM() *StateMachine {
	sm := NewStateMachine("ATM")

	idle := sm.Root().AddChild(Composite("idle"))
	active := sm.Root().AddChild(Composite("active"))
	menu := active.AddChild(Composite("menu"))
	transaction := active.AddChild(Composite("transaction"))

	// idle substates
	cardIn := idle.AddChild(Basic("card_reader"))
	pinEntry := idle.AddChild(Basic("pin_entry"))

	// menu substates
	menu.AddChild(Basic("balance_inquiry"))
	menu.AddChild(Basic("withdrawal"))
	menu.AddChild(Basic("deposit"))

	// transaction substates
	transaction.AddChild(Basic("processing"))
	done := transaction.AddChild(Basic("done"))

	// Transitions
	idle.AddTransition("CARD_INSERTED", active)
	active.AddTransition("VALID_PIN", menu)
	menu.AddTransition("SELECT", transaction)
	transaction.AddTransition("COMPLETE", idle)

	// idle internal
	cardIn.AddTransition("CARD_READ", pinEntry)

	// done -> idle
	done.AddTransition("RETURN_CARD", idle)

	return sm
}
