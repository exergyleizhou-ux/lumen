package config

import "testing"

func TestLocalPresetsExist(t *testing.T) {
	// LM Studio, Ollama, and vLLM all speak the OpenAI wire protocol on a
	// localhost endpoint and accept an empty or placeholder API key. They are
	// the foundation for running lumen against a free local model.
	want := map[string]struct {
		baseURL string
	}{
		"lmstudio": {baseURL: "http://localhost:1234/v1"},
		"ollama":   {baseURL: "http://localhost:11434/v1"},
		"vllm":     {baseURL: "http://localhost:8000/v1"},
		"exo":      {baseURL: "http://localhost:52415/v1"},
	}

	for name, exp := range want {
		p := FindPreset(name)
		if p == nil {
			t.Fatalf("preset %q not found", name)
		}
		if p.Kind != "openai" {
			t.Errorf("%s: kind = %q, want openai", name, p.Kind)
		}
		if p.BaseURL != exp.baseURL {
			t.Errorf("%s: base_url = %q, want %q", name, p.BaseURL, exp.baseURL)
		}
		if !p.IsLocal() {
			t.Errorf("%s: IsLocal() = false, want true", name)
		}
	}
}

func TestLocalPresetsReturnsOnlyLocal(t *testing.T) {
	local := LocalPresets()
	if len(local) == 0 {
		t.Fatal("LocalPresets() returned none")
	}
	for _, p := range local {
		if !p.IsLocal() {
			t.Errorf("LocalPresets() included non-local preset %q (base_url %q)", p.Name, p.BaseURL)
		}
	}
	// A cloud preset must never be classified as local.
	if gpt := FindPreset("gpt-4o"); gpt != nil && gpt.IsLocal() {
		t.Error("gpt-4o classified as local")
	}
}

func TestIsLocalDetectsLoopbackHosts(t *testing.T) {
	cases := []struct {
		baseURL string
		local   bool
	}{
		{"http://localhost:1234/v1", true},
		{"http://127.0.0.1:8000/v1", true},
		{"http://[::1]:1234/v1", true},
		{"https://api.openai.com/v1", false},
		{"https://api.deepseek.com/v1", false},
	}
	for _, c := range cases {
		p := ModelPreset{BaseURL: c.baseURL}
		if got := p.IsLocal(); got != c.local {
			t.Errorf("IsLocal(%q) = %v, want %v", c.baseURL, got, c.local)
		}
	}
}
