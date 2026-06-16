package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// UploadConfig is how the reporter reaches the dev team.
type UploadConfig struct {
	RepoOwner string // github.com/<owner>
	RepoName  string // github.com/<owner>/<repo>
	Label     string // issue label to apply (e.g. "feedback")
	Enabled   bool   // user must opt in
}

// DefaultUploadConfig returns a disabled config pointing at the Lumen repo.
func DefaultUploadConfig() UploadConfig {
	return UploadConfig{
		RepoOwner: "exergyleizhou-ux",
		RepoName:  "lumen",
		Label:     "feedback",
		Enabled:   false, // disabled by default — user must opt in
	}
}

// LoadUploadConfig reads uplink settings from disk.
func LoadUploadConfig() UploadConfig {
	cfg := DefaultUploadConfig()
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".lumen", "uplink.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg
}

// SaveUploadConfig persists uplink settings.
func SaveUploadConfig(cfg UploadConfig) error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".lumen")
	os.MkdirAll(dir, 0700)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(filepath.Join(dir, "uplink.json"), data, 0600)
}

// UploadReport sends a usage report to the configured GitHub repo as an Issue.
// Uses GITHUB_TOKEN env var or gh CLI for auth. Returns the issue URL on success.
func UploadReport(bundle *ExportBundle, cfg UploadConfig) (string, error) {
	if !cfg.Enabled {
		return "", fmt.Errorf("uplink is disabled — run /uplink on to enable")
	}

	title := fmt.Sprintf("📊 Usage Report — %s (health: %.0f/100)",
		bundle.GeneratedAt.Format("2006-01-02"), bundle.HealthScore)
	body := FormatExport(bundle)

	issueURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", cfg.RepoOwner, cfg.RepoName)
	payload := map[string]any{
		"title":  title,
		"body":   body,
		"labels": []string{cfg.Label},
	}
	payloadBytes, _ := json.Marshal(payload)

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return "", fmt.Errorf("GITHUB_TOKEN not set — run: export GITHUB_TOKEN=ghp_...")
	}

	req, err := http.NewRequest("POST", issueURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		return "", fmt.Errorf("GitHub %d: %v", resp.StatusCode, errBody["message"])
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	url, _ := result["html_url"].(string)
	return url, nil
}

// MaybeUpload is called at session end. Uploads if opt-in is enabled.
// Returns the issue URL if uploaded, or empty string + error.
func MaybeUpload() (string, error) {
	cfg := LoadUploadConfig()
	if !cfg.Enabled {
		return "", nil
	}

	// Only upload if we have real data (at least 2 sessions worth)
	c := NewCollector()
	defer c.Close()
	bundle := c.Export()
	if bundle.SessionCount < 1 {
		return "", nil
	}

	// Don't flood — throttle to once every 6 hours per session
	lastFile := filepath.Join(c.dir, ".last_upload")
	info, err := os.Stat(lastFile)
	if err == nil && time.Since(info.ModTime()) < 6*time.Hour {
		return "", nil
	}

	url, err := UploadReport(bundle, cfg)
	if err != nil {
		return "", err
	}

	// Touch the timestamp
	os.WriteFile(lastFile, []byte(time.Now().Format(time.RFC3339)), 0600)
	return url, nil
}
