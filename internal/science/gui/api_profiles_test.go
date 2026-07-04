package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sciconfig "lumen/internal/science/config"
)

func TestHandleTemplates(t *testing.T) {
	srv, dir := testServer(t)
	_ = dir
	api := srv.api
	mux := http.NewServeMux()
	api.RegisterProfiles(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/templates")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	tpls, _ := body["templates"].([]any)
	if len(tpls) < 7 {
		t.Fatalf("templates %d", len(tpls))
	}
}

func TestHandleProfilesGET(t *testing.T) {
	srv, _ := testServer(t)
	mux := http.NewServeMux()
	srv.api.RegisterProfiles(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/profiles")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%d", resp.StatusCode)
	}
}

func TestHandleCheckUpdate(t *testing.T) {
	srv, _ := testServer(t)
	mux := http.NewServeMux()
	srv.api.RegisterProfiles(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/check-update")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%d", resp.StatusCode)
	}
}

func TestProfileJSONMasksKey(t *testing.T) {
	p := sciconfig.Profile{ID: "1", Name: "n", APIKey: "sk-abcdefghij"}
	j := profileJSON(p, true)
	if j["key_masked"] == "" {
		t.Fatal()
	}
	if j["active"] != true {
		t.Fatal()
	}
}

func TestHandleRelayModels(t *testing.T) {
	srv, _ := testServer(t)
	mux := http.NewServeMux()
	srv.api.RegisterProfiles(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	body, _ := json.Marshal(map[string]any{
		"base_url": "http://127.0.0.1:1", "api_key": "tok",
	})
	resp, err := http.Post(ts.URL+"/api/relay/models", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// unreachable upstream → 400 from handler
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad upstream, got %d", resp.StatusCode)
	}
}

func TestHandleRelayModelsMethodNotAllowed(t *testing.T) {
	srv, _ := testServer(t)
	mux := http.NewServeMux()
	srv.api.RegisterProfiles(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/relay/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("%d", resp.StatusCode)
	}
}

func TestHandleProfilesPOST(t *testing.T) {
	srv, sciDir := testServer(t)
	mux := http.NewServeMux()
	srv.api.sciDir = sciDir
	srv.api.RegisterProfiles(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	body, _ := json.Marshal(map[string]any{
		"name": "T", "template_id": "deepseek", "api_key": "sk-test1234567890",
	})
	resp, err := http.Post(ts.URL+"/api/profiles", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Truthful-save probes upstream; fake key should be rejected.
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid key, got %d", resp.StatusCode)
	}
}

func TestHandleProfilesPOSTRejectsEmptyKey(t *testing.T) {
	srv, _ := testServer(t)
	mux := http.NewServeMux()
	srv.api.RegisterProfiles(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	body, _ := json.Marshal(map[string]any{"name": "T", "template_id": "deepseek"})
	resp, err := http.Post(ts.URL+"/api/profiles", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("%d", resp.StatusCode)
	}
}
