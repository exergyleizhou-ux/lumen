package maestro

import ("context";"fmt";"strings";"testing";"time")

func TestOrchestratorRun(t *testing.T) {
	ctx := context.Background()
	o := NewOrchestrator()
	wb := NewWorkflow("wf1", "Simple Workflow")
	wb.Task("t1", "Task 1", nil, func(ctx context.Context) error { return nil })
	wb.Task("t2", "Task 2", []string{"t1"}, func(ctx context.Context) error { return nil })
	wb.Timeout(10 * time.Second)
	o.RegisterWorkflow(wb.Build())
	result, err := o.Run(ctx, "wf1")
	if err != nil { t.Fatal(err) }
	if result.Status != "success" { t.Errorf("want success, got %s", result.Status) }
	if len(result.Results) != 2 { t.Error("result count") }
}
func TestOrchestratorFail(t *testing.T) {
	ctx := context.Background()
	o := NewOrchestrator()
	wb := NewWorkflow("wf2", "Failing")
	wb.Task("t1", "FailTask", nil, func(ctx context.Context) error { return fmt.Errorf("boom") })
	o.RegisterWorkflow(wb.Build())
	result, _ := o.Run(ctx, "wf2")
	if result.Status != "failed" { t.Errorf("want failed, got %s", result.Status) }
}
func TestOrchestratorCycle(t *testing.T) {
	o := NewOrchestrator()
	wf := &Workflow{ID: "cycle", Name: "Cycle", Tasks: map[string]*Task{}, Timeout: 5 * time.Second}
	wf.Tasks["a"] = &Task{ID: "a", Name: "A", DependsOn: []string{"b"}, Fn: func(ctx context.Context) error { return nil }, Timeout: time.Minute}
	wf.Tasks["b"] = &Task{ID: "b", Name: "B", DependsOn: []string{"a"}, Fn: func(ctx context.Context) error { return nil }, Timeout: time.Minute}
	o.RegisterWorkflow(wf)
	_, err := o.Run(context.Background(), "cycle")
	if err == nil { t.Error("should detect cycle") }
}
func TestFormatResult(t *testing.T) {
	r := &RunResult{WorkflowID: "wf", Status: "success", Results: []Result{{TaskID: "t1", Status: "success", Duration: time.Second}}}
	s := FormatRunResult(r)
	if !strings.Contains(s, "wf") { t.Error("format") }
}
func TestRetryPolicyDefault(t *testing.T) {
	rp := DefaultRetryPolicy()
	if rp.MaxRetries != 3 { t.Error("default retry") }
}
