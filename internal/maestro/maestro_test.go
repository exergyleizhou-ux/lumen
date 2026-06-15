package maestro

import (
	"context"
	"fmt"
	"testing"
)

func TestOrchestratorRun(t *testing.T) {
	ctx := context.Background()
	o := NewOrchestrator()
	wb := NewWorkflow("wf1", "Simple")
	wb.Task("t1", "Task1", nil, func(ctx context.Context) error { return nil })
	o.RegisterWorkflow(wb.Build())
	result, err := o.Run(ctx, "wf1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" {
		t.Error("status")
	}
}
func TestFail(t *testing.T) {
	ctx := context.Background()
	o := NewOrchestrator()
	wb := NewWorkflow("wf2", "Failing")
	wb.Task("t1", "Fail", nil, func(ctx context.Context) error { return fmt.Errorf("boom") })
	o.RegisterWorkflow(wb.Build())
	result, _ := o.Run(ctx, "wf2")
	if result.Status != "failed" {
		t.Error("status")
	}
}
func TestRetry(t *testing.T) {
	rp := DefaultRetryPolicy()
	if rp.MaxRetries != 3 {
		t.Error("retry")
	}
}
