package dispatcher

import (
	"context"
	"testing"
	"time"
)

type mockWorker struct{ id string }

func (mw *mockWorker) ID() string { return mw.id }
func (mw *mockWorker) Process(ctx context.Context, task *Task) *TaskResult {
	time.Sleep(10 * time.Millisecond)
	return &TaskResult{TaskID: task.ID, Success: true, Duration: time.Millisecond, CompletedAt: time.Now()}
}
func TestEnqueue(t *testing.T) {
	d := NewDispatcher()
	d.Enqueue(&Task{ID: "t1", Priority: PriorityNormal})
	if d.QueueLen() != 1 {
		t.Error("enqueue")
	}
}
func TestDispatch(t *testing.T) {
	d := NewDispatcher()
	d.RegisterWorker(&mockWorker{"w1"})
	d.Enqueue(&Task{ID: "t2", Priority: PriorityHigh})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	d.Stop()
	results := d.Results()
	if len(results) != 1 {
		t.Error("results")
	}
	if !results[0].Success {
		t.Error("should succeed")
	}
}
func TestFormatStatus(t *testing.T) {
	d := NewDispatcher()
	d.Enqueue(&Task{ID: "t3", Priority: PriorityCritical})
	s := d.FormatStatus()
	if s == "" {
		t.Error("format")
	}
}
