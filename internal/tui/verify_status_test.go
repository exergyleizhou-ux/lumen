package tui

import "testing"

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
