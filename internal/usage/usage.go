// Package usage captures tenant-scoped, idempotent model usage records.
package usage

import (
	"errors"
	"math"
	"sync"
	"time"

	"lumen/internal/event"
	"lumen/internal/runstate"
)

var ErrDuplicate = errors.New("usage event already recorded")

// Record is the durable billing boundary. EstimatedCostMicros is an integer
// number of millionths of the pricing currency, avoiding floating point in
// persistence and quota accounting.
type Record struct {
	EventID             string    `json:"event_id"`
	RunID               string    `json:"run_id"`
	UserID              string    `json:"user_id"`
	WorkspaceID         string    `json:"workspace_id"`
	Provider            string    `json:"provider"`
	Model               string    `json:"model"`
	InputTokens         int       `json:"input_tokens"`
	OutputTokens        int       `json:"output_tokens"`
	CacheHitTokens      int       `json:"cache_hit_tokens"`
	CacheMissTokens     int       `json:"cache_miss_tokens"`
	EstimatedCostMicros int64     `json:"estimated_cost_micros"`
	CreatedAt           time.Time `json:"created_at"`
}

// Store inserts one usage event. Implementations must enforce uniqueness by
// (run_id,event_id), returning ErrDuplicate for a replay.
type Store interface{ CreateUsage(Record) error }
type Reader interface {
	ListRun(runstate.Owner, string) ([]Record, error)
}

// MemoryStore is the Phase 3 implementation and a useful deterministic test
// double. Phase 4 supplies the Postgres implementation behind the same API.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{records: make(map[string]Record)} }
func (s *MemoryStore) CreateUsage(r Record) error {
	if r.RunID == "" || r.EventID == "" || r.UserID == "" || r.WorkspaceID == "" {
		return errors.New("usage identity required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := r.RunID + "\x00" + r.EventID
	if _, exists := s.records[k]; exists {
		return ErrDuplicate
	}
	s.records[k] = r
	return nil
}
func (s *MemoryStore) Records() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out
}
func (s *MemoryStore) ListRun(owner runstate.Owner, runID string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Record
	for _, r := range s.records {
		if r.RunID == runID && r.UserID == owner.UserID && r.WorkspaceID == owner.WorkspaceID {
			out = append(out, r)
		}
	}
	return out, nil
}

// Pricing is expressed per million tokens.
type Pricing struct{ Input, Output, CacheHit float64 }

// CapturingSink records stamped usage events then forwards every event.
type CapturingSink struct {
	Store           Store
	Owner           runstate.Owner
	Provider, Model string
	Pricing         Pricing
	Next            event.Sink
}

func (s CapturingSink) Emit(e event.Event) {
	if e.Kind == event.UsageKind && e.Usage != nil && s.Store != nil {
		input := e.Usage.PromptTokens
		if e.Usage.CacheMissTokens > 0 {
			input = e.Usage.CacheMissTokens
		}
		cost := (float64(input)*s.Pricing.Input + float64(e.Usage.CompletionTokens)*s.Pricing.Output + float64(e.Usage.CacheHitTokens)*s.Pricing.CacheHit)
		r := Record{EventID: e.EventID, RunID: e.RunID, UserID: s.Owner.UserID, WorkspaceID: s.Owner.WorkspaceID, Provider: s.Provider, Model: s.Model, InputTokens: e.Usage.PromptTokens, OutputTokens: e.Usage.CompletionTokens, CacheHitTokens: e.Usage.CacheHitTokens, CacheMissTokens: e.Usage.CacheMissTokens, EstimatedCostMicros: int64(math.Round(cost)), CreatedAt: e.Timestamp}
		if err := s.Store.CreateUsage(r); err != nil && !errors.Is(err, ErrDuplicate) && s.Next != nil {
			s.Next.Emit(event.Event{SchemaVersion: e.SchemaVersion, RunID: e.RunID, EventID: e.EventID + ":usage_error", Kind: event.Notice, Level: event.LevelErr, Text: "usage persistence failed: " + err.Error(), Timestamp: time.Now().UTC()})
		}
	}
	if s.Next != nil {
		s.Next.Emit(e)
	}
}
