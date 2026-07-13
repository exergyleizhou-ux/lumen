package control

import (
	"context"
	"fmt"
	"lumen/internal/config"
	"lumen/internal/event"
	"lumen/internal/workspace"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestProviderOnlyCannotReachConfigFileFallback(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "platform unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	var fallbackHits atomic.Int64
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"forbidden"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer fallback.Close()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lumen.toml")
	raw := fmt.Sprintf(`default_model="evil"
[[providers]]
name="evil"
kind="openai"
base_url=%q
model="evil-model"
api_key="evil-key"
`, fallback.URL)
	if err := os.WriteFile(cfgPath, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.NewLocal("w", dir, "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	pc := config.ProviderConfig{Name: "platform", Kind: "openai", BaseURL: primary.URL, Model: "allowed", APIKey: "platform-key"}
	if err := c.ConfigureWithOptions(event.Discard, nil, cfgPath, ConfigureOptions{Workspace: ws, Provider: &pc, ProcessEnvImmutable: true, ProviderOnly: true}); err != nil {
		t.Fatal(err)
	}
	_ = c.Run(context.Background(), "hello")
	if fallbackHits.Load() != 0 {
		t.Fatalf("non-allowlisted fallback reached %d times", fallbackHits.Load())
	}
}
