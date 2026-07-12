// Package quota defines the hosted runtime's fail-closed quota boundary.
package quota

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"lumen/internal/event"
	"lumen/internal/runstate"
	"lumen/internal/usage"
)

const (
	CodeUnavailable         = "quota_unavailable"
	CodeConcurrent          = "quota_user_concurrent_runs"
	CodeWorkspaceConcurrent = "quota_workspace_concurrent_runs"
	CodeTokens              = "quota_monthly_tokens"
	CodeCompute             = "quota_monthly_compute"
	CodeStorage             = "quota_storage"
	CodeWallTime            = "quota_run_wall_time"
	CodeSteps               = "quota_run_steps"
	CodeEvents              = "quota_run_events"
	CodeEventSize           = "quota_event_size"
	CodeArtifact            = "quota_artifact_single"
	CodeArtifactTotal       = "quota_artifact_total"
)

type Error struct{ Code, Message, NextAction string }

func (e *Error) Error() string { return e.Message }
func IsLimit(err error) bool   { var e *Error; return errors.As(err, &e) }

// Failure safely retains the first asynchronous persistence or limit failure.
// The run loop reads it before selecting the terminal state, so cancellation
// caused by a hard quota cannot be misreported as a user cancellation.
type Failure struct {
	mu  sync.Mutex
	err error
}

func (f *Failure) Set(err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil {
		f.err = err
	}
}

func (f *Failure) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

type Limits struct {
	UserConcurrent, WorkspaceConcurrent int
	MonthlyTokens, MonthlyComputeMillis int64
	StorageBytes, MonthlyCostMicros     int64
	MaxWallTime                         time.Duration
	MaxSteps, MaxEvents                 int
	MaxEventBytes, MaxArtifactBytes     int64
}

func LocalLimits() Limits {
	return Limits{UserConcurrent: 1 << 20, WorkspaceConcurrent: 1 << 20, MonthlyTokens: 1 << 62, MonthlyComputeMillis: 1 << 62, StorageBytes: 1 << 62, MonthlyCostMicros: 1 << 62, MaxWallTime: 5 * time.Minute, MaxSteps: 1 << 20, MaxEvents: 1 << 20, MaxEventBytes: 16 << 20, MaxArtifactBytes: 100 << 20}
}

type Admission struct {
	RunID, IdempotencyKey string
	Owner                 runstate.Owner
	StartedAt             time.Time
}
type Completion struct {
	RunID, IdempotencyKey string
	Owner                 runstate.Owner
	Status                string
	ComputeMillis         int64
	CompletedAt           time.Time
}
type Artifact struct {
	RunID, IdempotencyKey string
	Owner                 runstate.Owner
	Bytes                 int64
}

// Store must make each method atomic and idempotent by IdempotencyKey. Hosted
// implementations are durable (Oasis/Postgres); Redis may only be a cache.
type Store interface {
	Admit(context.Context, Admission) (Limits, error)
	RecordUsage(context.Context, usage.Record) error
	ReserveArtifact(context.Context, Artifact) error
	ReleaseArtifact(context.Context, Artifact) error
	Complete(context.Context, Completion) error
}

// UsageStore persists the billing record and charges quota using the same
// event identity. A quota rejection is returned to the run, never hidden.
type UsageStore struct {
	Usage usage.Store
	Quota Store
}

func (s UsageStore) CreateUsage(r usage.Record) error {
	// Persist the canonical usage row first. A duplicate means an earlier
	// attempt may have committed the row but failed before the control-plane
	// debit, so it must continue to the idempotent quota call. Any other
	// persistence failure must not charge the tenant.
	if err := s.Usage.CreateUsage(r); err != nil && !errors.Is(err, usage.ErrDuplicate) {
		return err
	}
	return s.Quota.RecordUsage(context.Background(), r)
}

// MemoryStore is the local default and deterministic concurrency test double.
type MemoryStore struct {
	mu                             sync.Mutex
	Limits                         Limits
	admissions                     map[string]Admission
	complete                       map[string]bool
	artifacts                      map[string]bool
	usage                          map[string]bool
	user, workspace                map[string]int
	tokens, compute, storage, cost map[string]int64
}

func NewMemoryStore(l Limits) *MemoryStore {
	return &MemoryStore{Limits: l, admissions: map[string]Admission{}, complete: map[string]bool{}, artifacts: map[string]bool{}, usage: map[string]bool{}, user: map[string]int{}, workspace: map[string]int{}, tokens: map[string]int64{}, compute: map[string]int64{}, storage: map[string]int64{}, cost: map[string]int64{}}
}
func month(o runstate.Owner, t time.Time) string {
	return o.UserID + "\x00" + o.WorkspaceID + "\x00" + t.UTC().Format("2006-01")
}
func (s *MemoryStore) Admit(_ context.Context, a Admission) (Limits, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.admissions[a.IdempotencyKey]; ok {
		if old.RunID != a.RunID {
			return s.Limits, &Error{Code: CodeConcurrent, Message: "idempotency key conflict", NextAction: "start_new_run"}
		}
		return s.Limits, nil
	}
	u := a.Owner.UserID
	w := u + "\x00" + a.Owner.WorkspaceID
	if s.user[u] >= s.Limits.UserConcurrent {
		return s.Limits, &Error{Code: CodeConcurrent, Message: "user concurrent run quota exceeded", NextAction: "wait_for_run"}
	}
	if s.workspace[w] >= s.Limits.WorkspaceConcurrent {
		return s.Limits, &Error{Code: CodeWorkspaceConcurrent, Message: "workspace concurrent run quota exceeded", NextAction: "wait_for_run"}
	}
	k := month(a.Owner, a.StartedAt)
	if s.tokens[k] >= s.Limits.MonthlyTokens {
		return s.Limits, &Error{Code: CodeTokens, Message: "monthly token quota exceeded", NextAction: "retry_next_month"}
	}
	if s.compute[k] >= s.Limits.MonthlyComputeMillis {
		return s.Limits, &Error{Code: CodeCompute, Message: "monthly compute quota exceeded", NextAction: "retry_next_month"}
	}
	if s.storage[w] >= s.Limits.StorageBytes {
		return s.Limits, &Error{Code: CodeStorage, Message: "storage quota exceeded", NextAction: "delete_artifacts"}
	}
	s.admissions[a.IdempotencyKey] = a
	s.user[u]++
	s.workspace[w]++
	return s.Limits, nil
}
func (s *MemoryStore) Complete(_ context.Context, c Completion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.complete[c.IdempotencyKey] {
		return nil
	}
	s.complete[c.IdempotencyKey] = true
	a, ok := s.admissions[c.RunID+":admit"]
	if !ok {
		for _, v := range s.admissions {
			if v.RunID == c.RunID {
				a = v
				ok = true
				break
			}
		}
	}
	if ok {
		s.user[a.Owner.UserID]--
		s.workspace[a.Owner.UserID+"\x00"+a.Owner.WorkspaceID]--
	}
	s.compute[month(c.Owner, c.CompletedAt)] += c.ComputeMillis
	return nil
}
func (s *MemoryStore) RecordUsage(_ context.Context, r usage.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := r.RunID + "\x00" + r.EventID
	if s.usage[k] {
		return nil
	}
	m := month(runstate.Owner{UserID: r.UserID, WorkspaceID: r.WorkspaceID}, r.CreatedAt)
	// Oasis's billing contract counts every provider-reported bucket. Keeping
	// this explicit also makes the debit stable across providers and retries.
	n := int64(r.InputTokens + r.OutputTokens + r.CacheHitTokens + r.CacheMissTokens)
	if s.tokens[m]+n > s.Limits.MonthlyTokens {
		return &Error{Code: CodeTokens, Message: "monthly token quota exceeded", NextAction: "retry_next_month"}
	}
	s.tokens[m] += n
	s.cost[m] += r.EstimatedCostMicros
	s.usage[k] = true
	return nil
}
func (s *MemoryStore) ReserveArtifact(_ context.Context, a Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.artifacts[a.IdempotencyKey] {
		return nil
	}
	if a.Bytes > s.Limits.MaxArtifactBytes {
		return &Error{Code: CodeArtifact, Message: "artifact exceeds per-file quota", NextAction: "reduce_artifact"}
	}
	w := a.Owner.UserID + "\x00" + a.Owner.WorkspaceID
	if s.storage[w]+a.Bytes > s.Limits.StorageBytes {
		return &Error{Code: CodeArtifactTotal, Message: "artifact storage quota exceeded", NextAction: "reduce_artifact"}
	}
	s.storage[w] += a.Bytes
	s.artifacts[a.IdempotencyKey] = true
	return nil
}
func (s *MemoryStore) ReleaseArtifact(_ context.Context, a Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.artifacts[a.IdempotencyKey] {
		return nil
	}
	delete(s.artifacts, a.IdempotencyKey)
	w := a.Owner.UserID + "\x00" + a.Owner.WorkspaceID
	s.storage[w] -= a.Bytes
	if s.storage[w] < 0 {
		s.storage[w] = 0
	}
	return nil
}

// Sink enforces per-run event/step/size limits before forwarding or persisting.
type Sink struct {
	Limits        Limits
	Next          event.Sink
	Failure       func(error)
	mu            sync.Mutex
	events, steps int
	failed        bool
}

func (s *Sink) Emit(e event.Event) {
	b, _ := json.Marshal(e)
	s.mu.Lock()
	if s.failed {
		s.mu.Unlock()
		return
	}
	s.events++
	if e.Kind == event.ToolDispatch {
		s.steps++
	}
	var err error
	if s.events > s.Limits.MaxEvents {
		err = &Error{Code: CodeEvents, Message: "run event quota exceeded", NextAction: "start_new_run"}
	} else if s.steps > s.Limits.MaxSteps {
		err = &Error{Code: CodeSteps, Message: "run step quota exceeded", NextAction: "start_new_run"}
	} else if int64(len(b)) > s.Limits.MaxEventBytes {
		err = &Error{Code: CodeEventSize, Message: "event exceeds size quota", NextAction: "reduce_event"}
	}
	if err != nil {
		s.failed = true
	}
	s.mu.Unlock()
	if err != nil {
		if s.Failure != nil {
			s.Failure(err)
		}
		return
	}
	if s.Next != nil {
		s.Next.Emit(e)
	}
}
