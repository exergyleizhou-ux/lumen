package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAgentPoolRegister(t *testing.T) {
	pool := NewAgentPool()
	a := &mockAgent{name: "test-agent", result: "ok"}
	pool.Register(a)
	if pool.Select("test-agent", nil) != a { t.Error("should select registered agent") }
}

func TestAgentPoolAcquireRelease(t *testing.T) {
	pool := NewAgentPool()
	a := &mockAgent{name: "limited", result: "ok"}
	pool.Register(a)
	if !pool.Acquire("limited") { t.Error("first acquire should succeed") }
	if pool.Acquire("limited") { t.Error("second acquire should fail (max=1)") }
	pool.Release("limited")
	if !pool.Acquire("limited") { t.Error("should succeed after release") }
}

func TestExecutorSimplePlan(t *testing.T) {
	pool := NewAgentPool()
	a := &mockAgent{name: "worker", result: "done"}
	pool.Register(a)
	exec := NewExecutor(DefaultConfig(), pool)
	plan := exec.NewPlan("test", []*Task{{ID: "t1", Name: "task-1", Agent: "worker", Prompt: "do it", MaxRetries: 1}})
	err := exec.Execute(context.Background(), plan)
	if err != nil { t.Fatalf("execute: %v", err) }
	if plan.Status != PlanCompleted { t.Errorf("expected completed, got %s", plan.Status) }
}

func TestExecutorDependencies(t *testing.T) {
	pool := NewAgentPool()
	a := &mockAgent{name: "w", result: "ok"}
	pool.Register(a)
	exec := NewExecutor(DefaultConfig(), pool)
	plan := exec.NewPlan("dep-test", []*Task{
		{ID: "a", Name: "first", Agent: "w", Prompt: "first", MaxRetries: 1},
		{ID: "b", Name: "second", Agent: "w", Prompt: "second", DependsOn: []string{"a"}, MaxRetries: 1},
		{ID: "c", Name: "third", Agent: "w", Prompt: "third", DependsOn: []string{"a"}, MaxRetries: 1},
	})
	err := exec.Execute(context.Background(), plan)
	if err != nil { t.Fatalf("execute: %v", err) }
	if plan.Status != PlanCompleted { t.Errorf("expected completed, got %s", plan.Status) }
	if a.calls < 3 { t.Errorf("expected at least 3 calls, got %d", a.calls) }
}

func TestAgentPoolFormat(t *testing.T) {
	pool := NewAgentPool()
	pool.Register(&mockAgent{name: "alpha", result: "ok"})
	s := pool.FormatStats()
	if !strings.Contains(s, "alpha") { t.Error("should contain agent name") }
}

func TestNewPlan(t *testing.T) {
	exec := NewExecutor(DefaultConfig(), NewAgentPool())
	plan := exec.NewPlan("test", []*Task{{ID: "1", Name: "t1", Prompt: "hi"}})
	if plan.Status != PlanPending { t.Error("new plan should be pending") }
	if plan.ID == "" { t.Error("plan should have ID") }
}

func TestUsageSnapshot(t *testing.T) {
	pool := NewAgentPool()
	pool.Register(&mockAgent{name: "a", result: "ok"})
	snap := pool.Snapshot()
	if snap.TotalBusy != 0 { t.Error("no tasks should be busy") }
	if len(snap.Agents) == 0 { t.Error("should have agent stats") }
}

func TestExecutorMaxParallel(t *testing.T) {
	pool := NewAgentPool()
	pool.Register(&mockAgent{name: "w", result: "ok"})
	cfg := DefaultConfig(); cfg.MaxParallel = 2
	exec := NewExecutor(cfg, pool)
	tasks := make([]*Task, 5)
	for i := range tasks {
		tasks[i] = &Task{ID: fmt.Sprintf("t%d", i), Name: fmt.Sprintf("task-%d", i), Agent: "w", Prompt: "run", MaxRetries: 1}
	}
	plan := exec.NewPlan("parallel-test", tasks)
	err := exec.Execute(context.Background(), plan)
	if err != nil { t.Fatalf("execute: %v", err) }
}

func TestOrchestratorFormatPlan(t *testing.T) {
	exec := NewExecutor(DefaultConfig(), NewAgentPool())
	plan := exec.NewPlan("format-test", []*Task{{ID: "1", Name: "done-task", Status: TaskDone, Result: "success", Agent: "w"}})
	out := exec.FormatPlan(plan)
	if !strings.Contains(out, "done-task") { t.Error("should contain task name") }
}
