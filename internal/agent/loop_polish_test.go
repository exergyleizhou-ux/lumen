package agent

import (
	"strings"
	"testing"

	"lumen/internal/provider"
)

// A verify failure with zero structured diagnostics (e.g. a pytest/jest failure
// the Go-oriented parser doesn't structure) must not show the contradictory
// "✗ test (0 diagnostics)" — say "✗ test failed" instead.
func TestVerifyFailText(t *testing.T) {
	if got := verifyFailText("test", 0); strings.Contains(got, "0 diagnostics") || !strings.Contains(got, "failed") {
		t.Errorf("zero diagnostics should read 'failed', got %q", got)
	}
	if got := verifyFailText("build", 3); !strings.Contains(got, "3 diagnostics") {
		t.Errorf("nonzero diagnostics should show the count, got %q", got)
	}
}

// recordRepeatSuccess must return the running count so the loop guard can act
// when the model rewrites identical content repeatedly (the map was previously
// written but never read).
func TestRecordRepeatSuccess_ReturnsIncrementingCount(t *testing.T) {
	a := &Agent{}
	tw := &testWriteTool{}
	call := provider.ToolCall{Name: "write_file", Arguments: `{"path":"x.txt","content":"same"}`}
	if n := a.recordRepeatSuccess(call, tw); n != 1 {
		t.Errorf("first identical write: got %d want 1", n)
	}
	if n := a.recordRepeatSuccess(call, tw); n != 2 {
		t.Errorf("second identical write: got %d want 2", n)
	}
	// a different write resets to its own count
	other := provider.ToolCall{Name: "write_file", Arguments: `{"path":"y.txt","content":"diff"}`}
	if n := a.recordRepeatSuccess(other, tw); n != 1 {
		t.Errorf("distinct write: got %d want 1", n)
	}
}
