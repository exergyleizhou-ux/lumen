// Package perf computes per-turn performance metrics — time-to-first-token,
// decode throughput, and turn wall-clock — and renders them as a one-line HUD.
//
// These matter most for local inference: a 27B model on Mac/Metal is slower
// than a cloud API, so latency (not cost) is the bottleneck the user feels.
// The HUD makes that visible every turn.
package perf

import (
	"fmt"
	"time"

	"lumen/internal/event"
)

// Sample is the raw timing captured during one model stream.
type Sample struct {
	StreamStart      time.Time // just before provider.Stream
	FirstChunk       time.Time // first content/reasoning/tool chunk; zero if none arrived
	Done             time.Time // stream finished
	CompletionTokens int       // from real usage when reported, else 0
}

// Compute turns raw timings into the metrics carried on the perf event. It is
// total-safe: a missing first chunk or zero tokens yields zero, never NaN or a
// negative value.
func Compute(s Sample) event.Perf {
	p := event.Perf{CompletionTokens: s.CompletionTokens}

	if !s.Done.IsZero() && !s.StreamStart.IsZero() {
		if d := s.Done.Sub(s.StreamStart); d > 0 {
			p.TurnMs = d.Milliseconds()
		}
	}

	if !s.FirstChunk.IsZero() && !s.StreamStart.IsZero() {
		if ttft := s.FirstChunk.Sub(s.StreamStart); ttft > 0 {
			p.TTFTMs = ttft.Milliseconds()
		}
		// Decode throughput excludes TTFT: tokens over the generation window
		// (first chunk → done). Reported separately from TTFT above.
		if window := s.Done.Sub(s.FirstChunk); window > 0 && s.CompletionTokens > 0 {
			p.TokensPerSec = float64(s.CompletionTokens) / window.Seconds()
		}
	}
	return p
}

// Render formats one perf line for the HUD. Single line, no trailing newline.
func Render(p event.Perf) string {
	tps := "—"
	if p.TokensPerSec > 0 {
		tps = fmt.Sprintf("%.0f tok/s", p.TokensPerSec)
	}
	ttft := "—"
	if p.TTFTMs > 0 {
		ttft = fmt.Sprintf("%dms", p.TTFTMs)
	}
	return fmt.Sprintf("⏱ ttft %s · %s · turn %s", ttft, tps, humanizeMs(p.TurnMs))
}

// humanizeMs renders a duration in ms as "850ms" or "2.2s".
func humanizeMs(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
