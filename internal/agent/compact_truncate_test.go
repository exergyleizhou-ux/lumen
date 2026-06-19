package agent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// truncateForCompact byte-sliced the input, which splits a multibyte (CJK) rune
// and feeds invalid UTF-8 into the compaction model. The cut must land on a rune
// boundary.
func TestTruncateForCompact_RuneSafe(t *testing.T) {
	s := strings.Repeat("中", 100) // 300 bytes, 3 bytes/rune
	for _, maxLen := range []int{10, 47, 50, 100, 151} {
		out := truncateForCompact(s, maxLen)
		if !utf8.ValidString(out) {
			t.Errorf("maxLen=%d produced invalid UTF-8 (split a rune): %q", maxLen, out)
		}
	}
}
