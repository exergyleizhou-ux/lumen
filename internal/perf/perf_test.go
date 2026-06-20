package perf

import (
	"strings"
	"testing"
	"time"

	"lumen/internal/event"
)

func TestComputeBasicMetrics(t *testing.T) {
	base := time.Unix(1700000000, 0)
	s := Sample{
		StreamStart:      base,
		FirstChunk:       base.Add(200 * time.Millisecond), // TTFT = 200ms
		Done:             base.Add(2200 * time.Millisecond), // turn = 2200ms; decode window = 2000ms
		CompletionTokens: 100,                                // 100 tok / 2.0s = 50 tok/s
	}
	p := Compute(s)
	if p.TTFTMs != 200 {
		t.Errorf("TTFTMs = %d, want 200", p.TTFTMs)
	}
	if p.TurnMs != 2200 {
		t.Errorf("TurnMs = %d, want 2200", p.TurnMs)
	}
	if got := p.TokensPerSec; got < 49.9 || got > 50.1 {
		t.Errorf("TokensPerSec = %v, want ~50", got)
	}
	if p.CompletionTokens != 100 {
		t.Errorf("CompletionTokens = %d, want 100", p.CompletionTokens)
	}
}

func TestComputeNoFirstChunk(t *testing.T) {
	// A stream that produced no content chunk (e.g. immediate error): TTFT and
	// throughput are zero, not NaN or a negative number.
	base := time.Unix(1700000000, 0)
	p := Compute(Sample{StreamStart: base, Done: base.Add(time.Second)})
	if p.TTFTMs != 0 {
		t.Errorf("TTFTMs = %d, want 0 when no first chunk", p.TTFTMs)
	}
	if p.TokensPerSec != 0 {
		t.Errorf("TokensPerSec = %v, want 0", p.TokensPerSec)
	}
	if p.TurnMs != 1000 {
		t.Errorf("TurnMs = %d, want 1000", p.TurnMs)
	}
}

func TestComputeZeroTokens(t *testing.T) {
	// First chunk arrived but usage reported 0 completion tokens — no divide by
	// zero, throughput is 0.
	base := time.Unix(1700000000, 0)
	p := Compute(Sample{
		StreamStart:      base,
		FirstChunk:       base.Add(100 * time.Millisecond),
		Done:             base.Add(500 * time.Millisecond),
		CompletionTokens: 0,
	})
	if p.TokensPerSec != 0 {
		t.Errorf("TokensPerSec = %v, want 0", p.TokensPerSec)
	}
}

func TestRenderOneLine(t *testing.T) {
	line := Render(event.Perf{TTFTMs: 180, TokensPerSec: 50.0, TurnMs: 2200, CompletionTokens: 100})
	// A single line, no newline embedded.
	if strings.Contains(line, "\n") {
		t.Errorf("Render produced multiple lines: %q", line)
	}
	for _, want := range []string{"ttft", "180", "50", "tok/s", "2.2s"} {
		if !strings.Contains(strings.ToLower(line), strings.ToLower(want)) {
			t.Errorf("Render = %q, missing %q", line, want)
		}
	}
}
