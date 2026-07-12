package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"lumen/internal/control"
	"lumen/internal/hostedauth"
)

func tenantToken(t *testing.T, user, workspaceID, session string) string {
	t.Helper()
	now := time.Now()
	c := hostedauth.Claims{UserID: user, WorkspaceID: workspaceID, Permissions: []string{"code:run", "run:read", "artifact:read"}, RegisteredClaims: jwt.RegisteredClaims{Issuer: hostedauth.Issuer, Audience: jwt.ClaimStrings{hostedauth.Audience}, Subject: user, ID: session, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now.Add(-time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func hostedCall(t *testing.T, s *Server, token, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestHostedCodeRoutesIsolateTenantFilesStateAndMetadata(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOSTED_WORKSPACE_ROOT", root)
	t.Setenv("LUMEN_DEMO", "1")
	s, err := New(Config{Ctrl: control.New(), Hosted: true, WorkbenchJWTSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	a, b := tenantToken(t, "alice", "shared", "session"), tenantToken(t, "bob", "shared", "session")
	for token, content := range map[string]string{a: "alice", b: "bob"} {
		rec := hostedCall(t, s, token, http.MethodPost, "/api/files/write", []byte(`{"path":"same.txt","content":"`+content+`"}`))
		if rec.Code != 200 {
			t.Fatalf("write: %d %s", rec.Code, rec.Body.String())
		}
		rec = hostedCall(t, s, token, http.MethodPost, "/v1/memories", []byte(`{"action":"save","entry":{"name":"same","title":"`+content+`"}}`))
		if rec.Code != 200 {
			t.Fatalf("memory: %d %s", rec.Code, rec.Body.String())
		}
	}
	for token, want := range map[string]string{a: "alice", b: "bob"} {
		rec := hostedCall(t, s, token, http.MethodGet, "/api/files/content?path=same.txt", nil)
		if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
			t.Fatalf("content %s: %d %s", want, rec.Code, rec.Body.String())
		}
		rec = hostedCall(t, s, token, http.MethodGet, "/v1/memories", nil)
		if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"title":"`+want+`"`)) {
			t.Fatalf("memory %s: %s", want, rec.Body.String())
		}
	}
	if rec := hostedCall(t, s, a, http.MethodPost, "/v1/mode", []byte(`{"mode":"plan"}`)); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	if rec := hostedCall(t, s, b, http.MethodGet, "/v1/mode", nil); rec.Code != 200 || bytes.Contains(rec.Body.Bytes(), []byte(`"ui":"plan"`)) {
		t.Fatalf("mode leaked: %s", rec.Body.String())
	}
	if rec := hostedCall(t, s, a, http.MethodPost, "/v1/workflow", []byte(`{"action":"workflow","prompt":"alice plan"}`)); rec.Code != 200 {
		t.Fatalf("workflow: %d %s", rec.Code, rec.Body.String())
	}
	if rec := hostedCall(t, s, b, http.MethodPost, "/v1/workflow", []byte(`{"action":"reject"}`)); rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("no plan")) {
		t.Fatalf("workflow leaked: %s", rec.Body.String())
	}
}

func TestHostedFilesRejectTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOSTED_WORKSPACE_ROOT", root)
	s, _ := New(Config{Ctrl: control.New(), Hosted: true, WorkbenchJWTSecret: "secret"})
	tok := tenantToken(t, "alice", "ws", "s")
	if rec := hostedCall(t, s, tok, http.MethodGet, "/api/files/content?path=../../outside", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("traversal status %d: %s", rec.Code, rec.Body.String())
	}
	tenant := filepath.Join(root, "alice", "ws")
	if err := os.MkdirAll(tenant, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tenant, "escape")); err != nil {
		t.Fatal(err)
	}
	if rec := hostedCall(t, s, tok, http.MethodGet, "/api/files/content?path=escape", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("symlink status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHostedSessionsUseTenantHistory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOSTED_WORKSPACE_ROOT", root)
	s, _ := New(Config{Ctrl: control.New(), Hosted: true, WorkbenchJWTSecret: "secret"})
	a, b := tenantToken(t, "a", "w", "s"), tenantToken(t, "b", "w", "s")
	for user, text := range map[string]string{"a": "alpha", "b": "beta"} {
		dir := filepath.Join(root, user, "w", ".lumen", "history")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"role": "user", "content": text})
		if err := os.WriteFile(filepath.Join(dir, "same.jsonl"), append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for tok, want := range map[string]string{a: "alpha", b: "beta"} {
		rec := hostedCall(t, s, tok, http.MethodGet, "/v1/sessions/content?name=same", nil)
		if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
			t.Fatalf("session %s: %d %s", want, rec.Code, rec.Body.String())
		}
	}
}
