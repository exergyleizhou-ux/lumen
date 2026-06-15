package sandbox

import (
	"context"
	"testing"
)

func TestAvailable(t *testing.T) {
	avail := Available()
	t.Logf("sandbox available: %v", avail)
}

func TestCanSeatbelt(t *testing.T) {
	can := CanSeatbelt()
	t.Logf("seatbelt available: %v", can)
}

func TestCanDocker(t *testing.T) {
	can := CanDocker()
	t.Logf("docker available: %v", can)
}

func TestExecutorNative(t *testing.T) {
	e := NewExecutor(Config{Mode: ModeNone})
	out, err := e.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("native run: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("output: want 'hello\\n', got %q", string(out))
	}
}

func TestExecutorAuto(t *testing.T) {
	e := NewExecutor(Config{Mode: ModeAuto, WorkspaceRoot: "/tmp"})
	mode := e.Mode()
	t.Logf("auto-resolved mode: %s", mode)
}

func TestQuickProfile(t *testing.T) {
	profile := QuickProfile("/tmp/test")
	if profile == "" {
		t.Error("QuickProfile should return a valid profile string")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{Mode: ModeAuto}
	e := NewExecutor(cfg)
	if e.mode == "" {
		t.Error("executor should have a resolved mode")
	}
}
