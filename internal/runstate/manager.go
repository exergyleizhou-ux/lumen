package runstate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"lumen/internal/event"
)

var (
	ErrRunNotFound       = errors.New("run not found")
	ErrInvalidTransition = errors.New("invalid run transition")
	ErrVersionConflict   = errors.New("run version conflict")
)

// Store is the durable persistence boundary for runs and their ordered events.
// A nil Store keeps Manager fully functional in memory.
type Store interface {
	CreateRun(run Run) error
	UpdateRun(run Run, expectedVersion uint64) error
	GetRun(id string) (Run, error)
	AppendEvent(event.Event) error
	Events(runID string, afterSeq uint64) ([]event.Event, error)
}

type managedRun struct {
	mu     sync.Mutex
	run    Run
	seq    uint64
	events []event.Event
}

// Manager owns independent run state machines and per-run event sequences.
type Manager struct {
	mu    sync.RWMutex
	runs  map[string]*managedRun
	store Store
}

func NewManager(store Store) *Manager {
	return &Manager{runs: map[string]*managedRun{}, store: store}
}

func (m *Manager) Start(sessionID, profile, title, parentID string) (Run, error) {
	return m.StartOwned(LocalOwner, sessionID, profile, title, parentID)
}

func (m *Manager) StartOwned(owner Owner, sessionID, profile, title, parentID string) (Run, error) {
	if !owner.Valid() {
		return Run{}, errors.New("run owner required")
	}
	id, err := newRunID()
	if err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	run := Run{
		ID: id, UserID: owner.UserID, WorkspaceID: owner.WorkspaceID, SessionID: sessionID, ParentID: parentID,
		Profile: profile, Title: title, Status: StatusRunning,
		CreatedAt: now, UpdatedAt: now, StartedAt: timePtr(now), Version: 1,
	}
	if m.store != nil {
		if err := m.store.CreateRun(run); err != nil {
			return Run{}, err
		}
	}
	m.mu.Lock()
	m.runs[id] = &managedRun{run: run}
	m.mu.Unlock()
	return run, nil
}

func (m *Manager) GetOwned(owner Owner, runID string) (Run, error) {
	run, err := m.Get(runID)
	if err != nil || run.UserID != owner.UserID || run.WorkspaceID != owner.WorkspaceID {
		return Run{}, fmt.Errorf("%w: %s", ErrRunNotFound, runID)
	}
	return run, nil
}

// ValidateRetryParent permits retry lineage only from an owned terminal Run.
// Callers must still require a new prompt; the original prompt is intentionally
// neither reconstructed nor copied into the child Run.
func (m *Manager) ValidateRetryParent(owner Owner, runID string) (Run, error) {
	run, err := m.GetOwned(owner, runID)
	if err != nil {
		return Run{}, err
	}
	if !run.Status.Terminal() {
		return Run{}, errors.New("parent run must be terminal")
	}
	return run, nil
}

func (m *Manager) EventsOwned(owner Owner, runID string, afterSeq uint64) ([]event.Event, error) {
	if _, err := m.GetOwned(owner, runID); err != nil {
		return nil, err
	}
	return m.Events(runID, afterSeq)
}

func (m *Manager) WrapSink(runID string, inner event.Sink) event.Sink {
	if inner == nil {
		inner = event.Discard
	}
	return &stampingSink{manager: m, runID: runID, inner: inner}
}

func (m *Manager) Finish(runID string, runErr error) (Run, error) {
	mr, err := m.lookup(runID)
	if err != nil {
		return Run{}, err
	}
	mr.mu.Lock()
	defer mr.mu.Unlock()
	status, reason := ClassifyTerminal(runErr)
	if !CanTransition(mr.run.Status, status) {
		return Run{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, mr.run.Status, status)
	}
	now := time.Now().UTC()
	next := mr.run
	next.Status = status
	next.StopReason = reason
	next.UpdatedAt = now
	next.FinishedAt = timePtr(now)
	next.Version++
	if runErr != nil {
		next.Error = runErr.Error()
	}
	if m.store != nil {
		if err := m.store.UpdateRun(next, mr.run.Version); err != nil {
			if errors.Is(err, ErrVersionConflict) {
				fresh, readErr := m.store.GetRun(runID)
				if readErr == nil {
					mr.run = fresh
				}
			}
			return Run{}, err
		}
	}
	mr.run = next
	return next, nil
}

func (m *Manager) Get(runID string) (Run, error) {
	mr, err := m.lookup(runID)
	if err != nil {
		if m.store != nil {
			return m.store.GetRun(runID)
		}
		return Run{}, err
	}
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.run, nil
}

func (m *Manager) Events(runID string, afterSeq uint64) ([]event.Event, error) {
	mr, err := m.lookup(runID)
	if err != nil {
		if m.store != nil {
			return m.store.Events(runID, afterSeq)
		}
		return nil, err
	}
	mr.mu.Lock()
	defer mr.mu.Unlock()
	out := make([]event.Event, 0, len(mr.events))
	for _, ev := range mr.events {
		if ev.Seq > afterSeq {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (m *Manager) lookup(runID string) (*managedRun, error) {
	m.mu.RLock()
	mr := m.runs[runID]
	m.mu.RUnlock()
	if mr == nil {
		return nil, fmt.Errorf("%w: %s", ErrRunNotFound, runID)
	}
	return mr, nil
}

func (m *Manager) stamp(runID string, ev event.Event) (event.Event, error) {
	mr, err := m.lookup(runID)
	if err != nil {
		return event.Event{}, err
	}
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.seq++
	ev.SchemaVersion = 1
	ev.Seq = mr.seq
	ev.RunID = runID
	ev.EventID = fmt.Sprintf("%s:%d", runID, ev.Seq)
	if ev.ToolCallID == "" {
		ev.ToolCallID = ev.Tool.ID
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if m.store != nil {
		if err := m.store.AppendEvent(ev); err != nil {
			return event.Event{}, err
		}
	}
	mr.events = append(mr.events, ev)
	return ev, nil
}

type stampingSink struct {
	manager *Manager
	runID   string
	inner   event.Sink
}

func (s *stampingSink) Emit(ev event.Event) {
	stamped, err := s.manager.stamp(s.runID, ev)
	if err != nil {
		s.inner.Emit(event.Event{
			Kind: event.Notice, Level: event.LevelErr,
			Text: "run event persistence failed: " + err.Error(), Timestamp: time.Now().UTC(),
		})
		return
	}
	s.inner.Emit(stamped)
}

func newRunID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate run id: %w", err)
	}
	return "run_" + hex.EncodeToString(raw[:]), nil
}

func timePtr(v time.Time) *time.Time { return &v }
