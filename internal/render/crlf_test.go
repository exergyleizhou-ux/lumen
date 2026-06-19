package render

import (
	"strings"
	"testing"
)

// A file with CRLF (\r\n) line endings was split only on \n, leaving a trailing
// \r on every line. The terminal renders that bare CR as a literal ^M at the end
// of each diff line (and can also reposition the cursor), garbling the preview.
func TestDiff_StripsCRLFCarriageReturns(t *testing.T) {
	out := Diff("x.txt", "alpha\r\nbeta\r\n", "alpha\r\nBETA\r\n")
	if strings.ContainsRune(out, '\r') {
		t.Fatalf("a CRLF file's diff must not render literal carriage returns (^M): %q", out)
	}
}
