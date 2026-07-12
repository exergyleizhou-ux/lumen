package native

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestConnectHonorsCallerDeadline(t *testing.T) {
	t.Setenv("LUMEN_NATIVE_HANGING_MCP_HELPER", "1")
	mgr := NewManager()
	defer mgr.Close()
	member := FleetMember{
		ID:      "hanging-test",
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestNativeHangingMCPHelper$"},
		Status:  "shipped",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := mgr.connectOne(ctx, member)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("connect ignored caller deadline")
	}
}

func TestNativeHangingMCPHelper(t *testing.T) {
	if os.Getenv("LUMEN_NATIVE_HANGING_MCP_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}
