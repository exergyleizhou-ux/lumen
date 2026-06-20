package localprobe

import (
	"fmt"
	"strings"
)

// FormatMarkdown renders probe results as a capability matrix: one row per
// endpoint/model, the decisive "drives agent" column (can it emit a tool_call),
// plus throughput and latency — the metrics that matter for local inference.
func FormatMarkdown(results []Result) string {
	var b strings.Builder
	b.WriteString("| Endpoint | Model | Drives agent (tool_call) | tokens/sec | latency | Notes |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, r := range results {
		drives := "❌"
		notes := ""
		switch {
		case r.Err != nil:
			drives = "⚠️"
			notes = "probe failed: " + r.Err.Error()
		case r.CanToolCall:
			drives = "✅"
		default:
			notes = "prose only — agent unusable"
			if r.TextReply != "" {
				notes += " (replied in text)"
			}
		}
		tps := "—"
		if r.TokensPerSec > 0 {
			tps = fmt.Sprintf("%.1f", r.TokensPerSec)
		}
		lat := "—"
		if r.ElapsedMs > 0 {
			lat = fmt.Sprintf("%dms", r.ElapsedMs)
		}
		model := r.Model
		if model == "" {
			model = "—"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
			r.Name, model, drives, tps, lat, notes))
	}
	return b.String()
}
