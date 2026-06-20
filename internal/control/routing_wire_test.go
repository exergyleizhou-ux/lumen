package control

import (
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/event"
	"lumen/internal/modelpool"
)

func TestIsLoopbackURL(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:1234/v1":  true,
		"http://127.0.0.1:8000/v1":  true,
		"http://[::1]:1234/v1":      true,
		"https://api.openai.com/v1": false,
		"https://api.deepseek.com":  false,
		"":                          false,
	}
	for url, want := range cases {
		if got := isLoopbackURL(url); got != want {
			t.Errorf("isLoopbackURL(%q) = %v, want %v", url, got, want)
		}
	}
}

// TestConfigureRoutesMultipleProviders proves the wiring: with a local + a cloud
// provider configured, Configure routes the agent through a RoutingProvider.
func TestConfigureRoutesMultipleProviders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfgPath := filepath.Join(dir, "lumen.toml")
	cfg := `default_model = "local"

[[providers]]
name = "local"
kind = "openai"
base_url = "http://localhost:1234/v1"
model = "qwen3.6-27b"
api_key = "lm-studio"

[[providers]]
name = "cloud"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
model = "deepseek-chat"
api_key = "x"
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}

	c := New()
	if err := c.Configure(event.Discard, nil, cfgPath); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if _, ok := c.prov.(*modelpool.RoutingProvider); !ok {
		t.Errorf("c.prov = %T, want *modelpool.RoutingProvider", c.prov)
	}
	if len(c.fallbacks) != 0 {
		t.Errorf("fallbacks = %d, want 0 (failover moved into the router)", len(c.fallbacks))
	}
}

func TestConfigureSingleProviderNoRouter(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cfgPath := filepath.Join(dir, "lumen.toml")
	cfg := `default_model = "only"

[[providers]]
name = "only"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
model = "deepseek-chat"
api_key = "x"
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	c := New()
	if err := c.Configure(event.Discard, nil, cfgPath); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if _, ok := c.prov.(*modelpool.RoutingProvider); ok {
		t.Error("single provider should not be wrapped in a RoutingProvider")
	}
}
