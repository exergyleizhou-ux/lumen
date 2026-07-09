package compute

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTruncateOutput(t *testing.T) {
	s, trunc := truncateOutput("hello", 100)
	if trunc || s != "hello" {
		t.Fatalf("got %q trunc=%v", s, trunc)
	}
	big := make([]byte, 2000)
	for i := range big {
		big[i] = 'a'
	}
	s2, trunc2 := truncateOutput(string(big), 100)
	if !trunc2 || len(s2) > 100 {
		t.Fatalf("len=%d trunc=%v", len(s2), trunc2)
	}
}

func TestSubmitLocalFakeSSH(t *testing.T) {
	// Use a host that will fail fast with BatchMode — still exercises store paths.
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	j, err := store.SubmitOpts("127.0.0.1", "true", "", SubmitOpts{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if j.ID == "" || j.Status != "pending" {
		t.Fatalf("%+v", j)
	}
	// Wait for terminal state
	deadline := time.Now().Add(5 * time.Second)
	var last *Job
	for time.Now().Before(deadline) {
		last, err = store.Get(j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if last.Status == "done" || last.Status == "failed" || last.Status == "timeout" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if last == nil || (last.Status != "done" && last.Status != "failed" && last.Status != "timeout") {
		t.Fatalf("stuck: %+v", last)
	}
	// Persist file exists
	if _, err := os.Stat(filepath.Join(dir, ".lumen", "compute", j.ID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitRequiresHost(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	_, err := store.Submit("", "echo hi", "")
	if err == nil {
		t.Fatal("expected error")
	}
}
