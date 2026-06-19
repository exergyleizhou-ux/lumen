package render

import (
	"fmt"
	"strings"
	"testing"
)

// A small change deep inside a large file. The O(n*m) LCS path (a) risks a huge
// DP matrix — a 50k-line edit would allocate tens of GB — and (b) emits the
// leading common lines as context, so the 200-line render cap truncates BEFORE
// reaching the actual change. The large-input fallback must trim common context
// and surface the change.
func TestDiff_LargeFileShowsTheChangeNotJustLeadingContext(t *testing.T) {
	var a []string
	for i := 0; i < 1000; i++ {
		a = append(a, fmt.Sprintf("ctx %d", i))
	}
	a = append(a, "OLD_CHANGED_LINE")
	for i := 0; i < 1000; i++ {
		a = append(a, fmt.Sprintf("tail %d", i))
	}
	b := append([]string(nil), a...)
	b[1000] = "NEW_CHANGED_LINE"

	out := Diff("x.txt", strings.Join(a, "\n"), strings.Join(b, "\n"))

	if !strings.Contains(out, "OLD_CHANGED_LINE") || !strings.Contains(out, "NEW_CHANGED_LINE") {
		t.Fatal("the actual change must be visible in a large-file diff, not buried under truncated leading context")
	}
}
