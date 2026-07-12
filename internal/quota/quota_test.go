package quota

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"lumen/internal/event"
	"lumen/internal/runstate"
	"lumen/internal/usage"
)

func limits() Limits {
	return Limits{UserConcurrent: 1, WorkspaceConcurrent: 1, MonthlyTokens: 10, MonthlyComputeMillis: 100, StorageBytes: 20, MonthlyCostMicros: 20, MaxWallTime: time.Second, MaxSteps: 1, MaxEvents: 2, MaxEventBytes: 1024, MaxArtifactBytes: 10}
}
func TestAdmissionAtomicNoOversellAndIdempotentCompletion(t *testing.T) {
	s := NewMemoryStore(limits())
	o := runstate.Owner{UserID: "u", WorkspaceID: "w"}
	var wg sync.WaitGroup
	wins := 0
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, e := s.Admit(context.Background(), Admission{RunID: string(rune('a' + n)), IdempotencyKey: string(rune('a'+n)) + ":admit", Owner: o, StartedAt: time.Now()})
			if e == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("admissions=%d", wins)
	}
	var admitted Admission
	s.mu.Lock()
	for _, a := range s.admissions {
		admitted = a
	}
	s.mu.Unlock()
	c := Completion{RunID: admitted.RunID, IdempotencyKey: admitted.RunID + ":complete", Owner: o, CompletedAt: time.Now()}
	if err := s.Complete(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Admit(context.Background(), Admission{RunID: "next", IdempotencyKey: "next:admit", Owner: o, StartedAt: time.Now()}); err != nil {
		t.Fatalf("slot not released: %v", err)
	}
}

func TestUsageDuplicateAndMonthReset(t *testing.T) {
	s := NewMemoryStore(limits())
	r := usage.Record{RunID: "r", EventID: "e", UserID: "u", WorkspaceID: "w", InputTokens: 4, OutputTokens: 2, CacheHitTokens: 2, CacheMissTokens: 2, CreatedAt: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)}
	if err := s.RecordUsage(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUsage(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	r.EventID = "e2"
	if err := s.RecordUsage(context.Background(), r); err == nil {
		t.Fatal("expected quota")
	}
	r.EventID = "e3"
	r.CreatedAt = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := s.RecordUsage(context.Background(), r); err != nil {
		t.Fatalf("month did not reset: %v", err)
	}
}

func TestCanceledAndFailedCompletionChargeComputeOnce(t *testing.T) {
	for _, status := range []string{"canceled", "failed"} {
		t.Run(status, func(t *testing.T) {
			s := NewMemoryStore(limits())
			o := runstate.Owner{UserID: "u", WorkspaceID: "w"}
			_, _ = s.Admit(context.Background(), Admission{RunID: "r", IdempotencyKey: "r:admit", Owner: o, StartedAt: time.Now()})
			c := Completion{RunID: "r", IdempotencyKey: "r:complete", Owner: o, Status: status, ComputeMillis: 7, CompletedAt: time.Now()}
			_ = s.Complete(context.Background(), c)
			_ = s.Complete(context.Background(), c)
			s.mu.Lock()
			defer s.mu.Unlock()
			if got := s.compute[month(o, c.CompletedAt)]; got != 7 {
				t.Fatalf("compute=%d", got)
			}
		})
	}
}

func TestSinkStopsBeforeForwardingLimitEvent(t *testing.T) {
	var got int
	s := &Sink{Limits: limits(), Next: event.FuncSink(func(event.Event) { got++ }), Failure: func(err error) {
		var q *Error
		if !errors.As(err, &q) || q.Code != CodeSteps {
			t.Errorf("err=%v", err)
		}
	}}
	s.Emit(event.Event{Kind: event.ToolDispatch})
	s.Emit(event.Event{Kind: event.ToolDispatch})
	s.Emit(event.Event{Kind: event.Text})
	if got != 1 {
		t.Fatalf("forwarded=%d", got)
	}
}

func TestArtifactReserveDuplicateAndRelease(t *testing.T) {
	s := NewMemoryStore(limits())
	a := Artifact{RunID: "r", IdempotencyKey: "a", Owner: runstate.Owner{UserID: "u", WorkspaceID: "w"}, Bytes: 8}
	if err := s.ReserveArtifact(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveArtifact(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseArtifact(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveArtifact(context.Background(), Artifact{RunID: "r", IdempotencyKey: "big", Owner: a.Owner, Bytes: 11}); err == nil {
		t.Fatal("expected single artifact limit")
	}
}
