package tui

import (
	"strings"
	"testing"
)

func TestRenderStatus_verifyRunning(t *testing.T) {
	m := NewModel()
	m.status.status = StatusMsg{
		Model:    "deepseek-chat",
		Provider: "deepseek",
		Mode:     "default",
		TokensIn: 10000, TokensOut: 4000, CacheHit: 9900,
		Cost: 0.0039, Steps: 3, Turns: 2,
	}
	m.status.verifyState = "running"

	out := m.renderStatus(120)
	if !strings.Contains(out, "⟳ verifying") {
		t.Fatalf("expected '⟳ verifying…' in status, got: %s", out)
	}
}

func TestRenderStatus_verifyOk(t *testing.T) {
	m := NewModel()
	m.status.status = StatusMsg{
		Model:    "deepseek-chat",
		Provider: "deepseek",
		Mode:     "default",
		TokensIn: 10000, TokensOut: 4000, CacheHit: 9900,
		Cost: 0.0039, Steps: 3, Turns: 2,
	}
	m.status.verifyState = "ok"

	out := m.renderStatus(120)
	if !strings.Contains(out, "✓ verified") {
		t.Fatalf("expected '✓ verified' in status, got: %s", out)
	}
}

func TestRenderStatus_verifyFail(t *testing.T) {
	m := NewModel()
	m.status.status = StatusMsg{
		Model:    "deepseek-chat",
		Provider: "deepseek",
		Mode:     "default",
		TokensIn: 10000, TokensOut: 4000, CacheHit: 9900,
		Cost: 0.0039, Steps: 3, Turns: 2,
	}
	m.status.verifyState = "fail"
	m.status.verifyDetail = "build failed (2)"

	out := m.renderStatus(120)
	if !strings.Contains(out, "build failed (2)") {
		t.Fatalf("expected fail detail in status, got: %s", out)
	}
	if !strings.Contains(out, "✗") {
		t.Fatalf("expected '✗' marker in status, got: %s", out)
	}
}

func TestRenderStatus_verifyEmpty(t *testing.T) {
	m := NewModel()
	m.status.status = StatusMsg{
		Model:    "deepseek-chat",
		Provider: "deepseek",
		Mode:     "default",
		TokensIn: 10000, TokensOut: 4000, CacheHit: 9900,
		Cost: 0.0039, Steps: 3, Turns: 2,
	}
	// verifyState is empty (the zero value)

	out := m.renderStatus(120)
	if strings.Contains(out, "⟳ verifying") || strings.Contains(out, "✓ verified") || strings.Contains(out, "✗") {
		t.Fatalf("empty verifyState should produce no verify text, got: %s", out)
	}
}

func TestRenderStatus_verifyFailDetailTruncated(t *testing.T) {
	m := NewModel()
	m.status.status = StatusMsg{
		Model:    "deepseek-chat",
		Provider: "deepseek",
		Mode:     "default",
		TokensIn: 10000, TokensOut: 4000, CacheHit: 9900,
		Cost: 0.0039, Steps: 3, Turns: 2,
	}
	m.status.verifyState = "fail"
	m.status.verifyDetail = "this is a very long error message that goes beyond forty characters easily"

	out := m.renderStatus(120)
	// Should contain truncated version (≤ 40 chars + …)
	if strings.Contains(out, "goes beyond forty characters easily") {
		t.Fatalf("detail should be truncated at 40 chars, got: %s", out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("truncated detail should end with …, got: %s", out)
	}
}

func TestRenderStatus_statusMsgDoesNotOverwriteVerify(t *testing.T) {
	m := NewModel()
	// Set verify state first
	m.status.verifyState = "fail"
	m.status.verifyDetail = "test error"

	// Then update status — verify should remain
	m.status.status = StatusMsg{
		Model:    "deepseek-chat",
		Provider: "deepseek",
		Mode:     "default",
		TokensIn: 10000, TokensOut: 4000,
	}
	// verifyState should still be "fail" (statusMsg does NOT touch it)
	if m.status.verifyState != "fail" {
		t.Error("StatusMsg should not overwrite verifyState")
	}
	if m.status.verifyDetail != "test error" {
		t.Error("StatusMsg should not overwrite verifyDetail")
	}
}
