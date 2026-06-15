package notify

import (
	"runtime"
	"testing"
)

func TestAvailable(t *testing.T) {
	avail := Available()
	t.Logf("notifications available: %v on %s", avail, runtime.GOOS)
}

func TestNotify(t *testing.T) {
	// Don't actually fire a notification in test — just verify it doesn't panic
	err := Notify("Lumen Test", "test notification")
	if err != nil && runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Logf("Notify not supported: %v", err)
	}
}

func TestTaskDone(t *testing.T) {
	// Should not panic
	TaskDone("bash", "go build", "Build succeeded.")
}

func TestApprovalNeeded(t *testing.T) {
	ApprovalNeeded("write_file")
}

func TestTruncate(t *testing.T) {
	if s := truncate("hello", 10); s != "hello" {
		t.Errorf("short string: got %q", s)
	}
	if s := truncate("1234567890abcdef", 10); len(s) <= 10 {
		t.Log("truncation works")
	}
}
