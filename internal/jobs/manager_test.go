package jobs

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartAndGet(t *testing.T) {
	m := NewManager()

	release := make(chan struct{})
	job := m.Start("bash", "test command", func(ctx context.Context) (string, error) {
		<-release // stay running until the assertions below have run
		return "hello", nil
	})

	if job.ID == "" {
		t.Error("job should have an ID")
	}
	if st := job.statusSafe(); st != StatusRunning {
		t.Errorf("new job should be running, got %s", st)
	}
	close(release)

	got := m.Get(job.ID)
	if got == nil {
		t.Error("Get should return the job")
	}
	if got.Label != "test command" {
		t.Errorf("label: want 'test command', got %q", got.Label)
	}
}

func TestStartAndWait(t *testing.T) {
	m := NewManager()

	job := m.Start("bash", "echo", func(ctx context.Context) (string, error) {
		time.Sleep(10 * time.Millisecond)
		return "done", nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result, err := m.Wait(ctx, job.ID)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result != "done" {
		t.Errorf("result: want 'done', got %q", result)
	}

	// Job should be removed after Wait
	if m.Get(job.ID) != nil {
		t.Error("job should be removed after Wait")
	}
}

func TestKill(t *testing.T) {
	m := NewManager()

	job := m.Start("bash", "long running", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	time.Sleep(10 * time.Millisecond) // let it start

	killed := m.Kill(job.ID)
	if killed == nil {
		t.Error("Kill should return the job")
	}
	if st := killed.statusSafe(); st != StatusKilled {
		t.Errorf("killed job status: want killed, got %s", st)
	}

	// Job should be removed
	if m.Get(job.ID) != nil {
		t.Error("job should be removed after Kill")
	}
}

func TestKillUnknown(t *testing.T) {
	m := NewManager()
	killed := m.Kill("nonexistent")
	if killed != nil {
		t.Error("Kill on unknown ID should return nil")
	}
}

func TestWaitUnknown(t *testing.T) {
	m := NewManager()
	ctx := context.Background()
	_, err := m.Wait(ctx, "nonexistent")
	if err == nil {
		t.Error("Wait on unknown ID should error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestWaitContextCancel(t *testing.T) {
	m := NewManager()

	job := m.Start("bash", "slow", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := m.Wait(ctx, job.ID)
	if err == nil {
		t.Error("Wait should error when context is cancelled")
	}
}

func TestList(t *testing.T) {
	m := NewManager()

	m.Start("bash", "a", func(ctx context.Context) (string, error) {
		time.Sleep(50 * time.Millisecond)
		return "a", nil
	})
	m.Start("task", "b", func(ctx context.Context) (string, error) {
		time.Sleep(50 * time.Millisecond)
		return "b", nil
	})

	jobs := m.List()
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestClean(t *testing.T) {
	m := NewManager()

	job := m.Start("bash", "fast", func(ctx context.Context) (string, error) {
		return "fast", nil
	})

	ctx := context.Background()
	m.Wait(ctx, job.ID) // removes it

	// Job should already be gone
	if m.Get(job.ID) != nil {
		t.Error("job should be gone after Wait")
	}

	// Clean on empty manager should not panic
	m.Clean()
}

func TestOutputWait(t *testing.T) {
	m := NewManager()

	job := m.Start("bash", "output test", func(ctx context.Context) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return "output", nil
	})

	// Running: no output yet
	output, done := m.OutputWait(job.ID)
	if done {
		t.Error("OutputWait should report not done while running")
	}
	_ = output

	// Wait for completion
	ctx := context.Background()
	m.Wait(ctx, job.ID)
}

func TestConcurrentJobs(t *testing.T) {
	m := NewManager()
	n := 20

	for i := 0; i < n; i++ {
		m.Start("bash", "concurrent", func(ctx context.Context) (string, error) {
			time.Sleep(5 * time.Millisecond)
			return "ok", nil
		})
	}

	jobs := m.List()
	if len(jobs) != n {
		t.Errorf("expected %d concurrent jobs, got %d", n, len(jobs))
	}
}

func TestWithManagerContext(t *testing.T) {
	m := NewManager()
	ctx := WithManager(context.Background(), m)

	got := FromContext(ctx)
	if got != m {
		t.Error("FromContext should return the same manager")
	}

	got2 := FromContext(context.Background())
	if got2 != nil {
		t.Error("FromContext should return nil without WithManager")
	}
}

func TestJobSnapshotRaceWithCompletion(t *testing.T) {
	// A background job completing (writing Status/Result/Err) must not race
	// readers (bash_output's Snapshot) or a concurrent Kill. Run under -race.
	m := NewManager()
	release := make(chan struct{})
	job := m.Start("bash", "x", func(ctx context.Context) (string, error) {
		<-release
		return "done-output", nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, _, _ = job.Snapshot()
			}
		}()
	}
	close(release) // job completes → completion goroutine writes fields concurrently
	wg.Wait()
}
