package runstate

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"lumen/internal/agent"
	"lumen/internal/event"
)

type collectingSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *collectingSink) Emit(ev event.Event) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

func (s *collectingSink) snapshot() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]event.Event, len(s.events))
	copy(out, s.events)
	return out
}

func TestStampingSinkAddsOrderedRunMetadata(t *testing.T) {
	mgr := NewManager(nil)
	run, err := mgr.Start("session-1", "code", "fix parser", "")
	if err != nil {
		t.Fatal(err)
	}
	inner := &collectingSink{}
	sink := mgr.WrapSink(run.ID, inner)
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call-1", Name: "read_file"}})
	sink.Emit(event.Event{Kind: event.TurnDone})

	got := inner.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d events", len(got))
	}
	if got[0].SchemaVersion != 1 || got[0].RunID != run.ID || got[0].Seq != 1 {
		t.Fatalf("first event not stamped: %#v", got[0])
	}
	if got[1].Seq != 2 || got[2].Seq != 3 {
		t.Fatalf("non-monotonic sequence: %#v", got)
	}
	if got[1].EventID == got[2].EventID || got[0].EventID == "" {
		t.Fatal("event ids must be present and unique")
	}
	if got[1].ToolCallID != "call-1" {
		t.Fatalf("tool call id not copied: %#v", got[1])
	}
}

func TestStampingSinkConcurrentSequenceIsContiguous(t *testing.T) {
	mgr := NewManager(nil)
	run, err := mgr.Start("session-1", "code", "parallel reads", "")
	if err != nil {
		t.Fatal(err)
	}
	inner := &collectingSink{}
	sink := mgr.WrapSink(run.ID, inner)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sink.Emit(event.Event{Kind: event.ToolProgress})
		}()
	}
	wg.Wait()

	got := inner.snapshot()
	if len(got) != 32 {
		t.Fatalf("got %d events", len(got))
	}
	seqs := make([]int, len(got))
	ids := map[string]bool{}
	for i, ev := range got {
		seqs[i] = int(ev.Seq)
		if ids[ev.EventID] {
			t.Fatalf("duplicate event id %q", ev.EventID)
		}
		ids[ev.EventID] = true
	}
	sort.Ints(seqs)
	for i, seq := range seqs {
		if seq != i+1 {
			t.Fatalf("sequence gap at %d: %v", i, seqs)
		}
	}
}

func TestRunSequenceCountersAreIndependent(t *testing.T) {
	mgr := NewManager(nil)
	runA, _ := mgr.Start("a", "code", "A", "")
	runB, _ := mgr.Start("b", "lab", "B", "")
	innerA, innerB := &collectingSink{}, &collectingSink{}
	mgr.WrapSink(runA.ID, innerA).Emit(event.Event{Kind: event.Text})
	mgr.WrapSink(runB.ID, innerB).Emit(event.Event{Kind: event.Text})
	if innerA.snapshot()[0].Seq != 1 || innerB.snapshot()[0].Seq != 1 {
		t.Fatalf("run counters leaked: A=%v B=%v", innerA.snapshot(), innerB.snapshot())
	}
}

func TestFinishClassifiesTerminalState(t *testing.T) {
	mgr := NewManager(nil)
	run, _ := mgr.Start("s", "code", "loop", "")
	finished, err := mgr.Finish(run.ID, &agent.MaxStepsError{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusExhausted || finished.StopReason != "max_steps" || finished.FinishedAt == nil {
		t.Fatalf("unexpected terminal run: %#v", finished)
	}
	if _, err := mgr.Finish(run.ID, context.Canceled); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal run accepted second finish: %v", err)
	}
}
