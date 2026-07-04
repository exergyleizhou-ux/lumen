package lab

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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
