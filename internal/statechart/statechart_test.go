package statechart

import (
	"strings"
	"testing"
)

func TestStateMachine_Creation(t *testing.T) {
	sm := NewStateMachine("test")
	if sm == nil || sm.Root() == nil {
		t.Fatal("nil")
	}
	if sm.Root().Name != "test" {
		t.Fatalf("name: %s", sm.Root().Name)
	}
}

func TestStateMachine_Start(t *testing.T) {
	sm := NewStateMachine("sm")
	a := sm.Root().AddChild(Basic("a"))
	b := sm.Root().AddChild(Basic("b"))
	sm.Root().SetInitial(a)

	if err := sm.Start(); err != nil {
		t.Fatal(err)
	}
	if !a.IsActive(sm) {
		t.Fatal("a should be active")
	}
	if b.IsActive(sm) {
		t.Fatal("b should not be active")
	}
}

func TestStateMachine_Send(t *testing.T) {
	sm := NewStateMachine("sm")
	a := sm.Root().AddChild(Basic("a"))
	b := sm.Root().AddChild(Basic("b"))
	sm.Root().SetInitial(a)
	a.AddTransition("go", b)

	sm.Start()
	if err := sm.Send("go"); err != nil {
		t.Fatal(err)
	}
	if !b.IsActive(sm) {
		t.Fatal("b should be active after transition")
	}
	if a.IsActive(sm) {
		t.Fatal("a should be inactive")
	}
}

func TestStateMachine_GuardedTransition(t *testing.T) {
	sm := NewStateMachine("sm")
	a := sm.Root().AddChild(Basic("a"))
	b := sm.Root().AddChild(Basic("b"))
	sm.Root().SetInitial(a)

	allowed := false
	guard := NewGuard("check", func(sm *StateMachine) bool { return allowed })
	a.AddTransitionWithGuard("go", b, guard)

	sm.Start()
	// Should fail — guard returns false.
	if err := sm.Send("go"); err == nil {
		t.Fatal("expected error for blocked transition")
	}
	if !a.IsActive(sm) {
		t.Fatal("a should still be active")
	}

	// Allow the guard.
	allowed = true
	if err := sm.Send("go"); err != nil {
		t.Fatal(err)
	}
	if !b.IsActive(sm) {
		t.Fatal("b should be active after allowed transition")
	}
}

func TestStateMachine_EntryExitActions(t *testing.T) {
	sm := NewStateMachine("sm")
	var log []string

	a := sm.Root().AddChild(Basic("a"))
	b := sm.Root().AddChild(Basic("b"))
	sm.Root().SetInitial(a)

	a.OnEntry = append(a.OnEntry, func(sm *StateMachine) { log = append(log, "enter_a") })
	a.OnExit = append(a.OnExit, func(sm *StateMachine) { log = append(log, "exit_a") })
	b.OnEntry = append(b.OnEntry, func(sm *StateMachine) { log = append(log, "enter_b") })
	b.OnExit = append(b.OnExit, func(sm *StateMachine) { log = append(log, "exit_b") })

	a.AddTransition("go", b)

	sm.Start()

	// Clear log (start triggers enter_a).
	log = nil

	sm.Send("go")

	if len(log) != 2 || log[0] != "exit_a" || log[1] != "enter_b" {
		t.Fatalf("expected exit_a enter_b, got %v", log)
	}
}

func TestStateMachine_NestedStates(t *testing.T) {
	sm := NewStateMachine("sm")
	outer := sm.Root().AddChild(Composite("outer"))
	innerA := outer.AddChild(Basic("inner_a"))
	innerB := outer.AddChild(Basic("inner_b"))
	outer.SetInitial(innerA)

	sm.Start()

	if !innerA.IsActive(sm) {
		t.Fatal("inner_a should be active")
	}

	innerA.AddTransition("switch", innerB)
	sm.Send("switch")

	if !innerB.IsActive(sm) {
		t.Fatal("inner_b should be active")
	}
}

func TestStateMachine_ParallelRegions(t *testing.T) {
	sm := NewStateMachine("sm")
	p := sm.Root().AddChild(Parallel("parallel"))
	r1 := p.AddChild(Composite("region1"))
	r2 := p.AddChild(Composite("region2"))

	a1 := r1.AddChild(Basic("a1"))
	a2 := r2.AddChild(Basic("a2"))

	r1.SetInitial(a1)
	r2.SetInitial(a2)

	sm.Start()

	states := sm.CurrentState()
	if len(states) != 2 {
		t.Fatalf("expected 2 active states, got %d", len(states))
	}
}

func TestStateMachine_Context(t *testing.T) {
	sm := NewStateMachine("sm")
	sm.Set("counter", 42)
	if sm.Get("counter") != 42 {
		t.Fatal("context get/set failed")
	}
}

func TestStateMachine_NotFound(t *testing.T) {
	sm := NewStateMachine("sm")
	a := sm.Root().AddChild(Basic("a"))
	sm.Root().SetInitial(a)
	sm.Start()

	if err := sm.Send("unknown"); err == nil {
		t.Fatal("expected error for unknown event")
	}
}

func TestFindChild(t *testing.T) {
	sm := NewStateMachine("sm")
	a := sm.Root().AddChild(Basic("a"))
	if sm.Root().FindChild("a") != a {
		t.Fatal("find child failed")
	}
	if sm.Root().FindChild("nonexistent") != nil {
		t.Fatal("should be nil")
	}
}

func TestFindDescendant(t *testing.T) {
	sm := NewStateMachine("sm")
	outer := sm.Root().AddChild(Composite("outer"))
	inner := outer.AddChild(Basic("inner"))
	if sm.Root().FindDescendant("inner") != inner {
		t.Fatal("find descendant failed")
	}
}

func TestAncestors(t *testing.T) {
	sm := NewStateMachine("sm")
	outer := sm.Root().AddChild(Composite("outer"))
	inner := outer.AddChild(Basic("inner"))
	anc := inner.Ancestors()
	if len(anc) != 2 || anc[0] != sm.Root() || anc[1] != outer {
		t.Fatalf("ancestors: %v", anc)
	}
}

func TestFormatStateChart(t *testing.T) {
	sm := BuildATM()
	dot := FormatStateChart(sm, DefaultFormatOptions())
	if !strings.Contains(dot, "digraph") {
		t.Fatal("not a digraph")
	}
	if !strings.Contains(dot, "ATM") {
		t.Fatal("missing name")
	}
	if !strings.Contains(dot, "CARD_INSERTED") {
		t.Fatal("missing transition label")
	}
}

func TestBuildATM(t *testing.T) {
	sm := BuildATM()
	sm.Start()

	states := sm.CurrentState()
	// Should be in the innermost leaf of idle (card_reader or pin_entry).
	if len(states) == 0 {
		t.Fatal("no active states")
	}

	// Navigate through ATM scenario.
	sm.Send("CARD_INSERTED")
	sm.Send("VALID_PIN")
	sm.Send("SELECT")
	sm.Send("COMPLETE")

	// Should be back in idle.
	states = sm.CurrentState()
	found := false
	for _, s := range states {
		if s.Name == "card_reader" || s.Name == "pin_entry" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected back in idle, got %v", stateNames(states))
	}
}

func stateNames(states []*State) []string {
	names := make([]string, len(states))
	for i, s := range states {
		names[i] = s.Name
	}
	return names
}

func TestStateKind_String(t *testing.T) {
	if KindBasic.String() != "basic" { t.Fatal("bad") }
	if KindComposite.String() != "composite" { t.Fatal("bad") }
	if KindParallel.String() != "parallel" { t.Fatal("bad") }
}
