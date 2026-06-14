package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.DefaultModel != "deepseek-flash" {
		t.Errorf("default model: want deepseek-flash, got %s", cfg.DefaultModel)
	}
	if cfg.Agent.MaxSteps <= 0 {
		t.Errorf("maxSteps should be positive, got %d", cfg.Agent.MaxSteps)
	}
	if cfg.Agent.ContextWindow <= 0 {
		t.Errorf("contextWindow should be positive, got %d", cfg.Agent.ContextWindow)
	}
	if cfg.Permissions.Mode != "default" {
		t.Errorf("permission mode: want default, got %s", cfg.Permissions.Mode)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(''): %v", err)
	}
	if cfg == nil {
		t.Fatal("Load('') returned nil")
	}
	if cfg.DefaultModel != "deepseek-flash" {
		t.Errorf("default model mismatch: %s", cfg.DefaultModel)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/tmp/does-not-exist-12345.toml")
	if err != nil {
		t.Fatalf("Load(missing): %v", err)
	}
	if cfg == nil {
		t.Fatal("Load(missing) should return defaults, not nil")
	}
}

func TestLoadValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `default_model = "grok"
[[providers]]
name = "test"
kind = "openai"
base_url = "https://api.test.com"
model = "test-model"
api_key_env = "TEST_KEY"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("TEST_KEY", "sk-test-123")
	defer os.Unsetenv("TEST_KEY")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultModel != "grok" {
		t.Errorf("default_model: want grok, got %s", cfg.DefaultModel)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].APIKey != "sk-test-123" {
		t.Errorf("APIKey: want sk-test-123, got %s", cfg.Providers[0].APIKey)
	}
}

func TestFindConfig(t *testing.T) {
	// Should find ./lumen.toml when in project root
	// We can't guarantee cwd in test, but FindConfig should not panic
	path := FindConfig()
	t.Logf("FindConfig = %q", path)
	// Either it finds a file or returns ""
}

func TestUserConfigPath(t *testing.T) {
	path, err := UserConfigPath()
	if err != nil {
		t.Fatalf("UserConfigPath: %v", err)
	}
	if path == "" {
		t.Error("UserConfigPath should not be empty")
	}
	if filepath.Ext(path) != ".toml" {
		t.Errorf("config path should end in .toml, got %s", path)
	}
}

func TestIsValidSkillName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"", false},
		{"brainstorming", true},
		{"bug-hunt", true},
		{"docker-patterns", true},
		{"9invalid", false},          // starts with digit
		{"valid_name", true},
		{"too-long" + string(make([]byte, 60)), false},
		{"has space", false},
		{"camelCase", true},
	}
	for _, tt := range tests {
		got := IsValidSkillName(tt.name)
		if got != tt.valid {
			t.Errorf("IsValidSkillName(%q) = %v, want %v", tt.name, got, tt.valid)
		}
	}
}

func TestSkillNameKey(t *testing.T) {
	if SkillNameKey("Bug-Hunt") != "bug-hunt" {
		t.Errorf("SkillNameKey should lowercase, got %q", SkillNameKey("Bug-Hunt"))
	}
	if SkillNameKey("  TEST  ") != "test" {
		t.Errorf("SkillNameKey should trim+lowercase, got %q", SkillNameKey("  TEST  "))
	}
}

func TestCanonicalSkillPath(t *testing.T) {
	p := CanonicalSkillPath("/tmp/foo/../bar")
	if p != filepath.Clean("/tmp/bar") {
		t.Errorf("CanonicalSkillPath should clean, got %s", p)
	}
}

func TestConventionDirs(t *testing.T) {
	if len(ConventionDirs) == 0 {
		t.Error("ConventionDirs should not be empty")
	}
	// Must include .reasonix
	found := false
	for _, d := range ConventionDirs {
		if d == ".reasonix" {
			found = true
		}
	}
	if !found {
		t.Error("ConventionDirs should include .reasonix")
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	os.WriteFile(envPath, []byte("# a comment\n"+
		"DEEPSEEK_API_KEY=sk-from-dotenv\n"+
		"export GROK_API_KEY=\"xai-quoted\"\n"+
		"\n"+
		"ALREADY_SET=should-not-win\n"), 0o644)

	// Existing environment must win over the .env file.
	t.Setenv("ALREADY_SET", "from-environment")
	// Target vars must start truly unset so .env can fill them.
	for _, k := range []string{"DEEPSEEK_API_KEY", "GROK_API_KEY"} {
		orig, had := os.LookupEnv(k)
		os.Unsetenv(k)
		t.Cleanup(func() {
			if had {
				os.Setenv(k, orig)
			} else {
				os.Unsetenv(k)
			}
		})
	}

	if err := LoadDotEnv(envPath); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "sk-from-dotenv" {
		t.Errorf("DEEPSEEK_API_KEY: want sk-from-dotenv, got %q", got)
	}
	if got := os.Getenv("GROK_API_KEY"); got != "xai-quoted" {
		t.Errorf("GROK_API_KEY (export + quotes): want xai-quoted, got %q", got)
	}
	if got := os.Getenv("ALREADY_SET"); got != "from-environment" {
		t.Errorf("existing env must win: want from-environment, got %q", got)
	}
}

func TestLoadDotEnvMissingFileIsOK(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Errorf("missing .env should not error, got %v", err)
	}
}
