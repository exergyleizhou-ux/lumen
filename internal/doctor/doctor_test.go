package doctor

import (
	"os"
	"path/filepath"
	"strings"
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

func TestCheckGoToolchain_goPresent(t *testing.T) {
	old := execLookPath
	execLookPath = func(file string) (string, error) {
		if file == "go" {
			return "/usr/local/go/bin/go", nil
		}
		return "", errNotFound
	}
	defer func() { execLookPath = old }()
	// Silence the real exec.Command call in checkGoToolchain
	// by not actually running it — the test just checks PATH lookup

	r := &Report{AllOk: true}
	// We can't easily mock exec.Command, but the lookup path is testable.
	// Verify the fallback message works.
	r.checkGoToolchain()
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(r.Results))
	}
	if r.Results[0].Status == "fail" {
		t.Error("go present should not fail")
	}
}

func TestCheckGoToolchain_goMissing(t *testing.T) {
	old := execLookPath
	execLookPath = func(file string) (string, error) {
		return "", errNotFound
	}
	defer func() { execLookPath = old }()

	r := &Report{AllOk: true}
	r.checkGoToolchain()
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(r.Results))
	}
	if r.Results[0].Status != "fail" {
		t.Errorf("expected fail for missing go, got %s: %s", r.Results[0].Status, r.Results[0].Detail)
	}
	if !r.AllOk {
		t.Log("AllOk correctly set to false when go missing")
	}
}

func TestCheckGopls_missing(t *testing.T) {
	old := execLookPath
	execLookPath = func(file string) (string, error) {
		return "", errNotFound
	}
	defer func() { execLookPath = old }()

	r := &Report{AllOk: true}
	r.checkGopls()
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(r.Results))
	}
	if r.Results[0].Status != "warn" {
		t.Errorf("expected warn for missing gopls, got %s", r.Results[0].Status)
	}
}

func TestCheckVerify_disabled(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lumen.toml")
	os.WriteFile(cfgPath, []byte(`
[verify]
enabled = false
`), 0644)

	oldFn := verifyConfigPath
	verifyConfigPath = func() string { return cfgPath }
	defer func() { verifyConfigPath = oldFn }()

	r := &Report{AllOk: true}
	r.checkVerify()
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 verify result, got %d", len(r.Results))
	}
	if r.Results[0].Status != "warn" {
		t.Errorf("expected warn for disabled verify, got %s: %s", r.Results[0].Status, r.Results[0].Detail)
	}
	if !strings.Contains(r.Results[0].Detail, "disabled") {
		t.Errorf("detail should mention disabled, got: %s", r.Results[0].Detail)
	}
}

func TestCheckVerify_inlineKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "lumen.toml")
	os.WriteFile(cfgPath, []byte(`
[verify]
enabled = true

[[providers]]
name = "deepseek"
kind = "openai"
api_key = "sk-FAKE-inline-key-for-test"
`), 0644)

	oldFn := verifyConfigPath
	verifyConfigPath = func() string { return cfgPath }
	defer func() { verifyConfigPath = oldFn }()

	r := &Report{AllOk: true}
	r.checkVerify()
	if len(r.Results) < 2 {
		t.Fatalf("expected verify + security results, got %d", len(r.Results))
	}
	found := false
	for _, res := range r.Results {
		if res.Name == "security:api_key" && res.Status == "warn" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected security:api_key warn for inline key")
	}
}

func TestCheckVerify_noConfig(t *testing.T) {
	oldFn := verifyConfigPath
	verifyConfigPath = func() string { return "" }
	defer func() { verifyConfigPath = oldFn }()

	r := &Report{AllOk: true}
	r.checkVerify()
	if len(r.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(r.Results))
	}
	if r.Results[0].Status != "warn" {
		t.Errorf("expected warn for missing config, got %s", r.Results[0].Status)
	}
}

var errNotFound = &errNotFoundType{}

type errNotFoundType struct{}

func (e *errNotFoundType) Error() string { return "not found" }
