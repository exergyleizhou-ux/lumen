package server

import (
	"bytes"
	"fmt"
	"lumen/internal/control"
	"lumen/internal/event"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalHTTPProviderOverridesNeverChangeEnvironment(t *testing.T) {
	before := append([]string(nil), os.Environ()...)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
		fmt.Fprintln(w)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lumen.toml")
	if err := os.WriteFile(cfgPath, []byte(`default_model="test"
[[providers]]
name="test"
kind="openai"
base_url="`+upstream.URL+`"
model="test"
api_key="startup"
`), 0600); err != nil {
		t.Fatal(err)
	}
	ctrl := control.New()
	if err := ctrl.Configure(event.Discard, nil, cfgPath); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{Ctrl: ctrl})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct{ path, body string }{{"/v1/chat", `{"prompt":"x","provider":"test","api_key":"local-secret"}`}, {"/v1/workflow", `{"action":"plan","prompt":"x","provider":"test","api_key":"local-secret"}`}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, endpoint.path, bytes.NewBufferString(endpoint.body))
		s.mux.ServeHTTP(rec, req)
	}
	after := os.Environ()
	if len(before) != len(after) {
		t.Fatalf("environment size changed")
	}
	m := map[string]bool{}
	for _, v := range before {
		m[v] = true
	}
	for _, v := range after {
		if !m[v] {
			t.Fatalf("environment changed: %s", v)
		}
	}
}
