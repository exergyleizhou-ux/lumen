package control

import (
	"lumen/internal/config"
	"lumen/internal/event"
	"lumen/internal/workspace"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentControllersReceiveDistinctImmutableProviderConfigs(t *testing.T) {
	before := os.Environ()
	root := t.TempDir()
	var wg sync.WaitGroup
	got := make(chan *config.ProviderConfig, 2)
	for _, pc := range []config.ProviderConfig{
		{Name: "one", Kind: "openai", BaseURL: "http://127.0.0.1:1", Model: "m1", APIKey: "key-one"},
		{Name: "two", Kind: "openai", BaseURL: "http://127.0.0.1:2", Model: "m2", APIKey: "key-two"},
	} {
		pc := pc
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := New()
			ws, err := workspace.NewLocal(pc.Name, root, pc.Name, nil)
			if err != nil {
				t.Error(err)
				return
			}
			if err := c.ConfigureWithOptions(event.Discard, nil, "", ConfigureOptions{Workspace: ws, Provider: &pc, DataRoot: filepath.Join(root, pc.Name), ProcessEnvImmutable: true}); err != nil {
				t.Error(err)
				return
			}
			got <- c.ProviderConfig()
		}()
	}
	wg.Wait()
	close(got)
	seen := map[string]string{}
	for pc := range got {
		seen[pc.Name] = pc.APIKey
	}
	if seen["one"] != "key-one" || seen["two"] != "key-two" {
		t.Fatalf("configs crossed: %#v", seen)
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
