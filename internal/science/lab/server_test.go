package lab

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLabWorkbenchParentOriginIsExact(t *testing.T) {
	if got := labWorkbenchOrigin("https://oasis.example/"); got != "https://oasis.example" {
		t.Fatalf("got %q", got)
	}
	for _, bad := range []string{"*", "javascript:alert(1)", "https://oasis.example/path", "https://oasis.example?q=x"} {
		if got := labWorkbenchOrigin(bad); got != "" {
			t.Fatalf("accepted %q as %q", bad, got)
		}
	}
}

func TestLabIndexInjectsWorkbenchParentOrigin(t *testing.T) {
	srv, err := New(Config{SciDir: t.TempDir(), Addr: "127.0.0.1:0", DisableFleetAutoConnect: true, WorkbenchOrigin: "https://oasis.example"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `window.__LUMEN_WORKBENCH_ORIGIN__="https://oasis.example"`) {
		t.Fatalf("index injection: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "https://oasis.example") {
		t.Fatalf("frame ancestor missing: %s", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestLabCORSOnlyAllowsConfiguredWorkbenchOrigin(t *testing.T) {
	srv, err := New(Config{SciDir: t.TempDir(), Addr: "127.0.0.1:0", DisableFleetAutoConnect: true, WorkbenchOrigin: "https://oasis.example"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ origin, want string }{
		{"https://oasis.example", "https://oasis.example"},
		{"https://tenant.oasisdata2026.xyz", ""},
		{"https://evil.example", ""},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/lab/health", nil)
		req.Header.Set("Origin", tc.origin)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tc.want {
			t.Errorf("origin %q: got %q want %q", tc.origin, got, tc.want)
		}
	}
}

func TestOasisEmbedRejectsForgedMessages(t *testing.T) {
	b, err := staticFS.ReadFile("static/oasis-embed.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, guard := range []string{"e.source === frame.contentWindow", "labOrigins.has(e.origin)"} {
		if !strings.Contains(s, guard) {
			t.Fatalf("embed message relay missing guard %q", guard)
		}
	}
}

func TestHealthEndpoint(t *testing.T) {
	sci := filepath.Join(t.TempDir(), "science")
	if err := os.MkdirAll(sci, 0o700); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{SciDir: sci, Addr: "127.0.0.1:19999", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	res, err := http.Get(ts.URL + "/api/lab/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body %v", body)
	}
	if port, ok := body["port"].(float64); !ok || int(port) != 19999 {
		t.Fatalf("port want 19999 got %v", body["port"])
	}
	pack, _ := body["research_pack"].(map[string]any)
	if pack == nil {
		t.Fatalf("missing research_pack: %v", body)
	}
	// seed_examples may be empty when pack not installed, but key must exist
	if _, ok := pack["seed_examples"]; !ok {
		t.Fatalf("research_pack.seed_examples missing: %v", pack)
	}
	if _, ok := body["ketcher"]; !ok {
		t.Fatalf("ketcher status missing: %v", body)
	}
	if _, ok := body["jupyter"]; !ok {
		t.Fatalf("jupyter status missing: %v", body)
	}
	if _, ok := body["onlyoffice"]; !ok {
		t.Fatalf("onlyoffice status missing: %v", body)
	}
}

func TestResolveKetcherDir(t *testing.T) {
	// Without install, may be empty; with third_party present from cwd, should find.
	dir := resolveKetcherDir(t.TempDir())
	// If developer tree has third_party/ketcher-standalone, dir is non-empty.
	if dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
			t.Fatalf("ketcher index missing: %v", err)
		}
	}
}

func TestArtifactsAPI(t *testing.T) {
	sci := filepath.Join(t.TempDir(), "science")
	srv, err := New(Config{SciDir: sci, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	create, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/projects", bytes.NewReader([]byte(`{"title":"Artifacts"}`)))
	create.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(create)
	if err != nil {
		t.Fatal(err)
	}
	var proj map[string]string
	_ = json.NewDecoder(res.Body).Decode(&proj)
	res.Body.Close()
	slug := proj["slug"]
	artRes, err := http.Get(ts.URL + "/api/lab/artifacts?project_id=" + slug)
	if err != nil {
		t.Fatal(err)
	}
	defer artRes.Body.Close()
	if artRes.StatusCode != http.StatusOK {
		t.Fatalf("status %d", artRes.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(artRes.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["artifacts"]; !ok {
		t.Fatalf("body %v", body)
	}
}

func TestCreateProjectAPI(t *testing.T) {
	sci := filepath.Join(t.TempDir(), "science")
	if err := os.MkdirAll(sci, 0o700); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{SciDir: sci, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/projects", bytes.NewReader([]byte(`{"title":"Smoke Test"}`)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
}
