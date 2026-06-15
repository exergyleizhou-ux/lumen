package doctor

import (
	"testing"

	"lumen/internal/config"
)

func TestRunEmptyConfig(t *testing.T) {
	cfg := &config.File{
		DefaultModel: "test",
	}
	report := Run(cfg)
	if report == nil {
		t.Fatal("Run should return a report")
	}
	if len(report.Results) == 0 {
		t.Error("report should have at least config check")
	}
}

func TestRunOutput(t *testing.T) {
	cfg := &config.File{
		DefaultModel: "test",
		Providers: []config.ProviderConfig{
			{Name: "echo", Kind: "openai", BaseURL: "http://localhost:1", Model: "test", APIKey: "sk-test"},
		},
	}
	report := Run(cfg)
	printed := report.Print()
	if printed == "" {
		t.Error("Print should return non-empty string")
	}
}

func TestResultStatus(t *testing.T) {
	r := &Report{}
	r.add(Result{Name: "test", Status: "ok", Detail: "good"})
	r.add(Result{Name: "test2", Status: "fail", Detail: "bad"})
	if r.AllOk {
		t.Error("AllOk should be false when a check fails")
	}
}
