package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// renderChat appended ALL wrapped lines of a long entry without respecting the
// per-panel msgH budget, so one tall message overflowed the chat panel past its
// allotted height. The rendered panel must fit within h.
func TestRenderChatRespectsHeightBudget(t *testing.T) {
	tall := NewModel()
	tall.addChatEntry(TuiMsg{Role: "assistant", Content: strings.Repeat("a line of text\n", 50)})
	tallH := lipgloss.Height(tall.renderChat(80, 10))

	short := NewModel()
	short.addChatEntry(TuiMsg{Role: "assistant", Content: "short"})
	shortH := lipgloss.Height(short.renderChat(80, 10))

	// The panel must be a fixed size; a 50-line message must not grow it.
	if tallH != shortH {
		t.Errorf("a tall message overflowed the panel: tall=%d short=%d rows", tallH, shortH)
	}
}
