package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestWorkflowRunsRegisteredAgent proves the wiring: once a workflow agent is
// registered, create_workflow + run_workflow actually execute it (previously
// the pool was empty and every task failed "no agent available").
func TestWorkflowRunsRegisteredAgent(t *testing.T) {
	resetWorkflowState()
	var ran int
	SetWorkflowAgent("lumen", func(ctx context.Context, prompt string) (string, error) {
		ran++
		return "handled:" + prompt, nil
	}, 4)
	t.Cleanup(func() { resetWorkflowState() })

	create := &CreateWorkflowTool{}
	args, _ := json.Marshal(map[string]any{
		"name": "wf1",
		"tasks": []map[string]any{
			{"id": "a", "name": "task-a", "prompt": "alpha"},
		},
	})
	if _, err := create.Execute(context.Background(), args); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}

	run := &RunWorkflowTool{}
	runArgs, _ := json.Marshal(map[string]any{"workflow_name": "wf1"})
	out, err := run.Execute(context.Background(), runArgs)
	if err != nil {
		t.Fatalf("run_workflow: %v", err)
	}
	if ran != 1 {
		t.Errorf("registered agent ran %d times, want 1", ran)
	}
	if !strings.Contains(out, "✅") || !strings.Contains(strings.ToLower(out), "handled:alpha") {
		t.Errorf("run_workflow output did not show the completed task:\n%s", out)
	}
}

func TestWorkflowWithoutAgentStillReportsClearly(t *testing.T) {
	// With no agent registered the task fails — but the executor must not panic;
	// it returns the "no agent available" error path.
	resetWorkflowState()
	t.Cleanup(func() { resetWorkflowState() })

	create := &CreateWorkflowTool{}
	args, _ := json.Marshal(map[string]any{
		"name":  "wf-empty",
		"tasks": []map[string]any{{"id": "a", "name": "task-a", "prompt": "x"}},
	})
	if _, err := create.Execute(context.Background(), args); err != nil {
		t.Fatalf("create_workflow: %v", err)
	}
	run := &RunWorkflowTool{}
	runArgs, _ := json.Marshal(map[string]any{"workflow_name": "wf-empty"})
	if _, err := run.Execute(context.Background(), runArgs); err == nil {
		t.Error("expected an error when no workflow agent is registered")
	}
}
