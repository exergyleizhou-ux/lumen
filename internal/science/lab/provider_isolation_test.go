package lab

import (
	"os"
	"sync"
	"testing"

	sciconfig "lumen/internal/science/config"
)

func TestScienceProviderConfigConcurrentIsolation(t *testing.T) {
	const sentinel = "unchanged-process-secret"
	t.Setenv("OPENAI_API_KEY", sentinel)
	configs := []sciconfig.File{
		{Provider: "custom-openai", ActiveProfileID: "a", Profiles: []sciconfig.Profile{{ID: "a", TemplateID: "custom-openai", APIKey: "sk-a-valid", BaseURL: "https://a.invalid/v1", Model: "model-a"}}},
		{Provider: "custom-openai", ActiveProfileID: "b", Profiles: []sciconfig.Profile{{ID: "b", TemplateID: "custom-openai", APIKey: "sk-b-valid", BaseURL: "https://b.invalid/v1", Model: "model-b"}}},
	}
	got := make([]string, 2)
	var wg sync.WaitGroup
	for i := range configs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pc, _, _, err := ScienceProviderConfig(configs[i])
			if err != nil {
				t.Errorf("resolve %d: %v", i, err)
				return
			}
			got[i] = pc.APIKey + "|" + pc.BaseURL + "|" + pc.Model
		}(i)
	}
	wg.Wait()
	if got[0] != "sk-a-valid|https://a.invalid/v1|model-a" || got[1] != "sk-b-valid|https://b.invalid/v1|model-b" {
		t.Fatalf("configs crossed: %#v", got)
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != sentinel {
		t.Fatalf("environment mutated: %q", v)
	}
}
