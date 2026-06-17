// Package reliability implements the monthly reliability report generator (SpaceX §3.2).
// Scores are based on session history in ~/.lumen/history/*.jsonl.
// Reports are written to ~/.lumen/reports/YYYY-MM.md.
package reliability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Report is a monthly reliability snapshot.
type Report struct {
	Period       string   `json:"period"` // "2026-06"
	GeneratedAt  string   `json:"generated_at"`
	Sessions     int      `json:"sessions"`
	Turns        int      `json:"turns"`
	TotalTokens  int      `json:"total_tokens"`
	TotalCost    float64  `json:"total_cost_usd"`
	AvgTokensPerTurn int  `json:"avg_tokens_per_turn"`
	AvgCostPerTurn   float64 `json:"avg_cost_per_turn"`
	AvgTurnsPerSession int `json:"avg_turns_per_session"`
	Crashes      int      `json:"crashes"`
	VerifyPasses int      `json:"verify_passes"`
	VerifyFails  int      `json:"verify_fails"`
	Rollbacks    int      `json:"rollbacks"`
	DirtyFiles   int      `json:"dirty_files"` // changed but not committed
	Notes        string   `json:"notes"`
}

// Generate reads the session history and produces a report for the given month.
func Generate(historyDir string, year int, month time.Month) Report {
	prefix := fmt.Sprintf("%04d-%02d", year, month)
	r := Report{
		Period:      prefix,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	entries, err := os.ReadDir(historyDir)
	if err != nil {
		r.Notes = "cannot read history: " + err.Error()
		return r
	}

	var sessionsWithTurns int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		path := filepath.Join(historyDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		r.Sessions++
		lines := strings.Split(string(data), "\n")
		userMsgs := 0
		assistantMsgs := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var msg struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}
			switch msg.Role {
			case "user":
				userMsgs++
			case "assistant":
				assistantMsgs++
			}
			// Estimate token count from character length (~4 chars/token)
			r.TotalTokens += len(msg.Content) / 4
		}

		r.Turns += userMsgs
		if userMsgs > 0 {
			sessionsWithTurns++
		}

		// Estimate cost (DeepSeek: $0.14/1M prompt, $0.28/1M completion)
		r.TotalCost += float64(r.TotalTokens) * 0.14 / 1_000_000
		_ = assistantMsgs
	}

	if r.Turns > 0 {
		r.AvgTokensPerTurn = r.TotalTokens / r.Turns
		r.AvgCostPerTurn = r.TotalCost / float64(r.Turns)
	}
	if sessionsWithTurns > 0 {
		r.AvgTurnsPerSession = r.Turns / sessionsWithTurns
	}

	// Count crashes: search for "panic" or "fatal" in session files
	r.Crashes = countPatternInHistory(historyDir, prefix, "panic|fatal|cannot recover")
	r.VerifyPasses = countPatternInHistory(historyDir, prefix, "verified")
	r.VerifyFails = countPatternInHistory(historyDir, prefix, "verify failed")
	r.Rollbacks = countPatternInHistory(historyDir, prefix, "↩ rollback")

	return r
}

func countPatternInHistory(dir, prefix, pattern string) int {
	count := 0
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(dir, entry.Name()))
		for _, substr := range strings.Split(pattern, "|") {
			count += strings.Count(string(data), substr)
		}
	}
	return count
}

// Print formats the report for terminal display.
func (r Report) Print() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Monthly Reliability Report — %s\n", r.Period))
	sb.WriteString(strings.Repeat("═", 56) + "\n\n")
	sb.WriteString(fmt.Sprintf("  Sessions:           %d\n", r.Sessions))
	sb.WriteString(fmt.Sprintf("  Turns:              %d\n", r.Turns))
	sb.WriteString(fmt.Sprintf("  Est. Tokens:        %d\n", r.TotalTokens))
	sb.WriteString(fmt.Sprintf("  Est. Cost:          $%.4f\n", r.TotalCost))
	sb.WriteString(fmt.Sprintf("  Avg Tokens/Turn:    %d\n", r.AvgTokensPerTurn))
	sb.WriteString(fmt.Sprintf("  Avg Cost/Turn:      $%.4f\n", r.AvgCostPerTurn))
	sb.WriteString(fmt.Sprintf("  Avg Turns/Session:  %d\n", r.AvgTurnsPerSession))
	sb.WriteByte('\n')
	sb.WriteString(fmt.Sprintf("  ✅ Verify Pass:    %d\n", r.VerifyPasses))
	sb.WriteString(fmt.Sprintf("  ❌ Verify Fail:    %d\n", r.VerifyFails))
	sb.WriteString(fmt.Sprintf("  ↩  Rollbacks:     %d\n", r.Rollbacks))
	sb.WriteString(fmt.Sprintf("  💥 Crashes:        %d\n", r.Crashes))
	sb.WriteByte('\n')
	if r.Notes != "" {
		sb.WriteString(fmt.Sprintf("  📝 %s\n", r.Notes))
	}
	return sb.String()
}

// Save writes the report to ~/.lumen/reports/YYYY-MM.md.
func (r Report) Save() (string, error) {
	dir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "reports")
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, r.Period+".md")
	return path, os.WriteFile(path, []byte(r.Print()), 0644)
}

// Latest returns the most recent report path, or "" if none exist.
func Latest() string {
	dir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "reports")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	if len(entries) == 0 {
		return ""
	}
	// Sort by name descending (YYYY-MM.md)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})
	return filepath.Join(dir, entries[0].Name())
}

// Ensure unused imports don't cause build errors.
var _ = fmt.Sprintf
