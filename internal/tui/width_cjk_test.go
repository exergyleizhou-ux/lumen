package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// trunc measured runes, not display columns, so CJK overflowed its budget.
func TestTrunc_CountsCJKDisplayColumns(t *testing.T) {
	out := trunc("你好世界世界", 4) // 12 cols, budget 4
	if w := ansi.StringWidth(out); w > 5 {
		t.Fatalf("trunc produced %d columns for a 4-col budget: %q", w, out)
	}
}

// wordWrap byte-sliced the string (paragraph[:width]), which both miscounts
// width AND splits multibyte runes — corrupting CJK chat text into invalid
// UTF-8. Every wrapped line must be valid UTF-8 and within the column width.
func TestWordWrap_RuneSafeAndColumnBounded(t *testing.T) {
	const width = 6
	out := wordWrap("你好世界这是一段没有空格的中文文本", width)
	for _, line := range strings.Split(out, "\n") {
		if !utf8.ValidString(line) {
			t.Fatalf("wrapped line is not valid UTF-8 (a rune was split): %q", line)
		}
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("wrapped line %q is %d columns, want <= %d", line, w, width)
		}
	}
	// No content lost (ignoring the inserted newlines).
	if got := strings.ReplaceAll(out, "\n", ""); got != "你好世界这是一段没有空格的中文文本" {
		t.Fatalf("wordWrap lost/changed content: %q", got)
	}
}
