package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lumen/internal/control"
	"lumen/internal/runstate"
)

func TestCodeHealthAndLocalReadinessAreSeparate(t *testing.T) {
	s, err := New(Config{Ctrl: control.New(), Runs: runstate.NewManager(nil)})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/healthz", `"status":"ok"`},
		{"/readyz", `"ready":true`},
	} {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.body) {
			t.Fatalf("%s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestHostedReadinessFailsClosedWithoutDependenciesAndIsSanitized(t *testing.T) {
	t.Setenv("WORKBENCH_CONTROL_PLANE_URL", "http://127.0.0.1:1/private-secret")
	s, err := New(Config{
		Ctrl:                control.New(),
		Runs:                runstate.NewManager(nil),
		Hosted:              true,
		WorkbenchJWTSecret:  "test-secret-that-is-at-least-32-bytes",
		HostedWorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, name := range []string{"database", "object_storage", "quota_control_plane", "provider"} {
		if !strings.Contains(rec.Body.String(), `"`+name+`":false`) {
			t.Fatalf("missing failed check %q: %s", name, rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), "private-secret") {
		t.Fatalf("readiness leaked dependency details: %s", rec.Body.String())
	}
}
