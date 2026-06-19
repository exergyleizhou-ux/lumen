package render

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TruncateVisible measured RUNES, not display columns, so a CJK string (each
// rune 2 columns wide) overflowed its budget by ~2x — misaligning every status
// bar / list row it bounds. The output must fit the column budget (+ ellipsis).
func TestTruncateVisible_CountsCJKDisplayColumns(t *testing.T) {
	// 6 wide runes = 12 columns; budget 4.
	out := TruncateVisible("你好世界你好", 4)
	if w := ansi.StringWidth(out); w > 5 {
		t.Fatalf("TruncateVisible produced %d columns for a 4-col budget: %q (CJK must count as 2 cols)", w, out)
	}
}

// A mix of ANSI styling + wide runes must still be measured by display columns.
func TestTruncateVisible_CJKWithANSI(t *testing.T) {
	out := TruncateVisible("\x1b[31m你好世界\x1b[0m", 4)
	if w := ansi.StringWidth(out); w > 5 {
		t.Fatalf("styled CJK truncation = %d cols, want <= ~4: %q", w, out)
	}
}
