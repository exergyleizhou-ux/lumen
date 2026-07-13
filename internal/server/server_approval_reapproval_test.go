package server

import (
	"bytes"
	"context"
	"encoding/json"
	"lumen/internal/approvalstate"
	"lumen/internal/control"
	"lumen/internal/permission"
	"lumen/internal/runstate"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEditedArgsInvalidateAndCreatePendingReplacement(t *testing.T) {
	store := approvalstate.NewMemoryStore()
	s, err := New(Config{Ctrl: control.New(), Runs: runstate.NewManager(nil), Approvals: store})
	if err != nil {
		t.Fatal(err)
	}
	oldArgs := json.RawMessage(`{"command":"echo old"}`)
	h, _ := approvalstate.HashArgs(oldArgs)
	store.Create(approvalstate.Approval{ID: "old", RunID: "run", StepID: "step", ToolCallID: "tc", Owner: runstate.LocalOwner, ArgsHash: h, EditableArgs: oldArgs, ExpiresAt: time.Now().Add(time.Minute)})
	wt := &approvalWaiter{ch: make(chan approvalDecision, 1), owner: runstate.LocalOwner, runID: "run", args: oldArgs}
	s.approvals.Store("old", wt)
	req := httptest.NewRequest(http.MethodPost, "/v1/approve", bytes.NewBufferString(`{"id":"old","allow":true,"args":{"command":"echo changed"}}`))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	newID, _ := body["id"].(string)
	if body["reapproval_required"] != true || newID == "old" {
		t.Fatalf("body=%v", body)
	}
	old, _ := store.Get(runstate.LocalOwner, "old")
	if old.Decision == nil || *old.Decision != approvalstate.DecisionInvalidated {
		t.Fatalf("old=%+v", old)
	}
	replacement, err := store.Get(runstate.LocalOwner, newID)
	if err != nil || replacement.Decision != nil || replacement.ArgsHash == h {
		t.Fatalf("replacement=%+v err=%v", replacement, err)
	}
	select {
	case <-wt.ch:
		t.Fatal("edited args faked approval")
	default:
	}
}
func TestReplacementApprovalExecutesNewGrantEndToEnd(t *testing.T) {
	store := approvalstate.NewMemoryStore()
	s, _ := New(Config{Ctrl: control.New(), Runs: runstate.NewManager(nil), Approvals: store})
	emitted := make(chan string, 1)
	execution := &permission.Execution{}
	ctx := permission.WithReview(runstate.WithRunID(context.Background(), "run"), permission.Review{StepID: "step", ToolCallID: "tc", Execution: execution})
	done := make(chan error, 1)
	go func() {
		ok, _, err := s.webApprover(runstate.LocalOwner, "run", func(_ string, p map[string]any) { emitted <- p["id"].(string) })(ctx, "bash", json.RawMessage(`{"command":"old"}`))
		if err == nil && !ok {
			err = approvalstate.ErrNotExecutable
		}
		done <- err
	}()
	oldID := <-emitted
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/approve", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w
	}
	first := call(`{"id":"` + oldID + `","allow":true,"args":{"command":"new"}}`)
	var payload map[string]any
	json.Unmarshal(first.Body.Bytes(), &payload)
	newID := payload["id"].(string)
	second := call(`{"id":"` + newID + `","allow":true}`)
	if second.Code != 200 {
		t.Fatalf("replacement approve %d %s", second.Code, second.Body.String())
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	old, _ := store.Get(runstate.LocalOwner, oldID)
	replacement, _ := store.Get(runstate.LocalOwner, newID)
	if old.Decision == nil || *old.Decision != approvalstate.DecisionInvalidated || replacement.ExecutionState != "consumed" {
		t.Fatalf("old=%+v new=%+v", old, replacement)
	}
	if execution.Complete == nil {
		t.Fatal("replacement completion missing")
	}
	if err := execution.Complete(true); err != nil {
		t.Fatal(err)
	}
}
