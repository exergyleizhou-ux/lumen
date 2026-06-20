package main

import (
	"strings"
	"testing"

	"lumen/internal/event"
)

// A passing verify-after-edit must be visible in `lumen run` (it was dropped).
func TestVerifyResultLineSuccess(t *testing.T) {
	out := verifyResultLine(event.LevelInfo, "✓")
	if !strings.Contains(out, "verified") {
		t.Errorf("passing verify should report 'verified', got %q", out)
	}
	if !strings.Contains(out, G) {
		t.Errorf("passing verify should be green, got %q", out)
	}
}

// A failing verify must surface its detail (which step, how many diagnostics) so
// the user sees the self-repair loop working instead of silence.
func TestVerifyResultLineFailureShowsDetail(t *testing.T) {
	out := verifyResultLine(event.LevelWarn, "✗ build (2 diagnostics)")
	if !strings.Contains(out, "✗ build (2 diagnostics)") {
		t.Errorf("failing verify detail dropped, got %q", out)
	}
}

// A "skipped" verify (no toolchain ran) must NOT render as a green "✓ verified" —
// it shows the skip message dim, so the user is never told unverified code passed.
func TestVerifyResultLineSkipIsNotAGreenCheck(t *testing.T) {
	out := verifyResultLine(event.LevelInfo, "↷ verify skipped — no build/test toolchain ran")
	if strings.Contains(out, "✓ verified") {
		t.Errorf("a skipped verify must not claim '✓ verified', got %q", out)
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("skip message should be shown, got %q", out)
	}
	if !strings.Contains(out, D) { // dim, not green
		t.Errorf("skip should render dim, got %q", out)
	}
}
