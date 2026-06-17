// Package config resolves provider and agent configuration from TOML files,
// environment variables, and CLI flags.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ConventionDirs are the directory names scanned for skills and memory files,
// in priority order. Compatible with Reasonix, Claude Code, and other agents.
var ConventionDirs = []string{".reasonix", ".agents", ".agent", ".claude"}

// File is the decoded lumen.toml.
type File struct {
	DefaultModel string             `toml:"default_model"`
	Providers    []ProviderConfig   `toml:"providers"`
	Coordinator  *CoordinatorConfig `toml:"coordinator"`
	Agent        AgentConfig        `toml:"agent"`
	Permissions  PermissionsConfig  `toml:"permissions"`
	Skills       SkillsConfig       `toml:"skills"`
}

// ProviderConfig is one model provider entry.
type ProviderConfig struct {
	Name      string `toml:"name"`
	Kind      string `toml:"kind"`
	BaseURL   string `toml:"base_url"`
	Model     string `toml:"model"`
	APIKeyEnv string `toml:"api_key_env"`
	APIKey    string `toml:"api_key,omitempty"` // from toml or resolved from env
}

// CoordinatorConfig configures the two-model planner+executor mode.
type CoordinatorConfig struct {
	Planner  string `toml:"planner"`
	Executor string `toml:"executor"`
}

// AgentConfig tunes the agent loop.
type AgentConfig struct {
	MaxSteps          int     `toml:"max_steps"`
	Temperature       float64 `toml:"temperature"`
	ContextWindow     int     `toml:"context_window"`
	SoftCompactRatio  float64 `toml:"soft_compact_ratio"`
	CompactRatio      float64 `toml:"compact_ratio"`
	CompactForceRatio float64 `toml:"compact_force_ratio"`
}

// PermissionsConfig controls the tool-call permission gate.
type PermissionsConfig struct {
	Mode string `toml:"mode"` // "default", "accept-edits", "bypass", "plan"
}

// SkillsConfig tunes skill discovery.
type SkillsConfig struct {
	MaxDepth int `toml:"max_depth"`
}

// Load reads and validates the config file at path. Returns defaults when path
// is empty or the file doesn't exist.
func Load(path string) (*File, error) {
	cfg := defaults()
	if path == "" {
		return cfg, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	// Resolve env vars
	for i := range cfg.Providers {
		if cfg.Providers[i].APIKeyEnv != "" && cfg.Providers[i].APIKey == "" {
			cfg.Providers[i].APIKey = os.Getenv(cfg.Providers[i].APIKeyEnv)
		}
	}
	return cfg, nil
}

// LoadWithEnv loads a .env file (filling any unset environment variables) and
// then loads + resolves the config. This is the standard startup path: it lets
// API keys live in .env (gitignored) and be referenced from lumen.toml via
// api_key_env, so no secret is ever committed inline. A missing .env is fine
// (envPath "" skips it).
func LoadWithEnv(configPath, envPath string) (*File, error) {
	if envPath != "" {
		if err := LoadDotEnv(envPath); err != nil {
			return nil, fmt.Errorf("load %s: %w", envPath, err)
		}
	}
	return Load(configPath)
}

// LoadDotEnv reads KEY=VALUE pairs from a .env file and sets them in the
// process environment. Variables already present in the environment win — the
// .env file only fills in what is unset. A missing file is not an error.
// Supports blank lines, "#" comments, an optional "export " prefix, and
// surrounding single/double quotes on the value.
func LoadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key == "" {
			continue
		}
		if _, ok := os.LookupEnv(key); ok {
			continue // a real environment variable wins over .env
		}
		os.Setenv(key, val)
	}
	return nil
}

// UserConfigPath returns the OS-specific user config file path.
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lumen", "lumen.toml"), nil
}

// FindConfig looks for lumen.toml (then reasonix.toml as fallback) in the
// standard locations: cwd, then user config dir. Returns the first found path or "".
func FindConfig() string {
	paths := []string{"./lumen.toml", "./reasonix.toml"}
	if p, err := UserConfigPath(); err == nil {
		paths = append(paths, p)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// FindDotEnv locates a .env file: ./.env first, then alongside the user config.
// Returns "" when none exists.
func FindDotEnv() string {
	candidates := []string{"./.env"}
	if p, err := UserConfigPath(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(p), ".env"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func defaults() *File {
	return &File{
		DefaultModel: "deepseek-flash",
		Agent: AgentConfig{
			MaxSteps:          50,
			Temperature:       0.0,
			ContextWindow:     128000,
			SoftCompactRatio:  0.5,
			CompactRatio:      0.8,
			CompactForceRatio: 1.0,
		},
		Permissions: PermissionsConfig{Mode: "default"},
		Skills:      SkillsConfig{MaxDepth: 3},
	}
}

// IsValidSkillName reports whether name is a usable skill identifier.
func IsValidSkillName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			if i == 0 && r >= '0' && r <= '9' {
				return false
			}
			continue
		}
		return false
	}
	return true
}

// SkillNameKey returns the canonical lowercase key for a skill name.
func SkillNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// CanonicalSkillPath normalizes a skill path for deduplication.
func CanonicalSkillPath(p string) string {
	return filepath.Clean(p)
}
