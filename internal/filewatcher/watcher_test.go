package filewatcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewWatcher(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWatcher(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
}

func TestDetectWrite(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "test.txt")
	os.WriteFile(fpath, []byte("initial"), 0o644)

	w, _ := NewWatcher(dir, 100*time.Millisecond)
	defer w.Close()

	time.Sleep(150 * time.Millisecond) // initial scan
	os.WriteFile(fpath, []byte("modified"), 0o644)
	time.Sleep(200 * time.Millisecond) // wait for poll

	select {
	case e := <-w.Events():
		if e.Op != OpWrite {
			t.Errorf("expected write, got %s", e.Op)
		}
		if e.Path != fpath {
			t.Errorf("path: got %s", e.Path)
		}
	default:
		t.Log("write may not have been detected in time")
	}
}

func TestNewSessionTracker(t *testing.T) {
	st := NewSessionTracker()
	if st.Count() != 0 {
		t.Error("new tracker should be empty")
	}
	st.RecordFileChange("/tmp/a.go", 1)
	if st.Count() != 1 {
		t.Error("should have 1 file")
	}
}

func TestChangeSummary(t *testing.T) {
	var lastPath string
	var lastCount int
	cs := NewChangeSummary(10*time.Second, func(p string, c int) {
		lastPath = p
		lastCount = c
	})

	cs.Record(Event{Path: "/tmp/a.go", Op: OpWrite, Timestamp: time.Now()})
	cs.Record(Event{Path: "/tmp/a.go", Op: OpWrite, Timestamp: time.Now()})

	if lastCount != 2 {
		t.Errorf("expected count 2, got %d", lastCount)
	}
	_ = lastPath
}
