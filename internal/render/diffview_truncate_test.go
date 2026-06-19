package render

import (
	"fmt"
	"strings"
	"testing"
)

// When a diff is truncated at the render cap, the user is reviewing an edit
// before approving it — a bare "(diff truncated)" hides whether 5 or 5000 lines
// were elided. Report the hidden count so the magnitude is visible.
func TestDiffTruncationShowsHiddenCount(t *testing.T) {
	var after strings.Builder
	for i := 0; i < 250; i++ { // 50 over the 200-line cap
		fmt.Fprintf(&after, "line %d\n", i)
	}
	out := plain(Diff("x.txt", "", after.String()))
	if !strings.Contains(out, "50 more lines") {
		tail := out
		if i := strings.LastIndex(strings.TrimRight(out, "\n"), "\n"); i >= 0 {
			tail = out[i+1:]
		}
		t.Errorf("truncation should report the 50 hidden lines; tail was: %q", tail)
	}
}
