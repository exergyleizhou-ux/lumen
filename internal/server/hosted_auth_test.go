package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"lumen/internal/control"
	"lumen/internal/hostedauth"
)

func serverToken(t *testing.T, secret string) string {
	t.Helper()
	now := time.Now()
	c := hostedauth.Claims{UserID: "user-1", WorkspaceID: "workspace-1", Permissions: []string{"code:run", "lab:run", "run:read", "run:cancel", "approval:decide", "artifact:read"}, RegisteredClaims: jwt.RegisteredClaims{Issuer: hostedauth.Issuer, Audience: jwt.ClaimStrings{hostedauth.Audience}, Subject: "user-1", ID: "session-1", IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now.Add(-time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestHostedProviderOverridesRejectedConcurrentlyWithoutEnvironmentMutation(t *testing.T) {
	t.Setenv("HOSTED_WORKSPACE_ROOT", t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "sentinel")
	s, err := New(Config{Ctrl: control.New(), Hosted: true, WorkbenchJWTSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	token := serverToken(t, "secret")
	bodies := []string{`{"prompt":"a","provider":"deepseek","api_key":"a"}`, `{"prompt":"b","provider":"qwen","model":"b"}`}
	var wg sync.WaitGroup
	for _, body := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewBufferString(body))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status %d: %s", rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Wait()
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "sentinel" {
		t.Fatalf("environment mutated: %q", got)
	}
}

func TestHostedServerFailsClosedAndProtectsBusinessRoutes(t *testing.T) {
	if _, err := New(Config{Ctrl: control.New(), Hosted: true}); err == nil {
		t.Fatal("hosted server accepted missing secret")
	}
	s, err := New(Config{Ctrl: control.New(), Hosted: true, WorkbenchJWTSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/status", "/api/files"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status %d", path, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/chat", nil)
	req.Header.Set("Authorization", "Bearer "+serverToken(t, "secret"))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("valid identity rejected")
	}
}

func TestCodePermissionByOperation(t *testing.T) {
	cases := []struct{ method, path, want string }{
		{http.MethodPost, "/v1/chat", "code:run"},
		{http.MethodGet, "/v1/runs/run-1", "run:read"},
		{http.MethodPost, "/v1/runs/run-1/cancel", "run:cancel"},
		{http.MethodPost, "/v1/approve", "approval:decide"},
		{http.MethodGet, "/api/files/content", "artifact:read"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if got := codePermission(r); got != tc.want {
			t.Errorf("%s %s: got %s want %s", tc.method, tc.path, got, tc.want)
		}
	}
}
