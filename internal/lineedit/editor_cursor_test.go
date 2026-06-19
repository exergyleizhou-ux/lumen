package lineedit

import "testing"

// cursorMoves must position the cursor correctly whether or not the content
// wrapped. The old logic ("\r" + CUF(offset)) is shown wrong inline.
func TestCursorMoves(t *testing.T) {
	const w = 10
	cases := []struct {
		name                 string
		total, offset, termW int
		want                 string
	}{
		// ── single row (no wrap) ──
		{"empty", 0, 0, w, ""},
		{"oneRow_atEnd", 5, 5, w, ""},           // cursor already at end after write
		{"oneRow_middle", 5, 3, w, "\r\x1b[3C"}, // back to col 0, forward 3
		{"oneRow_atStart", 5, 0, w, "\r"},       // col 0, no forward
		// ── wrapped: cursor at end needs NO move (Step 2 left it there) ──
		{"wrapped_atEnd", 25, 25, w, ""},
		// ── wrapped: target on row 0 (must walk up from the last row) ──
		{"wrapped_row0_middle", 25, 3, w, "\x1b[2A\r\x1b[3C"}, // endRow=2 → up 2
		// ── wrapped: target on row 1 ──
		{"wrapped_row1_middle", 25, 13, w, "\x1b[1A\r\x1b[3C"}, // endRow=2, targetRow=1 → up 1
		// ── wrapped: target at a wrapped-row start (col 0) ──
		{"wrapped_rowStart", 25, 10, w, "\x1b[1A\r"}, // targetRow=1, col 0
		{"wrapped_atStart", 25, 0, w, "\x1b[2A\r"},   // targetRow=0, col 0
		// ── exact-fill row boundaries (deferred-wrap) ──
		{"exactFill_oneRow_atEnd", 10, 10, w, ""},                 // pending wrap on row 0; no move
		{"exactFill_oneRow_middle", 10, 4, w, "\r\x1b[4C"},        // endRow=(10-1)/10=0
		{"exactFill_twoRows_sameRow", 20, 13, w, "\r\x1b[3C"},     // endRow=1, target row1 col3 → up 0
		{"exactFill_twoRows_upRow", 20, 3, w, "\x1b[1A\r\x1b[3C"}, // endRow=1, target row0 col3 → up 1
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cursorMoves(c.total, c.offset, c.termW); got != c.want {
				t.Errorf("cursorMoves(%d,%d,%d) = %q, want %q", c.total, c.offset, c.termW, got, c.want)
			}
		})
	}
}

// Guard the inputs the editor can actually produce.
func TestCursorMovesClampsBadInput(t *testing.T) {
	if got := cursorMoves(5, 99, 10); got != "" { // offset past end clamps to end → no move
		t.Errorf("offset>total should clamp to end (empty), got %q", got)
	}
	if got := cursorMoves(5, -3, 0); got == "" {
		// termW<=0 defaults to 80; negative offset clamps to 0 → "\r"
		t.Errorf("termW<=0 should default and not panic/empty for a mid-cursor, got %q", got)
	}
}
