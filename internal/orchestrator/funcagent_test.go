package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFuncAgentExecutes(t *testing.T) {
	a := &FuncAgent{
		AgentName:   "echo",
		Concurrency: 4,
		Exec: func(ctx context.Context, prompt string) (string, error) {
			return "did: " + prompt, nil
		},
	}
	if a.Name() != "echo" {
		t.Errorf("Name = %q, want echo", a.Name())
	}
	if !a.IsAvailable() {
		t.Error("IsAvailable = false")
	}
	if a.MaxConcurrency() != 4 {
		t.Errorf("MaxConcurrency = %d, want 4", a.MaxConcurrency())
	}
	out, err := a.Execute(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "did: hi" {
		t.Errorf("Execute = %q", out)
	}
}

// TestExecutorRunsTasksInParallel proves the orchestrator actually runs
// independent tasks concurrently through a real registered agent — and that it
// is race-clean (run with -race). Each task records the live concurrency; we
// assert the peak exceeded 1.
func TestExecutorRunsTasksInParallel(t *testing.T) {
	var live, peak int64
	agent := &FuncAgent{
		AgentName:   "worker",
		Concurrency: 8,
		Exec: func(ctx context.Context, prompt string) (string, error) {
			n := atomic.AddInt64(&live, 1)
			for {
				p := atomic.LoadInt64(&peak)
				if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond) // hold the slot so others overlap
			atomic.AddInt64(&live, -1)
			return "ok:" + prompt, nil
		},
	}

	pool := NewAgentPool()
	pool.Register(agent)
	exec := NewExecutor(Config{MaxParallel: 8, DefaultTimeout: time.Minute}, pool)

	var tasks []*Task
	for i := 0; i < 6; i++ {
		tasks = append(tasks, &Task{ID: fmt.Sprintf("t%d", i), Name: fmt.Sprintf("t%d", i), Prompt: fmt.Sprintf("p%d", i)})
	}
	plan := exec.NewPlan("parallel", tasks)

	if err := exec.Execute(context.Background(), plan); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if plan.Status != PlanCompleted {
		t.Fatalf("plan status = %s, want completed", plan.Status)
	}
	for _, tk := range plan.Tasks {
		if tk.Status != TaskDone {
			t.Errorf("task %s status = %s, want done", tk.ID, tk.Status)
		}
		if tk.Result == "" {
			t.Errorf("task %s produced no result", tk.ID)
		}
	}
	if atomic.LoadInt64(&peak) < 2 {
		t.Errorf("peak concurrency = %d, want ≥2 (tasks did not run in parallel)", peak)
	}
}

// TestExecutorConcurrentPlans runs several plans at once through the shared
// pool — the race detector guards the pool's busy/stats maps.
func TestExecutorConcurrentPlans(t *testing.T) {
	agent := &FuncAgent{
		AgentName:   "worker",
		Concurrency: 16,
		Exec: func(ctx context.Context, prompt string) (string, error) {
			time.Sleep(time.Millisecond)
			return prompt, nil
		},
	}
	pool := NewAgentPool()
	pool.Register(agent)
	exec := NewExecutor(Config{MaxParallel: 8, DefaultTimeout: time.Minute}, pool)

	var wg sync.WaitGroup
	for p := 0; p < 4; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			var tasks []*Task
			for i := 0; i < 4; i++ {
				tasks = append(tasks, &Task{ID: fmt.Sprintf("p%d-t%d", p, i), Prompt: "x"})
			}
			plan := exec.NewPlan(fmt.Sprintf("plan%d", p), tasks)
			if err := exec.Execute(context.Background(), plan); err != nil {
				t.Errorf("plan %d: %v", p, err)
			}
		}(p)
	}
	wg.Wait()
}
