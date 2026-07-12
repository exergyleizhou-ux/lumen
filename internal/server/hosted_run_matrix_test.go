package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"lumen/internal/approvalstate"
	"lumen/internal/artifact"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/hostedauth"
	"lumen/internal/runstate"
)

func ownerToken(t *testing.T, secret, user string) string {
	t.Helper()
	now := time.Now()
	c := hostedauth.Claims{UserID: user, WorkspaceID: "w", Permissions: []string{"run:read", "run:cancel", "approval:decide", "artifact:read", "code:run"}, RegisteredClaims: jwt.RegisteredClaims{Issuer: hostedauth.Issuer, Audience: jwt.ClaimStrings{hostedauth.Audience}, Subject: user, ID: "s-" + user, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now.Add(-time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func authReq(s *Server, token, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestHostedRunAndApprovalCrossOwnerMatrix(t *testing.T) {
	secret := "secret"
	runs := runstate.NewManager(nil)
	s, err := New(Config{Ctrl: control.New(), Runs: runs, Hosted: true, WorkbenchJWTSecret: secret})
	if err != nil {
		t.Fatal(err)
	}
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	bToken := ownerToken(t, secret, "b")
	aToken := ownerToken(t, secret, "a")
	run, err := runs.StartOwned(a, "s", "code", "private", "")
	if err != nil {
		t.Fatal(err)
	}
	runs.WrapSink(run.ID, event.Discard).Emit(event.Event{Kind: event.Text, Text: "secret"})
	runs.WrapSink(run.ID, event.Discard).Emit(event.Event{Kind: event.VerifyStarted})
	runs.WrapSink(run.ID, event.Discard).Emit(event.Event{Kind: event.VerifyResult, Level: event.LevelInfo, Text: "passed"})
	hash, _ := approvalstate.HashArgs([]byte(`{}`))
	if err := s.approvalStore.Create(approvalstate.Approval{ID: "snapshot-approval", RunID: run.ID, Owner: a, RiskLevel: "high", Reason: "SECRET", Command: "SECRET", EditableArgs: []byte(`{"key":"SECRET"}`), ArgsHash: hash, ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.artifactStore.(*artifact.MemoryStore).Put(artifact.Record{ID: "snapshot-artifact", RunID: run.ID, Owner: a, Name: "../../report.txt", MIME: "text/plain", ObjectKey: "object"}, []byte("artifact bytes")); err != nil {
		t.Fatal(err)
	}
	ctx, cleanup := s.beginActiveRun(context.Background(), a, run.ID, time.Minute)
	defer cleanup()
	for _, path := range []string{"/v1/runs/" + run.ID, "/v1/runs/" + run.ID + "/events", "/v1/runs/" + run.ID + "/workbench-snapshot", "/v1/runs/" + run.ID + "/approvals", "/v1/runs/" + run.ID + "/artifacts/snapshot-artifact/download"} {
		if rec := authReq(s, bToken, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("B %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if rec := authReq(s, bToken, http.MethodPost, "/v1/runs/"+run.ID+"/cancel", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("B cancel: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-ctx.Done():
		t.Fatal("B canceled A")
	default:
	}
	if rec := authReq(s, aToken, http.MethodGet, "/v1/runs/"+run.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("A get: %d", rec.Code)
	}
	if rec := authReq(s, aToken, http.MethodGet, "/v1/runs/"+run.ID+"/workbench-snapshot", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"workspace_id":"w"`) || !strings.Contains(rec.Body.String(), `"last_seq":3`) || !strings.Contains(rec.Body.String(), `"pending_approvals":1`) || !strings.Contains(rec.Body.String(), `"verification":"passed"`) || !strings.Contains(rec.Body.String(), `"artifact_count":1`) || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("A snapshot must be owner scoped and sanitized: %d %s", rec.Code, rec.Body.String())
	}
	if rec := authReq(s, aToken, http.MethodGet, "/v1/runs/"+run.ID+"/approvals", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"risk_level":"high"`) || strings.Contains(rec.Body.String(), "SECRET") || strings.Contains(rec.Body.String(), "editable_args") {
		t.Fatalf("unsafe approval review: %d %s", rec.Code, rec.Body.String())
	}
	if rec := authReq(s, aToken, http.MethodGet, "/v1/runs/"+run.ID+"/artifacts/snapshot-artifact/download", ""); rec.Code != http.StatusOK || rec.Body.String() != "artifact bytes" || strings.Contains(rec.Header().Get("Content-Disposition"), "../") {
		t.Fatalf("artifact download: %d %s %#v", rec.Code, rec.Body.String(), rec.Header())
	}
	if rec := authReq(s, aToken, http.MethodGet, "/v1/runs/"+run.ID+"/artifacts/missing/download", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing artifact: %d", rec.Code)
	}
	wt := &approvalWaiter{ch: make(chan approvalDecision, 1), owner: a}
	s.approvals.Store("appr-x", wt)
	defer s.approvals.Delete("appr-x")
	if rec := authReq(s, bToken, http.MethodPost, "/v1/approve", `{"id":"appr-x","allow":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("B approve: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-wt.ch:
		t.Fatal("B resolved A approval")
	default:
	}
	if rec := authReq(s, aToken, http.MethodPost, "/v1/approve", `{"id":"appr-x","allow":false}`); rec.Code != http.StatusOK {
		t.Fatalf("A approve: %d", rec.Code)
	}
}

func TestWorkbenchVerificationSkippedIsNotRun(t *testing.T) {
	run := runstate.Run{Status: runstate.StatusSucceeded}
	got := workbenchVerification([]event.Event{{Kind: event.VerifyStarted}, {Kind: event.VerifyResult, Level: event.LevelInfo, Text: "verify skipped — no build/test toolchain ran"}}, run)
	if got != "not_run" {
		t.Fatalf("got %q", got)
	}
}
