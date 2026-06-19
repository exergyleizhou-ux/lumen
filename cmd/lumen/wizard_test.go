package main

import (
	"path/filepath"
	"testing"

	"lumen/internal/config"
)

// A fresh install (no config) must not dead-end: the wizard scaffolds a starter
// lumen.toml that loads into a usable provider + default_model.
func TestScaffoldDefaultConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lumen.toml")
	if err := scaffoldDefaultConfig(path); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("scaffolded config should load: %v", err)
	}
	if len(cfg.Providers) == 0 {
		t.Error("scaffold should configure at least one provider")
	}
	if cfg.DefaultModel == "" {
		t.Error("scaffold should set default_model")
	}
	// default_model must resolve to the scaffolded provider (no silent fallback)
	matched := false
	for _, p := range cfg.Providers {
		if p.Name == cfg.DefaultModel || p.Model == cfg.DefaultModel {
			matched = true
		}
	}
	if !matched {
		t.Errorf("scaffold default_model %q matches no provider", cfg.DefaultModel)
	}
}
