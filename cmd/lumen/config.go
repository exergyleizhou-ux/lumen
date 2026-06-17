package main

import (
	"fmt"
	"os"
	"strings"

	"lumen/internal/config"
)

// runConfig displays the current configuration in a human-readable format.
// It is read-only and does not modify any files.
func runConfig() {
	path := config.FindConfig()
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// ── Config file ──
	if path == "" {
		fmt.Println("config file: (none — using defaults)")
	} else {
		fmt.Printf("config file: %s\n", path)
	}

	// ── Default model ──
	fmt.Printf("default model: %s\n", cfg.DefaultModel)

	// ── Providers ──
	if len(cfg.Providers) == 0 {
		fmt.Println("providers:     (none configured)")
	} else {
		fmt.Println("providers:")
		for _, p := range cfg.Providers {
			keySrc := keySource(p)
			active := ""
			if p.Name == cfg.DefaultModel {
				active = " (default)"
			}
			fmt.Printf("  - %-20s  %-10s  %s%s\n", p.Name, p.Model, keySrc, active)
		}
	}

	// ── Permission mode ──
	fmt.Printf("permission mode: %s\n", cfg.Permissions.Mode)

	// ── Agent settings ──
	fmt.Printf("agent:")
	fmt.Printf("\n    max_steps:      %d", cfg.Agent.MaxSteps)
	fmt.Printf("\n    temperature:    %.1f", cfg.Agent.Temperature)
	fmt.Printf("\n    context_window: %d", cfg.Agent.ContextWindow)
	fmt.Println()

	// ── Verify: check key presence ──
	missing := false
	for _, p := range cfg.Providers {
		if p.APIKey == "" {
			fmt.Fprintf(os.Stderr, "⚠  %s: API key not found (env %q is empty or unset)\n", p.Name, p.APIKeyEnv)
			missing = true
		}
	}
	if !missing {
		fmt.Println("✓ all provider API keys present")
	}
}

// keySource returns a human-readable description of where the API key comes from.
func keySource(p config.ProviderConfig) string {
	if p.APIKey != "" {
		if p.APIKeyEnv != "" {
			return fmt.Sprintf("env:%s", p.APIKeyEnv)
		}
		return "config file"
	}
	if p.APIKeyEnv != "" {
		return fmt.Sprintf("env:%s (unset)", p.APIKeyEnv)
	}
	return "no key"
}

// configSummary returns a compact one-line summary of the configuration.
// Used by tests to verify the output contains expected values.
func configSummary() string {
	path := config.FindConfig()
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	var b strings.Builder
	if path == "" {
		b.WriteString("config:(defaults)")
	} else {
		b.WriteString(fmt.Sprintf("config:%s", path))
	}
	b.WriteString(fmt.Sprintf(" model:%s", cfg.DefaultModel))
	b.WriteString(fmt.Sprintf(" providers:%d", len(cfg.Providers)))
	b.WriteString(fmt.Sprintf(" mode:%s", cfg.Permissions.Mode))
	for _, p := range cfg.Providers {
		if p.APIKey == "" {
			b.WriteString(fmt.Sprintf(" !%s:no-key", p.Name))
		}
	}
	return b.String()
}
