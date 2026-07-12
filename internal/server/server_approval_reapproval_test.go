package server

import (
	"bytes"
	"encoding/json"
	"lumen/internal/approvalstate"
	"lumen/internal/control"
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
