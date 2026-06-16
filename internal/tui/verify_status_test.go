package tui

import (
	"strings"
	"testing"
)

// TestRenderStatusSegmentsSpaced guards against the right-side status segments
// being glued together (regression: JoinHorizontal without separators).
func TestRenderStatusSegmentsSpaced(t *testing.T) {
	m := NewModel()
	m.Update(StatusMsg{Model: "deepseek-chat", Provider: "deepseek", Mode: "default"})
	m.Update(VerifyMsg{State: "ok"})
	out := m.renderStatus(120)
	// The cache "♻" and verify "✓ verified" must not be directly adjacent.
	if strings.Contains(out, "%✓") || strings.Contains(out, "verified⚙") {
		t.Errorf("status segments are glued (missing separators):\n%q", out)
	}
	if !strings.Contains(out, "✓ verified") {
		t.Errorf("verify segment missing: %q", out)
	}
}

func TestVerifyMsgUpdatesStatusBar(t *testing.T) {
	m := NewModel()

	m.Update(VerifyMsg{State: "running"})
	if m.status.verifyState != "running" {
		t.Errorf("running: got %q", m.status.verifyState)
	}

	m.Update(VerifyMsg{State: "fail", Detail: "build failed (2)"})
	if m.status.verifyState != "fail" || m.status.verifyDetail != "build failed (2)" {
		t.Errorf("fail: got state=%q detail=%q", m.status.verifyState, m.status.verifyDetail)
	}

	// A StatusMsg must NOT clobber verify state (separate message types).
	m.Update(StatusMsg{Model: "deepseek-chat", Mode: "default"})
	if m.status.verifyState != "fail" {
		t.Errorf("StatusMsg clobbered verify state: got %q", m.status.verifyState)
	}

	m.Update(VerifyMsg{State: ""})
	if m.status.verifyState != "" {
		t.Errorf("clear: got %q", m.status.verifyState)
	}
}
