package lineedit

import (
	"fmt"
	"strings"
)

// cursorMoves returns the terminal escape sequence that moves the cursor from
// the end of just-written content to the target column-offset `cursorOffset`,
// where `total` is the full display width of the written content (prompt+text)
// and termW is the terminal width. Both offsets are measured in display columns
// from the very start of the prompt.
//
// The buggy predecessor emitted only `\r` + CUF(cursorOffset): a single
// horizontal move. CUF cannot cross a wrapped row and clamps at the right
// margin, so when the content wrapped, the cursor landed on the LAST wrap row at
// the wrong column. This walks UP to the target row first.
//
// Assumes a deferred-wrap ("phantom column") terminal — i.e. writing exactly
// termW columns leaves the cursor pending at the end of that row, not on the
// next one — which is how every mainstream terminal behaves. Under that model
// the cursor, after writing `total` columns from row 0, sits on row
// (total-1)/termW.
func cursorMoves(total, cursorOffset, termW int) string {
	if termW <= 0 {
		termW = 80
	}
	if cursorOffset < 0 {
		cursorOffset = 0
	}
	if cursorOffset > total {
		cursorOffset = total
	}
	// Cursor already at the end of the written content — Step 2 left it there.
	if cursorOffset == total {
		return ""
	}
	// Row the cursor sits on after writing `total` columns (deferred wrap).
	endRow := 0
	if total > 0 {
		endRow = (total - 1) / termW
	}
	targetRow := cursorOffset / termW
	targetCol := cursorOffset % termW

	var b strings.Builder
	if up := endRow - targetRow; up > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", up) // cursor up `up` rows
	}
	b.WriteByte('\r') // column 0 of the target row
	if targetCol > 0 {
		fmt.Fprintf(&b, "\x1b[%dC", targetCol) // cursor forward to the target column
	}
	return b.String()
}
