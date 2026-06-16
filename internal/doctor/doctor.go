// Package doctor runs health checks on the agent configuration: API key
// validity, model reachability, MCP server status, workspace state.
package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/config"
	"lumen/internal/editverify"
)

// Result holds the outcome of one health check.
type Result struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok", "warn", "fail"
	Detail string `json:"detail,omitempty"`
}

// Report holds the full health check report.
type Report struct {
	Results []Result `json:"results"`
	AllOk   bool     `json:"all_ok"`
}

// Run executes all health checks and returns the report.
func Run(cfg *config.File) *Report {
	r := &Report{AllOk: true}

	// Check config existence
	r.checkConfig()

	// Check each provider
	for _, pc := range cfg.Providers {
		r.checkProvider(pc)
	}

	// Check workspace
	r.checkWorkspace()

	// Check git availability
	r.checkGit()

	// Check Go toolchain
	r.checkGoToolchain()

	// Check gopls (LSP)
	r.checkGopls()

	// Check verify config
	r.checkVerify()

	return r
}

func (r *Report) checkConfig() {
	path := config.FindConfig()
	if path == "" {
		r.add(Result{Name: "config", Status: "warn", Detail: "no lumen.toml found — using defaults"})
		return
	}
	r.add(Result{Name: "config", Status: "ok", Detail: path})
}

func (r *Report) checkProvider(pc config.ProviderConfig) {
	name := "provider:" + pc.Name
	if pc.APIKey == "" {
		r.add(Result{Name: name, Status: "warn", Detail: pc.APIKeyEnv + " not set"})
		return
	}

	// Quick liveness check: list models or ping base URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try /v1/models as a lightweight auth check
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(pc.BaseURL, "/")+"/models", nil)
	if err != nil {
		r.add(Result{Name: name, Status: "warn", Detail: "bad URL: " + err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+pc.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.add(Result{Name: name, Status: "warn", Detail: "unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		r.add(Result{Name: name, Status: "fail", Detail: fmt.Sprintf("HTTP %d — API key invalid or expired", resp.StatusCode)})
		r.AllOk = false
		return
	}
	if resp.StatusCode >= 400 {
		// Some providers don't expose /models — try a chat probe
		r.checkChatProbe(ctx, pc)
		return
	}

	// Parse model list
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if json.Unmarshal(body, &models) == nil {
		found := false
		for _, m := range models.Data {
			if m.ID == pc.Model {
				found = true
				break
			}
		}
		if found {
			r.add(Result{Name: name, Status: "ok", Detail: pc.Model + " available"})
		} else {
			r.add(Result{Name: name, Status: "warn", Detail: pc.Model + " not in model list; got " + fmt.Sprintf("%d models", len(models.Data))})
		}
		return
	}

	r.add(Result{Name: name, Status: "ok", Detail: "HTTP " + http.StatusText(resp.StatusCode)})
}

func (r *Report) checkChatProbe(ctx context.Context, pc config.ProviderConfig) {
	body := map[string]any{
		"model": pc.Model,
		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 1,
		"stream":     false,
	}
	bodyJSON, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(pc.BaseURL, "/")+"/chat/completions",
		bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+pc.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.add(Result{Name: "provider:" + pc.Name, Status: "warn", Detail: "chat probe failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		r.add(Result{Name: "provider:" + pc.Name, Status: "ok", Detail: pc.Model + " reachable"})
	} else if resp.StatusCode == 401 || resp.StatusCode == 403 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		r.add(Result{Name: "provider:" + pc.Name, Status: "fail", Detail: fmt.Sprintf("HTTP %d — %s", resp.StatusCode, string(body))})
		r.AllOk = false
	} else {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		r.add(Result{Name: "provider:" + pc.Name, Status: "warn", Detail: fmt.Sprintf("HTTP %d — %s", resp.StatusCode, string(body))})
	}
}

func (r *Report) checkWorkspace() {
	wd, err := os.Getwd()
	if err != nil {
		r.add(Result{Name: "workspace", Status: "fail", Detail: "cannot get working directory: " + err.Error()})
		r.AllOk = false
		return
	}
	// Check for .git
	if _, err := os.Stat(filepath.Join(wd, ".git")); err == nil {
		r.add(Result{Name: "workspace", Status: "ok", Detail: wd + " (git repo)"})
		return
	}
	r.add(Result{Name: "workspace", Status: "ok", Detail: wd})
}

func (r *Report) checkGit() {
	path, err := execLookPath("git")
	if err != nil {
		r.add(Result{Name: "git", Status: "ok", Detail: "not installed (optional)"})
		return
	}
	r.add(Result{Name: "git", Status: "ok", Detail: path})
}

// checkGoToolchain verifies that the Go toolchain is installed and reports its version.
func (r *Report) checkGoToolchain() {
	goPath, err := execLookPath("go")
	if err != nil {
		r.add(Result{Name: "go", Status: "fail", Detail: "go not found in PATH — install go 1.23+ from https://go.dev/dl/"})
		r.AllOk = false
		return
	}
	out, err := exec.Command(goPath, "version").CombinedOutput()
	if err != nil {
		r.add(Result{Name: "go", Status: "warn", Detail: fmt.Sprintf("found at %s but version check failed: %v", goPath, err)})
		return
	}
	r.add(Result{Name: "go", Status: "ok", Detail: strings.TrimSpace(string(out))})
}

// checkGopls verifies the gopls LSP server is installed (warn if missing, ok if found).
func (r *Report) checkGopls() {
	goplsPath, err := execLookPath("gopls")
	if err != nil {
		r.add(Result{Name: "gopls", Status: "warn", Detail: "gopls not found — install with 'go install golang.org/x/tools/gopls@latest' for LSP diagnostics"})
		return
	}
	r.add(Result{Name: "gopls", Status: "ok", Detail: goplsPath})
}

// checkVerify loads the verify section from lumen.toml and reports its configuration.
// Also warns if the config file contains an inline api_key (security concern).
func (r *Report) checkVerify() {
	var verifyCfg editverify.Config
	cfgPath := verifyConfigPath()
	if cfgPath == "" {
		r.add(Result{Name: "verify", Status: "warn", Detail: "no lumen.toml found — verify defaults to enabled"})
		return
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		r.add(Result{Name: "verify", Status: "warn", Detail: fmt.Sprintf("cannot read config: %v", err)})
		return
	}

	verifyCfg, err = editverify.ConfigFromTOML(raw)
	if err != nil {
		r.add(Result{Name: "verify", Status: "warn", Detail: fmt.Sprintf("parse error: %v — using defaults", err)})
		return
	}

	if !verifyCfg.Enabled {
		r.add(Result{Name: "verify", Status: "warn", Detail: "disabled in config — build/vet/test after edits will not run"})
	} else {
		parts := []string{"enabled"}
		if verifyCfg.Command != "" {
			parts = append(parts, "command="+verifyCfg.Command)
		}
		parts = append(parts, "scope="+verifyCfg.Scope)
		if verifyCfg.RunTests {
			parts = append(parts, "tests=on")
		}
		parts = append(parts, fmt.Sprintf("max_repair=%d", verifyCfg.MaxRepairCycles))
		r.add(Result{Name: "verify", Status: "ok", Detail: strings.Join(parts, " ")})
	}

	// Security: check for inline api_key in config file
	if strings.Contains(string(raw), "api_key") && strings.Contains(string(raw), "sk-") {
		r.add(Result{Name: "security:api_key", Status: "warn",
			Detail: "config contains inline api_key — move to env var and rotate the key"})
	}
}

func (r *Report) add(res Result) {
	r.Results = append(r.Results, res)
}

// Print formats the report for human reading.
func (r *Report) Print() string {
	var sb strings.Builder
	sb.WriteString("Lumen health check\n")
	sb.WriteString("─────────────────\n\n")
	for _, res := range r.Results {
		icon := "✅"
		switch res.Status {
		case "fail":
			icon = "❌"
		case "warn":
			icon = "⚠️"
		}
		fmt.Fprintf(&sb, "%s %s: %s\n", icon, res.Name, res.Detail)
	}
	sb.WriteByte('\n')
	if r.AllOk {
		sb.WriteString("All checks passed.\n")
	} else {
		sb.WriteString("Some checks failed — review above.\n")
	}
	return sb.String()
}

// execLookPath is os/exec.LookPath, aliased for testability.
var execLookPath = func(file string) (string, error) {
	return lookPathImpl(file)
}

// verifyConfigPath returns the config path to read for checkVerify.
// Aliased for testability.
var verifyConfigPath = config.FindConfig

func lookPathImpl(file string) (string, error) {
	// Simple path check
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		candidate := filepath.Join(dir, file)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH", file)
}
