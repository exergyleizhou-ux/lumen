package runstate

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"lumen/internal/event"
)

type faultStore struct {
	mu         sync.Mutex
	runs       map[string]Run
	events     map[string][]event.Event
	failAppend bool
}

func newFaultStore() *faultStore {
	return &faultStore{runs: make(map[string]Run), events: make(map[string][]event.Event)}
}

func (s *faultStore) CreateRun(r Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return nil
}

func (s *faultStore) UpdateRun(r Run, expected uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs[r.ID].Version != expected {
		return ErrVersionConflict
	}
	s.runs[r.ID] = r
	return nil
}

func (s *faultStore) GetRun(id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	return r, nil
}

func (s *faultStore) AppendEvent(e event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failAppend {
		return errors.New("postgres unavailable")
	}
	s.events[e.RunID] = append(s.events[e.RunID], e)
	return nil
}

func (s *faultStore) Events(runID string, after uint64) ([]event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.events[runID] {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestEventStoreOutageFailsRunAndDoesNotConsumeSequence(t *testing.T) {
	store := newFaultStore()
	mgr := NewManager(store)
	run, err := mgr.Start("session", "code", "outage", "")
	if err != nil {
		t.Fatal(err)
	}
	store.failAppend = true
	mgr.WrapSink(run.ID, event.Discard).Emit(event.Event{Kind: event.ToolResult})
	store.failAppend = false
	mgr.WrapSink(run.ID, event.Discard).Emit(event.Event{Kind: event.Notice})

	events, err := mgr.Events(run.ID, 0)
	if err != nil || len(events) != 1 || events[0].Seq != 1 {
		t.Fatalf("failed append created a sequence gap: %#v %v", events, err)
	}
	finished, err := mgr.Finish(run.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != StatusFailed || finished.Error == "" {
		t.Fatalf("persistence outage became fake success: %#v", finished)
	}
}

func TestCancelAndCompletionRaceHasOneTerminalWinner(t *testing.T) {
	mgr := NewManager(nil)
	run, _ := mgr.Start("session", "code", "race", "")
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, terminalErr := range []error{nil, errors.New("canceled")} {
		wg.Add(1)
		go func(runErr error) {
			defer wg.Done()
			<-start
			_, err := mgr.Finish(run.ID, runErr)
			errs <- err
		}(terminalErr)
	}
	close(start)
	wg.Wait()
	close(errs)
	var success, rejected int
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrInvalidTransition) {
			rejected++
		} else {
			t.Fatalf("unexpected finish error: %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("terminal winners=%d rejected=%d", success, rejected)
	}
}

func TestHundredConcurrentTenantRunsRemainIsolated(t *testing.T) {
	mgr := NewManager(nil)
	const count = 100
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			o := Owner{UserID: fmt.Sprintf("user-%d", i), WorkspaceID: fmt.Sprintf("workspace-%d", i)}
			r, err := mgr.StartOwned(o, fmt.Sprintf("session-%d", i), "code", "parallel", "")
			if err != nil {
				errs <- err
				return
			}
			mgr.WrapSink(r.ID, event.Discard).Emit(event.Event{Kind: event.Text, Text: o.UserID})
			if _, err = mgr.GetOwned(Owner{UserID: "attacker", WorkspaceID: o.WorkspaceID}, r.ID); !errors.Is(err, ErrRunNotFound) {
				errs <- fmt.Errorf("run %s leaked across tenant", r.ID)
				return
			}
			events, err := mgr.EventsOwned(o, r.ID, 0)
			if err != nil || len(events) != 1 || events[0].Text != o.UserID {
				errs <- fmt.Errorf("run %s events crossed: %#v %v", r.ID, events, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
