package config_validate

import (
	"testing"

	"lumen/internal/config"
)

func TestValidateEmptyConfig(t *testing.T) {
	cfg := &config.File{}
	r := Validate(cfg)
	if r.Valid {
		t.Error("empty config should be invalid")
	}
	if len(r.Issues) == 0 {
		t.Error("should have issues")
	}
}

func TestValidateDefaults(t *testing.T) {
	cfg := defaults()
	r := Validate(cfg)
	if !r.Valid {
		for _, issue := range r.Issues {
			t.Logf("issue: %s: %s", issue.Field, issue.Message)
		}
	}
}

func TestValidateAgentSettings(t *testing.T) {
	cfg := defaults()
	cfg.Agent.Temperature = 5.0
	r := Validate(cfg)
	found := false
	for _, issue := range r.Issues {
		if issue.Field == "agent.temperature" {
			found = true
		}
	}
	if !found {
		t.Error("should flag temperature > 2")
	}
}

func TestPrintReport(t *testing.T) {
	r := &Report{Valid: true}
	s := r.Print()
	if s == "" {
		t.Error("Print should return non-empty")
	}
}

func TestPrintReportWithIssues(t *testing.T) {
	r := &Report{Valid: false}
	r.add(Issue{Field: "test", Severity: "error", Message: "bad", Fix: "fix it"})
	r.add(Issue{Field: "test2", Severity: "warning", Message: "warn"})
	s := r.Print()
	if s == "" {
		t.Error("Print should return non-empty with issues")
	}
}

func defaults() *config.File {
	return &config.File{
		DefaultModel: "test",
		Providers: []config.ProviderConfig{
			{Name: "test", Kind: "openai", BaseURL: "https://api.test.com", Model: "test-model", APIKeyEnv: "TEST_KEY", APIKey: "sk-test"},
		},
		Agent: config.AgentConfig{MaxSteps: 50, Temperature: 0, ContextWindow: 128000},
	}
}
