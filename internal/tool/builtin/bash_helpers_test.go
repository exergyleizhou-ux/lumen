package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFilterLines(t *testing.T) {
	out := "error: boom\ninfo: ok\nerror: again\n"
	got := filterLines(out, "error")
	want := "error: boom\nerror: again"
	if got != want {
		t.Errorf("filterLines = %q, want %q", got, want)
	}
}

func TestFilterLinesNoMatch(t *testing.T) {
	if got := filterLines("a\nb\nc", "zzz"); got != "" {
		t.Errorf("no match should yield empty, got %q", got)
	}
}

// The background-job tools degrade with a clear error when no jobs manager is in
// context (the default for a bare ctx), rather than panicking.
func TestBashBackgroundWithoutJobsManager(t *testing.T) {
	bt := &BashTool{}
	_, err := bt.Execute(context.Background(), json.RawMessage(`{"command":"echo hi","run_in_background":true}`))
	if err == nil || !strings.Contains(err.Error(), "jobs manager") {
		t.Errorf("background bash without a jobs manager should error, got %v", err)
	}
}

func TestBashOutputRequiresJobID(t *testing.T) {
	bo := &BashOutputTool{}
	if _, err := bo.Execute(context.Background(), json.RawMessage(`{"job_id":""}`)); err == nil {
		t.Error("empty job_id should error")
	}
}

func TestBashOutputWithoutJobsManager(t *testing.T) {
	bo := &BashOutputTool{}
	_, err := bo.Execute(context.Background(), json.RawMessage(`{"job_id":"bash-1"}`))
	if err == nil || !strings.Contains(err.Error(), "jobs manager") {
		t.Errorf("bash_output without a jobs manager should error, got %v", err)
	}
}

func TestKillShellRequiresJobID(t *testing.T) {
	ks := &KillShellTool{}
	if _, err := ks.Execute(context.Background(), json.RawMessage(`{"job_id":""}`)); err == nil {
		t.Error("empty job_id should error")
	}
}

func TestWaitWithoutJobsManager(t *testing.T) {
	wt := &WaitTool{}
	_, err := wt.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "jobs manager") {
		t.Errorf("wait without a jobs manager should error, got %v", err)
	}
}
