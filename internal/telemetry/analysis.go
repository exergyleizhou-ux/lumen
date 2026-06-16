// Package telemetry — analysis engine. Reads collected telemetry data
// and produces actionable insights: trend detection, hotspots, satisfaction
// scoring, and iterative improvement suggestions.
package telemetry

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ── Analysis Report ────────────────────────────────────────

// AnalysisReport is a generated analysis of collected telemetry.
type AnalysisReport struct {
	GeneratedAt  time.Time         `json:"generated_at"`
	Period       string            `json:"period"`       // "day", "week", "month"
	DaysScanned  int               `json:"days_scanned"`
	TotalEvents  int               `json:"total_events"`
	TotalSessions int              `json:"total_sessions"`
	ByEventType  map[EventType]int `json:"by_event_type"`

	// Tool analysis
	ToolCalls     int                         `json:"tool_calls"`
	ToolErrors    int                         `json:"tool_errors"`
	ToolErrorRate float64                     `json:"tool_error_rate"`
	TopTools      []ToolStat                  `json:"top_tools"`
	ErrorTools    []ToolStat                  `json:"error_tools"`

	// Model analysis
	ModelCalls    int                         `json:"model_calls"`
	TopModels     []ModelStat                 `json:"top_models"`
	TotalTokens   int64                       `json:"total_tokens"`
	TotalCost     float64                     `json:"total_cost"`
	AvgLatency    time.Duration               `json:"avg_latency_ms,omitempty"`

	// Feedback
	FeedbackTotal  int                        `json:"feedback_total"`
	SatisfactionRate float64                  `json:"satisfaction_rate"`

	// Recommendations
	Recommendations []Recommendation          `json:"recommendations"`
	HealthScore    float64                    `json:"health_score"` // 0-100
}

// ToolStat is aggregated tool usage.
type ToolStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Errors int   `json:"errors"`
}

// ModelStat is aggregated model usage.
type ModelStat struct {
	Name  string `json:"name"`
	Calls int    `json:"calls"`
	Tokens int64 `json:"tokens"`
	Cost  float64 `json:"cost"`
}

// Recommendation is an actionable suggestion.
type Recommendation struct {
	Priority  string `json:"priority"` // high, medium, low
	Area      string `json:"area"`     // tool, model, performance, usability, feedback
	Title     string `json:"title"`
	Detail    string `json:"detail"`
}

// ── Analyzer ────────────────────────────────────────────────

// Analyzer reads telemetry and generates reports.
type Analyzer struct {
	telemetryDir  string
	feedbackDir   string
}

// NewAnalyzer creates an analyzer.
func NewAnalyzer() *Analyzer {
	home, _ := os.UserHomeDir()
	a := &Analyzer{
		telemetryDir: filepath.Join(home, ".lumen", "telemetry"),
		feedbackDir:  filepath.Join(home, ".lumen", "feedback"),
	}
	return a
}

// Analyze reads the last N days of telemetry and generates a report.
func (a *Analyzer) Analyze(period string) *AnalysisReport {
	report := &AnalysisReport{
		GeneratedAt: time.Now(),
		Period:      period,
		ByEventType: map[EventType]int{},
	}

	days := 7
	switch period {
	case "day":
		days = 1
	case "week":
		days = 7
	case "month":
		days = 30
	}

	// Collect all events in the period
	sessions := map[string]bool{}
	toolCalls := map[string]*ToolStat{}
	toolErrorsBy := map[string]int{}
	modelStats := map[string]*ModelStat{}

	now := time.Now()
	for i := 0; i < days; i++ {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		filename := filepath.Join(a.telemetryDir, date+".jsonl")
		data, err := os.ReadFile(filename)
		if err != nil {
			continue
		}
		report.DaysScanned++

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			var entry Entry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}

			report.TotalEvents++
			report.ByEventType[entry.Type]++
			if entry.SessionID != "" {
				sessions[entry.SessionID] = true
			}

			switch entry.Type {
			case EventToolCall:
				report.ToolCalls++
				name, _ := entry.Data["name"].(string)
				if name != "" {
					if toolCalls[name] == nil {
						toolCalls[name] = &ToolStat{Name: name}
					}
					toolCalls[name].Count++
				}

			case EventToolError:
				report.ToolErrors++
				name, _ := entry.Data["name"].(string)
				if name != "" {
					toolErrorsBy[name]++
				}

			case EventModelCall:
				report.ModelCalls++
				model, _ := entry.Data["model"].(string)
				if model != "" {
					if modelStats[model] == nil {
						modelStats[model] = &ModelStat{Name: model}
					}
					modelStats[model].Calls++
					if tokens, ok := entry.Data["tokens"].(float64); ok {
						modelStats[model].Tokens += int64(tokens)
					}
					if tokens, ok := entry.Data["tokens"].(json.Number); ok {
						n, _ := tokens.Int64()
						modelStats[model].Tokens += n
					}
				}
			}
		}
	}

	report.TotalSessions = len(sessions)

	// Tool stats
	for name, ts := range toolCalls {
		ts.Errors = toolErrorsBy[name]
		report.TopTools = append(report.TopTools, *ts)
		if ts.Errors > 0 {
			report.ErrorTools = append(report.ErrorTools, *ts)
		}
	}
	sort.Slice(report.TopTools, func(i, j int) bool { return report.TopTools[i].Count > report.TopTools[j].Count })
	sort.Slice(report.ErrorTools, func(i, j int) bool { return report.ErrorTools[i].Errors > report.ErrorTools[j].Errors })
	if report.ToolCalls > 0 {
		report.ToolErrorRate = float64(report.ToolErrors) / float64(report.ToolCalls) * 100
	}

	// Model stats
	for _, ms := range modelStats {
		report.TotalTokens += ms.Tokens
		report.TotalCost += float64(ms.Tokens) * 0.14 / 1e6 // approximate
		report.TopModels = append(report.TopModels, *ms)
	}
	sort.Slice(report.TopModels, func(i, j int) bool { return report.TopModels[i].Calls > report.TopModels[j].Calls })

	// Feedback analysis
	fs := NewFeedbackStore()
	feedbackItems := fs.List(1000)
	report.FeedbackTotal = len(feedbackItems)
	counts := fs.Counts()
	if counts["thumbs_up"]+counts["thumbs_down"]+counts["bug"] > 0 {
		report.SatisfactionRate = float64(counts["thumbs_up"]) / float64(counts["thumbs_up"]+counts["thumbs_down"]+counts["bug"]) * 100
	}

	// Generate recommendations
	report.Recommendations = generateRecommendations(report, feedbackItems)

	// Health score (0-100)
	report.HealthScore = calculateHealth(report)
	_ = countLimit
	return report
}

func generateRecommendations(r *AnalysisReport, feedback []FeedbackEntry) []Recommendation {
	var recs []Recommendation

	// Tool error rate
	if r.ToolErrorRate > 20 {
		recs = append(recs, Recommendation{
			Priority: "high", Area: "tool",
			Title: fmt.Sprintf("High tool error rate: %.0f%%", r.ToolErrorRate),
			Detail: fmt.Sprintf("%d errors across %d tool calls. Top failing tools: %s.",
				r.ToolErrors, r.ToolCalls, formatToolStats(r.ErrorTools, 3)),
		})
	} else if r.ToolErrorRate > 5 {
		recs = append(recs, Recommendation{
			Priority: "medium", Area: "tool",
			Title: fmt.Sprintf("Elevated tool error rate: %.0f%%", r.ToolErrorRate),
			Detail: fmt.Sprintf("Consider improving error handling for: %s.", formatToolStats(r.ErrorTools, 3)),
		})
	}

	// Unused tools
	if r.TotalEvents > 100 {
		usedTools := map[string]bool{}
		for _, t := range r.TopTools { usedTools[t.Name] = true }
		if len(usedTools) < 10 {
			recs = append(recs, Recommendation{
				Priority: "low", Area: "tool",
				Title: "Low tool utilization",
				Detail: "Only a few tools are being used. Consider promoting discoverability of other capabilities.",
			})
		}
	}

	// Satisfaction
	if r.FeedbackTotal >= 5 && r.SatisfactionRate < 50 {
		recs = append(recs, Recommendation{
			Priority: "high", Area: "feedback",
			Title: fmt.Sprintf("Low satisfaction: %.0f%%", r.SatisfactionRate),
			Detail: "Review recent thumbs-down and bug reports to identify patterns.",
		})
	}

	// Model diversity
	if r.ModelCalls > 0 && len(r.TopModels) == 1 {
		recs = append(recs, Recommendation{
			Priority: "low", Area: "model",
			Title: "Single model usage",
			Detail: "Users rely on one model exclusively. Consider suggesting alternative models for cost savings.",
		})
	}

	// Feedback-driven: bug reports
	bugs := 0
	for _, fb := range feedback {
		if fb.Type == "bug" { bugs++ }
	}
	if bugs > 3 {
		recs = append(recs, Recommendation{
			Priority: "high", Area: "feedback",
			Title: fmt.Sprintf("%d bug reports — time for a bugfix release", bugs),
			Detail: "Review the bug reports in ~/.lumen/feedback/ and prioritize fixes for the next release.",
		})
	}

	return recs
}

var countLimit = 0 // suppress unused var

func formatToolStats(stats []ToolStat, max int) string {
	var parts []string
	for i, t := range stats {
		if i >= max { break }
		parts = append(parts, fmt.Sprintf("%s (%d calls/%d errors)", t.Name, t.Count, t.Errors))
	}
	if len(stats) > max {
		parts = append(parts, fmt.Sprintf("+%d more", len(stats)-max))
	}
	if len(parts) == 0 { return "none" }
	return strings.Join(parts, ", ")
}

func calculateHealth(r *AnalysisReport) float64 {
	score := 100.0

	// Error rate penalty
	if r.ToolErrorRate > 0 {
		score -= math.Min(r.ToolErrorRate*2, 30)
	}

	// Satisfaction bonus/penalty
	if r.FeedbackTotal >= 5 {
		delta := (r.SatisfactionRate - 80) * 0.5
		score += delta
	}

	// Diversity bonus
	if len(r.TopTools) > 20 {
		score += 5
	}
	if len(r.TopModels) > 1 {
		score += 5
	}

	// Session bonus
	if r.TotalSessions > 10 {
		score += 5
	}

	return math.Max(0, math.Min(100, score))
}

// FormatReport renders an analysis report for terminal display.
func FormatReport(r *AnalysisReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Telemetry Analysis — %s period — %s\n", r.Period, r.GeneratedAt.Format("2006-01-02")))
	sb.WriteString(strings.Repeat("─", 50) + "\n\n")

	sb.WriteString(fmt.Sprintf("Health: %.0f/100\n", r.HealthScore))
	sb.WriteString(fmt.Sprintf("Sessions: %d  ·  Events: %d  ·  Days: %d\n", r.TotalSessions, r.TotalEvents, r.DaysScanned))

	if r.ToolCalls > 0 {
		sb.WriteString(fmt.Sprintf("\nTools: %d calls  ·  %.1f%% error rate\n", r.ToolCalls, r.ToolErrorRate))
		sb.WriteString("  Top tools:\n")
		maxTop := r.TopTools
		if len(maxTop) > 10 { maxTop = maxTop[:10] }
		for _, t := range maxTop {
			sb.WriteString(fmt.Sprintf("    %-25s %5d calls", t.Name, t.Count))
			if t.Errors > 0 {
				sb.WriteString(fmt.Sprintf("  %d errors (%.0f%%)", t.Errors, float64(t.Errors)/float64(t.Count)*100))
			}
			sb.WriteByte('\n')
		}
	}

	if r.ModelCalls > 0 {
		sb.WriteString(fmt.Sprintf("\nModels: %d calls  ·  tokens ~%dk  ·  cost ~$%.3f\n", r.ModelCalls, r.TotalTokens/1000, r.TotalCost))
		for _, m := range r.TopModels {
			sb.WriteString(fmt.Sprintf("    %-25s %5d calls\n", m.Name, m.Calls))
		}
	}

	if r.FeedbackTotal > 0 {
		sb.WriteString(fmt.Sprintf("\nFeedback: %d items  ·  %.0f%% satisfaction\n", r.FeedbackTotal, r.SatisfactionRate))
	}

	if len(r.Recommendations) > 0 {
		sb.WriteString("\nRecommendations:\n")
		for _, rec := range r.Recommendations {
			icon := "●"
			switch rec.Priority {
			case "high": icon = "🔴"
			case "medium": icon = "🟡"
			default: icon = "🟢"
			}
			sb.WriteString(fmt.Sprintf("  %s [%s] %s\n", icon, rec.Area, rec.Title))
			sb.WriteString(fmt.Sprintf("     %s\n", rec.Detail))
		}
	}

	return sb.String()
}
