package registry

import (
	"testing"
)

func TestTaskRegistryCreate(t *testing.T) {
	r := NewTaskRegistry()
	task := r.Create("test task", "description")
	if task.ID == "" {
		t.Error("task should have an ID")
	}
	if task.Status != TaskPending {
		t.Errorf("new task should be pending, got %s", task.Status)
	}
}

func TestTaskRegistryGet(t *testing.T) {
	r := NewTaskRegistry()
	task := r.Create("get test", "")
	got, ok := r.Get(task.ID)
	if !ok {
		t.Fatal("Get should find the task")
	}
	if got.Name != "get test" {
		t.Errorf("name mismatch: %s", got.Name)
	}
	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("Get should return false for missing task")
	}
}

func TestTaskRegistryUpdateStatus(t *testing.T) {
	r := NewTaskRegistry()
	task := r.Create("status test", "")
	r.UpdateStatus(task.ID, TaskRunning)
	got, _ := r.Get(task.ID)
	if got.Status != TaskRunning {
		t.Errorf("status should be running, got %s", got.Status)
	}
}

func TestTaskRegistryStop(t *testing.T) {
	r := NewTaskRegistry()
	task := r.Create("stop test", "")
	r.Stop(task.ID)
	got, _ := r.Get(task.ID)
	if got.Status != TaskCancelled {
		t.Errorf("stopped task should be cancelled, got %s", got.Status)
	}
}

func TestTeamRegistryCreate(t *testing.T) {
	r := NewTeamRegistry()
	team := r.Create("frontend", "Frontend team", []string{"alice", "bob"})
	if team.Name != "frontend" {
		t.Errorf("name mismatch: %s", team.Name)
	}
	got, ok := r.Get("frontend")
	if !ok || len(got.Members) != 2 {
		t.Error("Get should return team with members")
	}
}

func TestTeamRegistryDelete(t *testing.T) {
	r := NewTeamRegistry()
	r.Create("temp-team", "", nil)
	r.Delete("temp-team")
	_, ok := r.Get("temp-team")
	if ok {
		t.Error("deleted team should not be found")
	}
}

func TestCronRegistryCreate(t *testing.T) {
	r := NewCronRegistry()
	job := r.Create("daily build", "0 9 * * *", "go build ./...")
	if job.ID == "" || !job.Enabled {
		t.Error("new cron job should have ID and be enabled")
	}
}

func TestCronRegistryEnableDisable(t *testing.T) {
	r := NewCronRegistry()
	job := r.Create("toggle test", "* * * * *", "echo")
	r.Enable(job.ID, false)
	got, _ := r.Get(job.ID)
	if got.Enabled {
		t.Error("job should be disabled")
	}
	r.Enable(job.ID, true)
	got, _ = r.Get(job.ID)
	if !got.Enabled {
		t.Error("job should be enabled")
	}
}
