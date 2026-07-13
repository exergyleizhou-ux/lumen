package approvalstate

import (
	"encoding/json"
	"errors"
	"lumen/internal/runstate"
	"testing"
	"time"
)

func TestExecutionIsOwnerExpiryAndArgsBound(t *testing.T) {
	s := NewMemoryStore()
	o := runstate.Owner{UserID: "u", WorkspaceID: "w"}
	args := json.RawMessage(`{"b":2,"a":1}`)
	h, _ := HashArgs(args)
	now := time.Now().UTC()
	d := DecisionApproved
	a := Approval{ID: "a", RunID: "r", ToolCallID: "t", Owner: o, ArgsHash: h, CreatedAt: now, ExpiresAt: now.Add(time.Minute), Decision: &d}
	if err := s.Create(a); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExecution(s, o, "a", json.RawMessage(`{"a":1,"b":2}`), now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		o    runstate.Owner
		args json.RawMessage
		at   time.Time
	}{{runstate.Owner{UserID: "x", WorkspaceID: "w"}, args, now}, {o, json.RawMessage(`{"a":9}`), now}, {o, args, now.Add(2 * time.Minute)}} {
		if err := ValidateExecution(s, tc.o, "a", tc.args, tc.at); err == nil {
			t.Fatal("unsafe approval accepted")
		}
	}
}
func TestDecisionIsCASAndNonEnumerating(t *testing.T) {
	s := NewMemoryStore()
	o := runstate.Owner{UserID: "u", WorkspaceID: "w"}
	h, _ := HashArgs(json.RawMessage(`{}`))
	now := time.Now()
	s.Create(Approval{ID: "a", RunID: "r", ToolCallID: "t", Owner: o, ArgsHash: h, ExpiresAt: now.Add(time.Minute)})
	if _, err := s.Decide(o, "a", DecisionApproved, "u", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(o, "a", DecisionApproved, "u", now); !errors.Is(err, ErrNotExecutable) {
		t.Fatalf("got %v", err)
	}
	if _, err := s.Get(runstate.Owner{UserID: "other", WorkspaceID: "w"}, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner leaked: %v", err)
	}
}
func TestConsumeIsAtomicAcrossCrashAndRetry(t *testing.T) {
	s := NewMemoryStore()
	o := runstate.Owner{UserID: "u", WorkspaceID: "w"}
	h, _ := HashArgs(json.RawMessage(`{}`))
	now := time.Now()
	d := DecisionApproved
	s.Create(Approval{ID: "a", RunID: "r", ToolCallID: "tc", Owner: o, ArgsHash: h, Decision: &d, ExpiresAt: now.Add(time.Minute)})
	if _, err := s.Consume(o, "a", "exec-r-tc", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consume(o, "a", "exec-r-tc", now); !errors.Is(err, ErrNotExecutable) {
		t.Fatalf("replayed dangerous execution: %v", err)
	}
	restarted := s
	if _, err := restarted.Consume(o, "a", "different", now); !errors.Is(err, ErrNotExecutable) {
		t.Fatalf("restart repeated execution: %v", err)
	}
	if err := s.Complete(o, "a", "exec-r-tc", true, now); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Get(o, "a")
	if a.ExecutionState != "executed" || a.ExecutedAt == nil {
		t.Fatalf("state=%+v", a)
	}
}
