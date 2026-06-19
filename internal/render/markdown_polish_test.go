package render

import (
	"strings"
	"testing"
)

// A heading that contains an inline span (code/bold/italic) must keep its
// heading style for the WHOLE line. inline() emits its own ansiReset after each
// span, which previously cleared the heading's bold/white/underline so every
// character after the first span rendered as plain text.
func TestMarkdownHeadingStyleSurvivesInline(t *testing.T) {
	style := ansiBold + ansiWhite + ansiUnder
	out := Markdown("# A `b` C")
	if !strings.Contains(out, style+" C") {
		t.Errorf("heading style not re-asserted after inline span; trailing text unstyled: %q", out)
	}
}

// Numbered lists should get the same marker styling as bullet lists, not render
// as undistinguished prose. The ordinal must be preserved.
func TestMarkdownOrderedListStyled(t *testing.T) {
	out := Markdown("1. first\n2) second")
	if !strings.Contains(out, ansiCyan) {
		t.Errorf("ordered-list markers not styled (no cyan marker): %q", out)
	}
	p := plain(out)
	for _, want := range []string{"1.", "2)", "first", "second"} {
		if !strings.Contains(p, want) {
			t.Errorf("ordered list lost %q: %q", want, p)
		}
	}
}

// Spaced asterisks (arithmetic, shell globs) must NOT be treated as italic
// emphasis — CommonMark requires the delimiters to be flanking (no adjacent
// whitespace on the inside).
func TestMarkdownSpacedAsteriskNotItalic(t *testing.T) {
	out := Markdown("a * b * c")
	if strings.Contains(out, ansiItalic) {
		t.Errorf("spaced asterisks should not italicize: %q", out)
	}
	if got := plain(out); got != "a * b * c" {
		t.Errorf("spaced-asterisk text altered: %q", got)
	}
}

// Regression guard: genuine *italic* and **bold** must still render.
func TestMarkdownEmphasisStillWorks(t *testing.T) {
	if out := Markdown("this is *italic* here"); !strings.Contains(out, ansiItalic) {
		t.Errorf("real italic lost: %q", out)
	}
	if out := Markdown("this is **bold** here"); !strings.Contains(out, ansiBold) {
		t.Errorf("real bold lost: %q", out)
	}
}
