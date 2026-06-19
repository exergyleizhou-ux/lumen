package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// After a new message, addChatEntry auto-scrolls by setting scrollPos to
// len(entries). The render loop is top-anchored (scrollPos = first visible
// index), so scrollPos == len(entries) rendered ZERO entries — the chat went
// blank after every message. The latest message must stay visible.
func TestRenderChatShowsLatestAfterAutoScroll(t *testing.T) {
	m := NewModel()
	for i := 0; i < 5; i++ {
		m.addChatEntry(TuiMsg{Role: "assistant", Content: fmt.Sprintf("message-%d", i)})
	}
	out := m.renderChat(80, 24)
	if !strings.Contains(out, "message-4") {
		t.Errorf("latest message not visible after auto-scroll (chat blank); got:\n%q", out)
	}
}

// Even with more entries than fit, the most recent ones must show (bottom-anchored).
func TestRenderChatBottomAnchoredWhenOverflowing(t *testing.T) {
	m := NewModel()
	for i := 0; i < 60; i++ {
		m.addChatEntry(TuiMsg{Role: "assistant", Content: fmt.Sprintf("msg-%d", i)})
	}
	out := m.renderChat(80, 24)
	if !strings.Contains(out, "msg-59") {
		t.Errorf("most recent message not visible when overflowing; got:\n%q", out)
	}
}

// The status bar must fit on a single row. lipgloss .Width() WRAPS content wider
// than the inner width (it doesn't truncate), and View() reserves exactly one
// row for the status bar — so an over-wide bar wrapped to 2-3 rows and pushed
// the chat body off-screen.
func TestRenderStatusFitsOneRowOnNarrowTerminal(t *testing.T) {
	m := NewModel()
	m.Update(StatusMsg{Model: "deepseek-chat-extra-long-model-name", Provider: "deepseek", Mode: "default"})
	m.Update(VerifyMsg{State: "fail", Detail: "build failed: a long detail string that is quite wide"})
	out := m.renderStatus(40)
	if h := lipgloss.Height(out); h != 1 {
		t.Errorf("status bar must fit one row on a narrow (40-col) terminal, got %d rows:\n%q", h, out)
	}
}
