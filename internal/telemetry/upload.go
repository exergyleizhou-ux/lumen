// Package telemetry — upload.go. Lightweight upload dispatcher.
// Delegates heavy analysis to report.go's BuildInsightReport.
package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UploadConfig is how the reporter reaches the dev team.
type UploadConfig struct {
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
	Label     string `json:"label"`
	Enabled   bool   `json:"enabled"`
}

// DefaultUploadConfig returns a disabled config pointing at the Lumen repo.
func DefaultUploadConfig() UploadConfig {
	return UploadConfig{
		RepoOwner: "exergyleizhou-ux",
		RepoName:  "lumen",
		Label:     "report",
		Enabled:   false,
	}
}

// LoadUploadConfig reads uplink settings from ~/.lumen/uplink.json.
func LoadUploadConfig() UploadConfig {
	cfg := DefaultUploadConfig()
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".lumen", "uplink.json"))
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

// MaybeUpload is called at session end. Builds an InsightReport,
// encrypts it with the dev public key, and submits it as a GitHub Issue.
func MaybeUpload() (string, error) {
	cfg := LoadUploadConfig()
	if !cfg.Enabled {
		return "", nil // disabled is not an error — silence is golden
	}

	// Throttle: once per 6 hours
	home, _ := os.UserHomeDir()
	lastFile := filepath.Join(home, ".lumen", "telemetry", ".last_upload")
	info, err := os.Stat(lastFile)
	if err == nil && time.Since(info.ModTime()) < 6*time.Hour {
		return "", nil // throttled silently
	}

	report := BuildInsightReport(7)
	url, err := UploadEncryptedReport(report, cfg)
	if err != nil {
		return "", err
	}

	os.WriteFile(lastFile, []byte(time.Now().Format(time.RFC3339)), 0600)
	return url, nil
}

// ShareReport writes an unencrypted human-readable report to disk.
func ShareReport() (string, error) {
	report := BuildInsightReport(7)
	home, _ := os.UserHomeDir()
	shareFile := filepath.Join(home, ".lumen", "share_report.txt")

	f, err := os.Create(shareFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Write summary + full JSON
	fmt.Fprintf(f, "Lumen Intelligence Report — %s\n", report.GeneratedAt.Format("2006-01-02 15:04"))
	fmt.Fprintf(f, "%s\n\n", strings.Repeat("═", 55))
	fmt.Fprintf(f, "Health: %.0f  ·  Stability: %.0f  ·  Adoption: %.0f\n", report.HealthScore, report.StabilityScore, report.AdoptionScore)
	fmt.Fprintf(f, "Sessions: %d  ·  Days: %d  ·  Errors: %d (%.1f%%)\n", report.Sessions, report.DaysActive, report.TotalErrors, report.ErrorRate)
	fmt.Fprintf(f, "Models: %d  ·  Tokens: %d  ·  Cost: $%.4f\n", len(report.ModelUsage), report.TotalTokens, report.TotalCost)
	fmt.Fprintf(f, "\n")

	// Critical alerts
	if len(report.CriticalAlerts) > 0 {
		fmt.Fprintf(f, "CRITICAL:\n")
		for _, a := range report.CriticalAlerts {
			fmt.Fprintf(f, "  [%s] %s\n  → %s\n\n", a.Severity, a.Title, a.Detail)
		}
	}

	// Top tools
	fmt.Fprintf(f, "Top Tools:\n")
	for _, t := range report.TopTools {
		fmt.Fprintf(f, "  %-25s %5d calls  %d errors  [%s]\n", t.Name, t.Calls, t.Errors, t.Category)
	}

	// Error categories
	if len(report.ErrorCategories) > 0 {
		fmt.Fprintf(f, "\nError Categories:\n")
		for _, ec := range report.ErrorCategories {
			fmt.Fprintf(f, "  [%s] %dx — %s\n", ec.Category, ec.Count, strings.Join(ec.Examples, "; "))
		}
	}

	// Wins
	if len(report.Wins) > 0 {
		fmt.Fprintf(f, "\nWins:\n")
		for _, w := range report.Wins {
			fmt.Fprintf(f, "  ✅ %s\n", w)
		}
	}

	// Raw JSON
	fmt.Fprintf(f, "\n--- Raw JSON ---\n")
	raw, _ := json.MarshalIndent(report, "", "  ")
	f.Write(raw)

	return shareFile, nil
}
