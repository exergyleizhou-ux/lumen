package lab

import (
	"bytes"
	"lumen/internal/config"
	"lumen/internal/event"
	"lumen/internal/runstate"
	"lumen/internal/science/lab/project"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestHostedLabRejectsTenantProviderConfiguration(t *testing.T) {
	root, secret := t.TempDir(), "secret"
	t.Setenv(EnvHostedWorkspaceRoot, root)
	s, err := New(Config{SciDir: t.TempDir(), Addr: "127.0.0.1:0", Hosted: true, WorkbenchJWTSecret: secret, DisableFleetAutoConnect: true, HostedProviders: []config.ProviderConfig{{Name: "platform", Kind: "openai", BaseURL: "http://127.0.0.1:1", Model: "allowed", APIKey: "platform-key"}}, Runs: runstate.NewManager(nil)})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/lab/config", bytes.NewBufferString(`{"api_key":"tenant-secret","base_url":"https://evil","model":"other"}`))
	req.Header.Set("Authorization", "Bearer "+hostedLabToken(t, secret, "u", "w"))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("provider_config_forbidden")) {
		t.Fatalf("response=%d %s", rec.Code, rec.Body.String())
	}
}

func TestHostedLabFreshControllersUseDistinctPlatformConfigsWithoutEnvironmentMutation(t *testing.T) {
	before := append([]string(nil), os.Environ()...)
	root := t.TempDir()
	var wg sync.WaitGroup
	got := make(chan *config.ProviderConfig, 2)
	for i, pc := range []config.ProviderConfig{{Name: "a", Kind: "openai", BaseURL: "http://127.0.0.1:1", Model: "m1", APIKey: "k1"}, {Name: "b", Kind: "openai", BaseURL: "http://127.0.0.1:2", Model: "m2", APIKey: "k2"}} {
		i, pc := i, pc
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir := filepath.Join(root, pc.Name)
			store := project.NewStore(dir)
			p, err := store.Create("Project "+pc.Name, "")
			if err != nil {
				t.Error(err)
				return
			}
			c := newControllerWithPlatformProvider(dir, nil, store, &pc, "/startup/bin")
			if err := c.Configure(p.Slug, "session", event.Discard, nil); err != nil {
				t.Error(err)
				return
			}
			got <- c.ProviderConfig()
			_ = i
		}()
	}
	wg.Wait()
	close(got)
	seen := map[string]string{}
	for pc := range got {
		seen[pc.Name] = pc.APIKey
	}
	if seen["a"] != "k1" || seen["b"] != "k2" {
		t.Fatalf("configs crossed: %#v", seen)
	}
	after := os.Environ()
	if len(before) != len(after) {
		t.Fatal("environment size changed")
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

func TestHostedLangGraphExclusivelyUsesStartupPlatformProvider(t *testing.T) {
	root, secret := t.TempDir(), "secret"
	t.Setenv(EnvHostedWorkspaceRoot, root)
	before := append([]string(nil), os.Environ()...)
	// A malicious tenant-local-looking configuration must be irrelevant in hosted mode.
	if err := os.WriteFile(filepath.Join(root, "science.json"), []byte(`{"api_key":"tenant-key","base_url":"https://evil","model":"evil"}`), 0600); err != nil {
		t.Fatal(err)
	}
	platform := config.ProviderConfig{Name: "platform", Kind: "openai", BaseURL: "https://platform.invalid/v1", Model: "allowed-model", APIKey: "platform-key"}
	s, err := New(Config{SciDir: root, Addr: "127.0.0.1:0", Hosted: true, WorkbenchJWTSecret: secret, DisableFleetAutoConnect: true, HostedProviders: []config.ProviderConfig{platform}, Runs: runstate.NewManager(nil)})
	if err != nil {
		t.Fatal(err)
	}
	pc := s.api.langGraphProvider()
	if pc == nil || pc.APIKey != "platform-key" || pc.BaseURL != platform.BaseURL || pc.Model != "allowed-model" {
		t.Fatalf("provider=%+v", pc)
	}
	after := os.Environ()
	if len(before) != len(after) {
		t.Fatal("environment changed")
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
