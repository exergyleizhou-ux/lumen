package hostedauth

import (
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareAuthenticatesAndDoesNotLeakToken(t *testing.T) {
	v, _ := NewVerifier("test-secret-that-is-at-least-32-bytes")
	raw := signed(t, "test-secret-that-is-at-least-32-bytes", jwt.SigningMethodHS256, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok || id.UserID != "user-1" {
			t.Fatal("identity missing")
		}
		w.WriteHeader(204)
	})
	req := httptest.NewRequest("GET", "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	v.Middleware(next).ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("status %d", rec.Code)
	}
	bad := httptest.NewRequest("GET", "/v1/status", nil)
	bad.Header.Set("Authorization", "Bearer secret-token-value")
	rec = httptest.NewRecorder()
	v.Middleware(next).ServeHTTP(rec, bad)
	if rec.Code != 401 || strings.Contains(rec.Body.String(), "secret-token-value") {
		t.Fatalf("unsafe response: %d %q", rec.Code, rec.Body.String())
	}
}

func TestMiddlewareEnforcesPermission(t *testing.T) {
	v, _ := NewVerifier("test-secret-that-is-at-least-32-bytes")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", nil)
	req.Header.Set("Authorization", "Bearer "+signed(t, "test-secret-that-is-at-least-32-bytes", jwt.SigningMethodHS256, nil))
	rec := httptest.NewRecorder()
	v.Require("lab:run")(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "workbench_forbidden") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}
