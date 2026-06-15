// Package config_validate provides configuration validation and repair.
// It checks provider URLs, API key presence, model availability, and
// workspace settings. Adapted from claw-code's config_validate.rs.
package config_validate

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/config"
)

// Issue represents one configuration problem found during validation.
type Issue struct {
	Field    string `json:"field"`    // e.g. "providers.deepseek-flash.api_key"
	Severity string `json:"severity"` // "error", "warning", "info"
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"` // suggestion for how to fix
}

// Report holds all validation results.
type Report struct {
	Valid  bool    `json:"valid"`
	Issues []Issue `json:"issues"`
}

// Validate checks a config for common problems and returns a report.
func Validate(cfg *config.File) *Report {
	r := &Report{Valid: true}

	// Check default model
	if cfg.DefaultModel == "" {
		r.add(Issue{Field: "default_model", Severity: "error",
			Message: "no default model configured", Fix: "set default_model in lumen.toml"})
	}

	// Check providers
	if len(cfg.Providers) == 0 {
		r.add(Issue{Field: "providers", Severity: "error",
			Message: "no providers configured", Fix: "add at least one [[providers]] section to lumen.toml"})
	}
	seenNames := map[string]bool{}
	defaultFound := false
	for _, pc := range cfg.Providers {
		r.validateProvider(pc, cfg.DefaultModel, seenNames, &defaultFound)
	}

	if !defaultFound && cfg.DefaultModel != "" {
		r.add(Issue{Field: "default_model", Severity: "warning",
			Message: fmt.Sprintf("default_model %q not found in providers list", cfg.DefaultModel),
			Fix:     "set default_model to one of the configured provider names"})
	}

	// Check agent settings
	r.validateAgent(&cfg.Agent)

	// Check workspace
	r.validateWorkspace()

	return r
}

func (r *Report) validateProvider(pc config.ProviderConfig, defaultModel string, seen map[string]bool, defaultFound *bool) {
	if pc.Name == "" {
		r.add(Issue{Field: "providers[].name", Severity: "error", Message: "provider missing name"})
		return
	}
	if seen[pc.Name] {
		r.add(Issue{Field: fmt.Sprintf("providers.%s.name", pc.Name), Severity: "error",
			Message: fmt.Sprintf("duplicate provider name %q", pc.Name)})
	}
	seen[pc.Name] = true
	if pc.Name == defaultModel {
		*defaultFound = true
	}

	// Validate base URL
	if pc.BaseURL == "" {
		r.add(Issue{Field: fmt.Sprintf("providers.%s.base_url", pc.Name), Severity: "error",
			Message: "base_url is required", Fix: "set base_url (e.g. https://api.deepseek.com)"})
	} else if u, err := url.Parse(pc.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		r.add(Issue{Field: fmt.Sprintf("providers.%s.base_url", pc.Name), Severity: "warning",
			Message: fmt.Sprintf("base_url %q may not be a valid HTTP URL", pc.BaseURL),
			Fix:     "ensure base_url starts with http:// or https://"})
	}

	// Validate model
	if pc.Model == "" {
		r.add(Issue{Field: fmt.Sprintf("providers.%s.model", pc.Name), Severity: "error",
			Message: "model is required", Fix: "set model (e.g. deepseek-chat)"})
	}

	// Validate API key env
	if pc.APIKeyEnv != "" {
		if os.Getenv(pc.APIKeyEnv) == "" {
			r.add(Issue{Field: fmt.Sprintf("providers.%s.api_key_env", pc.Name), Severity: "warning",
				Message: fmt.Sprintf("environment variable %s is not set", pc.APIKeyEnv),
				Fix:     fmt.Sprintf("export %s=sk-... or add to .env file", pc.APIKeyEnv)})
		}
	} else {
		r.add(Issue{Field: fmt.Sprintf("providers.%s.api_key_env", pc.Name), Severity: "info",
			Message: "no api_key_env specified — provider may not be usable",
			Fix:     "add api_key_env = \"DEEPSEEK_API_KEY\" (or similar)"})
	}
}

func (r *Report) validateAgent(ac *config.AgentConfig) {
	if ac.MaxSteps <= 0 {
		ac.MaxSteps = 50
		r.add(Issue{Field: "agent.max_steps", Severity: "info",
			Message: "max_steps was 0 or negative, defaulting to 50"})
	}
	if ac.ContextWindow <= 0 {
		ac.ContextWindow = 128000
		r.add(Issue{Field: "agent.context_window", Severity: "info",
			Message: "context_window was 0, defaulting to 128000"})
	}
	if ac.Temperature < 0 || ac.Temperature > 2 {
		r.add(Issue{Field: "agent.temperature", Severity: "warning",
			Message: fmt.Sprintf("temperature %.1f is unusual (range 0.0-2.0)", ac.Temperature),
			Fix:     "set temperature between 0.0 and 2.0"})
	}
}

func (r *Report) validateWorkspace() {
	wd, _ := os.Getwd()
	gitPath := filepath.Join(wd, ".git")
	if _, err := os.Stat(gitPath); os.IsNotExist(err) {
		r.add(Issue{Field: "workspace", Severity: "info",
			Message: "current directory is not a git repository — some features (git diff, commit) are unavailable",
			Fix:     "run 'git init' or change to a git repository directory"})
	}
}

func (r *Report) add(issue Issue) {
	if issue.Severity == "error" {
		r.Valid = false
	}
	r.Issues = append(r.Issues, issue)
}

// Print formats the report for display.
func (r *Report) Print() string {
	var sb strings.Builder
	if r.Valid {
		sb.WriteString("✅ Configuration is valid.\n")
	} else {
		sb.WriteString("❌ Configuration has errors.\n")
	}
	if len(r.Issues) == 0 {
		sb.WriteString("No issues found.\n")
		return sb.String()
	}
	sb.WriteByte('\n')
	for _, issue := range r.Issues {
		icon := "ℹ️"
		switch issue.Severity {
		case "error":
			icon = "❌"
		case "warning":
			icon = "⚠️"
		}
		sb.WriteString(fmt.Sprintf("%s %s: %s\n", icon, issue.Field, issue.Message))
		if issue.Fix != "" {
			sb.WriteString(fmt.Sprintf("   → %s\n", issue.Fix))
		}
	}
	return sb.String()
}
