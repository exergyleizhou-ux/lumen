package control

import (
	"context"
	"encoding/json"
	"testing"
)

// Headless runs (no interactive approver set) must auto-approve — there is no
// human to prompt, and the guard layer still blocks dangerous commands. This is
// the behavior `lumen run`/eval depend on.
func TestApproveCallbackHeadlessAutoApproves(t *testing.T) {
	c := &Controller{}
	allow, newArgs, err := c.approveCallback()(context.Background(), "bash", nil)
	if err != nil || !allow {
		t.Fatalf("headless (no approver) should auto-approve, got allow=%v err=%v", allow, err)
	}
	if newArgs != nil {
		t.Fatalf("headless should not rewrite args: %s", newArgs)
	}
}

// When an interactive approver IS set (chat/tui), the gate must actually delegate
// to it — not silently auto-approve (the fictional-approval bug the review found).
func TestApproveCallbackDelegatesToInteractiveApprover(t *testing.T) {
	c := &Controller{}
	called := false
	c.SetApprover(func(ctx context.Context, tool string, args json.RawMessage) (bool, json.RawMessage, error) {
		called = true
		return false, nil, nil // user denied
	})
	allow, _, _ := c.approveCallback()(context.Background(), "bash", json.RawMessage(`{"command":"rm x"}`))
	if !called {
		t.Error("expected the interactive approver to be consulted")
	}
	if allow {
		t.Error("approver denied, so the gate must deny too — got allow=true")
	}
}

func TestApproveCallbackEditedArgs(t *testing.T) {
	c := &Controller{}
	edited := json.RawMessage(`{"command":"echo safe"}`)
	c.SetApprover(func(ctx context.Context, tool string, args json.RawMessage) (bool, json.RawMessage, error) {
		return true, edited, nil
	})
	allow, newArgs, err := c.approveCallback()(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /"}`))
	if err != nil || !allow {
		t.Fatalf("allow=%v err=%v", allow, err)
	}
	if string(newArgs) != string(edited) {
		t.Fatalf("newArgs %s", newArgs)
	}
}
