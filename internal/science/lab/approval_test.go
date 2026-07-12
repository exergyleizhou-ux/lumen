package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/approvalstate"
	"lumen/internal/permission"
	"lumen/internal/runstate"
	"net/http"
	"net/http/httptest"
)

func TestApprovalHubBlocksUntilResolve(t *testing.T) {
	mode := permission.ModeDefault
	h := newApprovalHub(func() permission.Mode { return mode })
	idCh := make(chan string, 1)
	emit := func(kind string, payload map[string]any) {
		if kind != "approval_request" {
			t.Errorf("kind %s", kind)
			return
		}
		id, _ := payload["id"].(string)
		select {
		case idCh <- id:
		default:
		}
	}
	done := make(chan bool, 1)
	go func() {
		ok, _, err := h.decide(context.Background(), "bash", json.RawMessage(`{}`), emit)
		if err != nil {
			t.Errorf("decide: %v", err)
		}
		done <- ok
	}()
	var gotID string
	select {
	case gotID = <-idCh:
	case <-time.After(2 * time.Second):
		t.Fatal("no approval id emitted")
	}
	if !h.resolve(gotID, true, nil) {
		t.Fatal("resolve failed")
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("expected allow")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting decide")
	}
}
func TestLabReplacementApprovalExecutesNewGrant(t *testing.T) {
	store := approvalstate.NewMemoryStore()
	h := newApprovalHub(func() permission.Mode { return permission.ModeDefault })
	h.store = store
	api := &API{approvals: h, approvalStore: store, approvalsTot: new(atomic.Uint64)}
	execution := &permission.Execution{}
	ctx := permission.WithReview(runstate.WithRunID(context.Background(), "run"), permission.Review{StepID: "step", ToolCallID: "tc", Execution: execution})
	ids := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		ok, _, err := h.decideOwned(ctx, runstate.LocalOwner, "bash", json.RawMessage(`{"command":"old"}`), func(_ string, p map[string]any) { ids <- p["id"].(string) })
		if err == nil && !ok {
			err = approvalstate.ErrNotExecutable
		}
		done <- err
	}()
	oldID := <-ids
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/lab/approve", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		api.handleApprove(w, r)
		return w
	}
	first := call(`{"id":"` + oldID + `","allow":true,"args":{"command":"new"}}`)
	var body map[string]any
	json.Unmarshal(first.Body.Bytes(), &body)
	newID := body["id"].(string)
	if w := call(`{"id":"` + newID + `","allow":true}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
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
		t.Fatal("completion missing")
	}
}

func TestApprovalHubRejectsCrossOwnerDecision(t *testing.T) {
	h := newApprovalHub(func() permission.Mode { return permission.ModeDefault })
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	b := runstate.Owner{UserID: "b", WorkspaceID: "w"}
	id := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = h.decideOwned(context.Background(), a, "bash", nil, func(_ string, p map[string]any) { id <- p["id"].(string) })
	}()
	approvalID := <-id
	if h.resolveOwned(b, approvalID, true, nil) {
		t.Fatal("cross-owner approval resolved")
	}
	if !h.resolveOwned(a, approvalID, false, nil) {
		t.Fatal("owner could not resolve approval")
	}
	<-done
}

func TestApprovalModesAreOwnerScoped(t *testing.T) {
	api := &API{modeMu: new(sync.Mutex), ownerModes: make(map[runstate.Owner]permission.Mode)}
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	b := runstate.Owner{UserID: "b", WorkspaceID: "w"}
	api.setOwnerMode(a, "bypass")
	api.setOwnerMode(b, "plan")
	if api.ownerMode(a) != permission.ModeBypass || api.ownerMode(b) != permission.ModePlan {
		t.Fatal("owner modes leaked")
	}
}

func TestApprovalHubPlanDenies(t *testing.T) {
	h := newApprovalHub(func() permission.Mode { return permission.ModePlan })
	ok, _, err := h.decide(context.Background(), "bash", nil, func(string, map[string]any) {})
	if err != nil || ok {
		t.Fatalf("plan should deny, ok=%v err=%v", ok, err)
	}
}

func TestApprovalHubBypassAllows(t *testing.T) {
	h := newApprovalHub(func() permission.Mode { return permission.ModeBypass })
	ok, _, err := h.decide(context.Background(), "bash", nil, func(string, map[string]any) {})
	if err != nil || !ok {
		t.Fatalf("bypass should allow, ok=%v err=%v", ok, err)
	}
}

func TestApprovalHubEditedArgs(t *testing.T) {
	h := newApprovalHub(func() permission.Mode { return permission.ModeDefault })
	idCh := make(chan string, 1)
	emit := func(kind string, payload map[string]any) {
		if kind == "approval_request" {
			idCh <- payload["id"].(string)
		}
	}
	done := make(chan struct{})
	var gotArgs json.RawMessage
	var ok bool
	var err error
	go func() {
		ok, gotArgs, err = h.decide(context.Background(), "bash", json.RawMessage(`{"command":"rm x"}`), emit)
		close(done)
	}()
	id := <-idCh
	edited := json.RawMessage(`{"command":"echo safe"}`)
	if !h.resolve(id, true, edited) {
		t.Fatal("resolve")
	}
	<-done
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if string(gotArgs) != string(edited) {
		t.Fatalf("args %s", gotArgs)
	}
}
