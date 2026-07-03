package main

import (
	"os"
	"strings"
	"testing"

	"lumen/internal/config"
)

func TestParseRunArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantPlan   bool
		wantMode   string
		wantPrompt string
	}{
		{"plain prompt", []string{"fix", "the", "bug"}, false, "", "fix the bug"},
		{"plan flag first", []string{"--plan", "look", "around"}, true, "", "look around"},
		{"mode flag first", []string{"--mode", "accept-edits", "do", "it"}, false, "accept-edits", "do it"},
		{"mode flag mid", []string{"do", "--mode", "plan", "it"}, false, "plan", "do it"},
		{"both flags", []string{"--plan", "--mode", "bypass", "go"}, true, "bypass", "go"},
		{"mode without value", []string{"--mode"}, false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, mode, prompt := parseRunArgs(c.args)
			if plan != c.wantPlan || mode != c.wantMode || prompt != c.wantPrompt {
				t.Errorf("parseRunArgs(%v) = (%v, %q, %q), want (%v, %q, %q)",
					c.args, plan, mode, prompt, c.wantPlan, c.wantMode, c.wantPrompt)
			}
		})
	}
}

// ── config command ─────────────────────────────────────────

func TestConfigSummaryDefaults(t *testing.T) {
	// configSummary should work with no config file present
	// (uses defaults). Must not panic or error.
	summary := configSummary()
	if !strings.Contains(summary, "config:(defaults)") {
		t.Errorf("expected defaults marker, got: %s", summary)
	}
	if !strings.Contains(summary, "model:") {
		t.Errorf("expected model in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "mode:") {
		t.Errorf("expected mode in summary, got: %s", summary)
	}
}

func TestConfigSummaryWithFile(t *testing.T) {
	dir := t.TempDir()
	tomlPath := dir + "/lumen.toml"
	content := `
default_model = "gpt-4o"

[[providers]]
name = "gpt-4o"
kind = "openai"
model = "gpt-4o"
api_key_env = "OPENAI_API_KEY"

[[providers]]
name = "deepseek-chat"
kind = "openai"
model = "deepseek-chat"
api_key_env = "DEEPSEEK_API_KEY"

[permissions]
mode = "bypass"
`
	if err := os.WriteFile(tomlPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Temporarily override CWD so FindConfig picks up our test file
	origWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origWd)

	// Isolate from host env keys so missing-key warnings are deterministic.
	for _, k := range []string{"OPENAI_API_KEY", "DEEPSEEK_API_KEY"} {
		t.Setenv(k, "")
	}

	summary := configSummary()
	if !strings.Contains(summary, "config:") {
		t.Errorf("expected config path in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "model:gpt-4o") {
		t.Errorf("expected model:gpt-4o, got: %s", summary)
	}
	if !strings.Contains(summary, "providers:2") {
		t.Errorf("expected providers:2, got: %s", summary)
	}
	if !strings.Contains(summary, "mode:bypass") {
		t.Errorf("expected mode:bypass, got: %s", summary)
	}
	if !strings.Contains(summary, "!gpt-4o:no-key") {
		t.Errorf("expected missing key warning, got: %s", summary)
	}
}

func TestKeySource(t *testing.T) {
	tests := []struct {
		name   string
		p      config.ProviderConfig
		want   string
	}{
		{"from env var", config.ProviderConfig{APIKey: "sk-xxx", APIKeyEnv: "MY_KEY"}, "env:MY_KEY"},
		{"from config file", config.ProviderConfig{APIKey: "sk-xxx", APIKeyEnv: ""}, "config file"},
		{"unset env var", config.ProviderConfig{APIKey: "", APIKeyEnv: "MISSING_KEY"}, "env:MISSING_KEY (unset)"},
		{"no key", config.ProviderConfig{APIKey: "", APIKeyEnv: ""}, "no key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keySource(tt.p)
			if got != tt.want {
				t.Errorf("keySource(%+v) = %q, want %q", tt.p, got, tt.want)
			}
		})
	}
}
