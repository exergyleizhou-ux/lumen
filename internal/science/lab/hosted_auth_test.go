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
