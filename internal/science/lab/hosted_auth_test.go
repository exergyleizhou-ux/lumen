package lab

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostedLabFailsClosedAndKeepsOnlyProbesPublic(t *testing.T) {
	if _, err := New(Config{SciDir: t.TempDir(), Addr: "127.0.0.1:0", Hosted: true, DisableFleetAutoConnect: true}); err == nil {
		t.Fatal("hosted lab accepted missing secret")
	}
	s, err := New(Config{SciDir: t.TempDir(), Addr: "127.0.0.1:0", Hosted: true, WorkbenchJWTSecret: "secret", DisableFleetAutoConnect: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/lab/health", "/api/lab/readyz"} {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized {
			t.Fatalf("probe %s protected", path)
		}
	}
	for _, path := range []string{"/api/lab/projects", "/api/lab/files", "/api/lab/chat"} {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("business %s status %d", path, rec.Code)
		}
	}
}

func TestLabPermissionByOperation(t *testing.T) {
	cases := []struct{ method, path, want string }{
		{http.MethodPost, "/api/lab/chat", "lab:run"},
		{http.MethodGet, "/api/lab/runs/run-1", "run:read"},
		{http.MethodPost, "/api/lab/runs/run-1/cancel", "run:cancel"},
		{http.MethodPost, "/api/lab/approve", "approval:decide"},
		{http.MethodGet, "/api/lab/artifacts", "artifact:read"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if got := labPermission(r); got != tc.want {
			t.Errorf("%s %s: got %s want %s", tc.method, tc.path, got, tc.want)
		}
	}
}
