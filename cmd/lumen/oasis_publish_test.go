package main

import (
	"encoding/json"
	"testing"

	"lumen/internal/oasis"
)

// The conveyor belt must hit the REAL marketplace admin endpoint
// (/api/v1/admin/compute/algorithms) — the old code POSTed to
// /api/compute/algorithms, which does not exist, so registration silently 404'd.
func TestOasisRegisterURL(t *testing.T) {
	cases := []struct{ base, want string }{
		{"http://localhost:8080", "http://localhost:8080/api/v1/admin/compute/algorithms"},
		{"http://localhost:8080/", "http://localhost:8080/api/v1/admin/compute/algorithms"},
		{"https://oasis.example.com/", "https://oasis.example.com/api/v1/admin/compute/algorithms"},
	}
	for _, c := range cases {
		if got := oasisRegisterURL(c.base); got != c.want {
			t.Errorf("oasisRegisterURL(%q) = %q, want %q", c.base, got, c.want)
		}
	}
}

func TestOasisReviewURL(t *testing.T) {
	got := oasisReviewURL("http://localhost:8080/", "abc-123")
	want := "http://localhost:8080/api/v1/admin/compute/algorithms/abc-123/review"
	if got != want {
		t.Errorf("oasisReviewURL = %q, want %q", got, want)
	}
}

func TestAlgoIDFromResponse(t *testing.T) {
	if got := algoIDFromResponse([]byte(`{"code":0,"message":"ok","data":{"id":"xyz-1","name":"a"}}`)); got != "xyz-1" {
		t.Errorf("algoIDFromResponse = %q, want xyz-1", got)
	}
	if got := algoIDFromResponse([]byte(`not json`)); got != "" {
		t.Errorf("bad json should yield empty id, got %q", got)
	}
}

// The admin endpoint binds params_schema as a JSON OBJECT (map[string]any). The
// old payload sent it as a quoted string, which 400'd. The payload must emit an
// object.
func TestBuildRegisterPayload(t *testing.T) {
	m := oasis.Manifest{Name: "a", Runtime: "docker", Image: "img", OutputKind: "model", Version: 2, ParamsSchema: `{"k":1}`}
	var got map[string]any
	if err := json.Unmarshal(buildRegisterPayload(m, "sha256:abc"), &got); err != nil {
		t.Fatalf("payload is not valid json: %v", err)
	}
	ps, ok := got["params_schema"].(map[string]any)
	if !ok {
		t.Fatalf("params_schema must be an object, got %T (%v)", got["params_schema"], got["params_schema"])
	}
	if ps["k"] != float64(1) {
		t.Errorf("params_schema.k = %v, want 1", ps["k"])
	}
	if got["image_digest"] != "sha256:abc" {
		t.Errorf("image_digest = %v", got["image_digest"])
	}
	// Empty/invalid params_schema must be omitted, not emitted as "" (which would 400).
	var got2 map[string]any
	json.Unmarshal(buildRegisterPayload(oasis.Manifest{Name: "b", Runtime: "docker", Image: "i", OutputKind: "model", Version: 1}, "sha256:x"), &got2)
	if _, present := got2["params_schema"]; present {
		t.Errorf("empty params_schema should be omitted, got %v", got2["params_schema"])
	}
}
