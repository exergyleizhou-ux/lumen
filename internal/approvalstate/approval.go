// Package approvalstate defines durable, owner-scoped tool approvals.
package approvalstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"lumen/internal/runstate"
)

var ErrNotFound = errors.New("approval not found")
var ErrNotExecutable = errors.New("approval is not executable")

type Decision string

const (
	DecisionApproved    Decision = "approved"
	DecisionRejected    Decision = "rejected"
	DecisionExpired     Decision = "expired"
	DecisionInvalidated Decision = "invalidated"
)

type Effects struct {
	Reads, Writes, Commands, Network, Remote, Compute, Publish, Charge bool `json:",omitempty"`
}
type Approval struct {
	ID                  string          `json:"approval_id"`
	RunID               string          `json:"run_id"`
	StepID              string          `json:"step_id"`
	ToolCallID          string          `json:"tool_call_id"`
	Owner               runstate.Owner  `json:"owner"`
	RiskLevel           string          `json:"risk_level"`
	Reason              string          `json:"reason"`
	Effects             Effects         `json:"effects"`
	Command             string          `json:"command,omitempty"`
	FileScope           []string        `json:"file_scope,omitempty"`
	RemoteTarget        string          `json:"remote_target,omitempty"`
	NetworkTargets      []string        `json:"network_targets,omitempty"`
	EstimatedCostMicros int64           `json:"estimated_cost_micros"`
	ExpectedOutputs     []string        `json:"expected_outputs,omitempty"`
	ArgsHash            string          `json:"args_hash"`
	EditableArgs        json.RawMessage `json:"editable_args,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	ExpiresAt           time.Time       `json:"expires_at"`
	DecidedAt           *time.Time      `json:"decided_at,omitempty"`
	DecidedBy           string          `json:"decided_by,omitempty"`
	Decision            *Decision       `json:"decision,omitempty"`
	Version             uint64          `json:"version"`
	ExecutionID         string          `json:"execution_id,omitempty"`
	ExecutionState      string          `json:"execution_state"`
	ExecutedAt          *time.Time      `json:"executed_at,omitempty"`
}

func HashArgs(args json.RawMessage) (string, error) {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(canonical)
	return hex.EncodeToString(h[:]), nil
}

type Store interface {
	Create(Approval) error
	Get(runstate.Owner, string) (Approval, error)
	Decide(runstate.Owner, string, Decision, string, time.Time) (Approval, error)
	ListRun(runstate.Owner, string) ([]Approval, error)
	Consume(runstate.Owner, string, string, time.Time) (Approval, error)
	Complete(runstate.Owner, string, string, bool, time.Time) error
}
type MemoryStore struct {
	mu     sync.Mutex
	values map[string]Approval
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{values: map[string]Approval{}} }
func (s *MemoryStore) Create(a Approval) error {
	if !a.Owner.Valid() || a.ID == "" || a.RunID == "" || a.ArgsHash == "" {
		return errors.New("approval identity required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[a.ID]; ok {
		return errors.New("approval already exists")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Version == 0 {
		a.Version = 1
	}
	if a.ExecutionState == "" {
		a.ExecutionState = "pending"
	}
	s.values[a.ID] = a
	return nil
}
func (s *MemoryStore) Get(o runstate.Owner, id string) (Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.values[id]
	if !ok || a.Owner != o {
		return Approval{}, ErrNotFound
	}
	return a, nil
}
func (s *MemoryStore) Decide(o runstate.Owner, id string, d Decision, by string, now time.Time) (Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.values[id]
	if !ok || a.Owner != o {
		return Approval{}, ErrNotFound
	}
	if a.Decision != nil || !now.Before(a.ExpiresAt) {
		return Approval{}, ErrNotExecutable
	}
	a.Decision = &d
	a.DecidedAt = &now
	a.DecidedBy = by
	a.Version++
	s.values[id] = a
	return a, nil
}
func (s *MemoryStore) ListRun(o runstate.Owner, run string) ([]Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Approval
	for _, a := range s.values {
		if a.Owner == o && a.RunID == run {
			out = append(out, a)
		}
	}
	return out, nil
}
func (s *MemoryStore) Consume(o runstate.Owner, id, executionID string, now time.Time) (Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.values[id]
	if !ok || a.Owner != o {
		return Approval{}, ErrNotFound
	}
	if a.Decision == nil || *a.Decision != DecisionApproved || a.ExecutionState != "pending" || !now.Before(a.ExpiresAt) {
		return Approval{}, ErrNotExecutable
	}
	a.ExecutionID = executionID
	a.ExecutionState = "consumed"
	a.Version++
	s.values[id] = a
	return a, nil
}
func (s *MemoryStore) Complete(o runstate.Owner, id, executionID string, success bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.values[id]
	if !ok || a.Owner != o {
		return ErrNotFound
	}
	if a.ExecutionState != "consumed" || a.ExecutionID != executionID {
		return ErrNotExecutable
	}
	a.ExecutionState = "failed"
	if success {
		a.ExecutionState = "executed"
	}
	a.ExecutedAt = &now
	a.Version++
	s.values[id] = a
	return nil
}

// ValidateExecution fails closed. A parameter change invalidates the old grant;
// callers must create and expose a new approval before invoking the tool.
func ValidateExecution(store Store, owner runstate.Owner, id string, args json.RawMessage, now time.Time) error {
	a, err := store.Get(owner, id)
	if err != nil {
		return err
	}
	if a.Decision == nil || *a.Decision != DecisionApproved || !now.Before(a.ExpiresAt) {
		return ErrNotExecutable
	}
	h, err := HashArgs(args)
	if err != nil {
		return err
	}
	if h != a.ArgsHash {
		return ErrNotExecutable
	}
	return nil
}
