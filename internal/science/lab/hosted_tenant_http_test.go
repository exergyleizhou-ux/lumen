package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"lumen/internal/event"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"lumen/internal/approvalstate"
	"lumen/internal/artifact"
	"lumen/internal/hostedauth"
	"lumen/internal/runstate"
)

func hostedLabToken(t *testing.T, secret, user, workspace string) string {
	t.Helper()
	now := time.Now()
	claims := hostedauth.Claims{
		UserID: user, WorkspaceID: workspace,
		Permissions: []string{"lab:run", "artifact:read", "run:read", "run:cancel", "approval:decide"},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: hostedauth.Issuer, Audience: jwt.ClaimStrings{hostedauth.Audience},
			Subject: user, ID: "session-" + user, IssuedAt: jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestHostedLabRunAndApprovalCrossOwnerMatrix(t *testing.T) {
	root, secret := t.TempDir(), "test-secret-that-is-at-least-32-bytes"
	t.Setenv(EnvHostedWorkspaceRoot, root)
	runs := runstate.NewManager(nil)
	s, err := New(Config{SciDir: t.TempDir(), Addr: "127.0.0.1:0", Hosted: true, WorkbenchJWTSecret: secret, DisableFleetAutoConnect: true, Runs: runs})
	if err != nil {
		t.Fatal(err)
	}
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	at := hostedLabToken(t, secret, "a", "w")
	bt := hostedLabToken(t, secret, "b", "w")
	run, err := runs.StartOwned(a, "s", "science", "private", "")
	if err != nil {
		t.Fatal(err)
	}
	runs.WrapSink(run.ID, event.Discard).Emit(event.Event{Kind: event.Text, Text: "test-secret-that-is-at-least-32-bytes"})
	runs.WrapSink(run.ID, event.Discard).Emit(event.Event{Kind: event.VerifyStarted})
	runs.WrapSink(run.ID, event.Discard).Emit(event.Event{Kind: event.VerifyResult, Level: event.LevelInfo, Text: "passed"})
	hash, _ := approvalstate.HashArgs([]byte(`{}`))
	if err := s.api.approvalStore.Create(approvalstate.Approval{ID: "snapshot-approval", RunID: run.ID, Owner: a, RiskLevel: "high", Reason: "SECRET", Command: "SECRET", EditableArgs: []byte(`{"key":"SECRET"}`), ArgsHash: hash, ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := s.api.artifactStore.(*artifact.MemoryStore).Put(artifact.Record{ID: "snapshot-artifact", RunID: run.ID, Owner: a, Name: "../../lab-report.txt", MIME: "text/plain", ObjectKey: "object"}, []byte("lab artifact bytes")); err != nil {
		t.Fatal(err)
	}
	ctx, cleanup := s.api.beginActiveRun(context.Background(), a, run.ID, time.Minute)
	defer cleanup()
	for _, path := range []string{"/api/lab/runs/" + run.ID, "/api/lab/runs/" + run.ID + "/events", "/api/lab/runs/" + run.ID + "/workbench-snapshot", "/api/lab/runs/" + run.ID + "/approvals", "/api/lab/runs/" + run.ID + "/artifacts/snapshot-artifact/download"} {
		if rec := labRequest(t, s, bt, http.MethodGet, path, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("B %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if rec := labRequest(t, s, bt, http.MethodPost, "/api/lab/runs/"+run.ID+"/cancel", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("B cancel: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-ctx.Done():
		t.Fatal("B canceled A")
	default:
	}
	if rec := labRequest(t, s, at, http.MethodGet, "/api/lab/runs/"+run.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("A get: %d", rec.Code)
	}
	if rec := labRequest(t, s, at, http.MethodGet, "/api/lab/runs/"+run.ID+"/workbench-snapshot", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"workspace_id":"w"`) || !strings.Contains(rec.Body.String(), `"last_seq":3`) || !strings.Contains(rec.Body.String(), `"pending_approvals":1`) || !strings.Contains(rec.Body.String(), `"verification":"passed"`) || !strings.Contains(rec.Body.String(), `"artifact_count":1`) || strings.Contains(rec.Body.String(), "test-secret-that-is-at-least-32-bytes") {
		t.Fatalf("A snapshot must be owner scoped and sanitized: %d %s", rec.Code, rec.Body.String())
	}
	if rec := labRequest(t, s, at, http.MethodGet, "/api/lab/runs/"+run.ID+"/approvals", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"risk_level":"high"`) || strings.Contains(rec.Body.String(), "SECRET") || strings.Contains(rec.Body.String(), "editable_args") {
		t.Fatalf("unsafe Lab approval review: %d %s", rec.Code, rec.Body.String())
	}
	if rec := labRequest(t, s, at, http.MethodGet, "/api/lab/runs/"+run.ID+"/artifacts/snapshot-artifact/download", nil); rec.Code != http.StatusOK || rec.Body.String() != "lab artifact bytes" || strings.Contains(rec.Header().Get("Content-Disposition"), "../") || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("Lab artifact download: %d %s %#v", rec.Code, rec.Body.String(), rec.Header())
	}
	if rec := labRequest(t, s, at, http.MethodGet, "/api/lab/runs/"+run.ID+"/artifacts/missing/download", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing Lab artifact: %d", rec.Code)
	}
	wt := &approvalWaiter{ch: make(chan approvalDecision, 1), owner: a}
	s.api.approvals.mu.Lock()
	s.api.approvals.waiters["appr-x"] = wt
	s.api.approvals.mu.Unlock()
	if rec := labRequest(t, s, bt, http.MethodPost, "/api/lab/approve", map[string]any{"id": "appr-x", "allow": true}); rec.Code != http.StatusNotFound {
		t.Fatalf("B approve: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-wt.ch:
		t.Fatal("B resolved A")
	default:
	}
	if rec := labRequest(t, s, at, http.MethodPost, "/api/lab/approve", map[string]any{"id": "appr-x", "allow": false}); rec.Code != http.StatusOK {
		t.Fatalf("A approve: %d", rec.Code)
	}
}

func TestLabWorkbenchVerificationSkippedIsNotRun(t *testing.T) {
	run := runstate.Run{Status: runstate.StatusSucceeded}
	got := labWorkbenchVerification([]event.Event{{Kind: event.VerifyStarted}, {Kind: event.VerifyResult, Level: event.LevelInfo, Text: "verify skipped — no build/test toolchain ran"}}, run)
	if got != "not_run" {
		t.Fatalf("got %q", got)
	}
}

func labRequest(t *testing.T, s *Server, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestHostedLabTenantHTTPMatrix(t *testing.T) {
	root, secret := t.TempDir(), "tenant-test-secret-that-is-at-least-32-bytes"
	t.Setenv(EnvHostedWorkspaceRoot, root)
	s, err := New(Config{SciDir: t.TempDir(), Addr: "127.0.0.1:0", Hosted: true, WorkbenchJWTSecret: secret, DisableFleetAutoConnect: true, Runs: runstate.NewManager(nil)})
	if err != nil {
		t.Fatal(err)
	}
	a := hostedLabToken(t, secret, "owner-a", "shared-workspace")
	b := hostedLabToken(t, secret, "owner-b", "shared-workspace")

	// Both owners may deliberately use the same public project ID and path;
	// their reads and mutations must still resolve to distinct durable roots.
	slugs := make([]string, 2)
	for i, tc := range []struct{ token, title string }{{a, "Alpha Project"}, {b, "Beta Project"}} {
		rec := labRequest(t, s, tc.token, http.MethodPost, "/api/lab/projects", map[string]any{"title": tc.title})
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s: %d %s", tc.title, rec.Code, rec.Body.String())
		}
		var created struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Slug == "" {
			t.Fatalf("decode create: %v %s", err, rec.Body.String())
		}
		slugs[i] = created.Slug
	}
	for _, tc := range []struct{ token, want, notWant string }{{a, "Alpha Project", "Beta Project"}, {b, "Beta Project", "Alpha Project"}} {
		rec := labRequest(t, s, tc.token, http.MethodGet, "/api/lab/projects", nil)
		if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(tc.want)) || bytes.Contains(rec.Body.Bytes(), []byte(tc.notWant)) {
			t.Fatalf("isolated list: %d %s", rec.Code, rec.Body.String())
		}
	}

	// Use the returned deterministic slugs and the same relative file path.
	for _, tc := range []struct{ token, slug, content string }{{a, slugs[0], "owner-a"}, {b, slugs[1], "owner-b"}} {
		path := "/api/lab/files/write?project_id=" + tc.slug
		rec := labRequest(t, s, tc.token, http.MethodPut, path, map[string]any{"path": "reports/shared.txt", "content": tc.content})
		if rec.Code != http.StatusOK {
			t.Fatalf("write %s: %d %s", tc.content, rec.Code, rec.Body.String())
		}
	}
	// Cross-owner IDs are indistinguishable from missing resources and cannot be mutated.
	if rec := labRequest(t, s, b, http.MethodDelete, "/api/lab/projects/"+slugs[0], nil); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := labRequest(t, s, a, http.MethodGet, "/api/lab/files/content?project_id="+slugs[1]+"&path=reports/shared.txt", nil); rec.Code < 400 {
		t.Fatalf("cross-owner file read: %d %s", rec.Code, rec.Body.String())
	}

	// Hosted path traversal and an existing parent symlink both fail closed.
	if rec := labRequest(t, s, a, http.MethodPut, "/api/lab/files/write?project_id="+slugs[0], map[string]any{"path": "../../escape", "content": "bad"}); rec.Code < 400 {
		t.Fatalf("traversal accepted: %d %s", rec.Code, rec.Body.String())
	}
	ta, err := s.api.tenants.acquire(labOwnerWith("owner-a", "shared-workspace"))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := ta.Projects.WorkspacePath(slugs[0])
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, "linked")); err != nil {
		t.Fatal(err)
	}
	s.api.tenants.release(ta.Owner)
	if rec := labRequest(t, s, a, http.MethodPut, "/api/lab/files/write?project_id="+slugs[0], map[string]any{"path": "linked/escape.txt", "content": "bad"}); rec.Code < 400 {
		t.Fatalf("symlink escape accepted: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside write: %v", err)
	}
}

func labOwnerWith(user, workspace string) runstate.Owner {
	return runstate.Owner{UserID: user, WorkspaceID: workspace}
}
