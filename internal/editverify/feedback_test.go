package editverify

import (
	"strings"
	"testing"
)

func TestFormatFeedback_OK(t *testing.T) {
	if got := FormatFeedback(Result{OK: true}, 1, 3); got != "" {
		t.Errorf("OK result should produce no feedback, got %q", got)
	}
}

func TestFormatFeedback_Diagnostics(t *testing.T) {
	r := Result{
		OK:     false,
		Failed: &Step{Name: "test", Args: []string{"go", "test", "./internal/foo"}},
		Diagnostics: []Diagnostic{
			{File: "internal/foo/bar.go", Line: 42, Col: 6, Msg: "undefined: helper", Sev: "error"},
			{File: "internal/foo/bar_test.go", Line: 13, Msg: "expected 3, got 0", Sev: "error"},
		},
	}
	got := FormatFeedback(r, 1, 3)
	for _, want := range []string{
		"verify failed at step `test`",
		"go test ./internal/foo",
		"internal/foo/bar.go:42:6: undefined: helper",
		"internal/foo/bar_test.go:13: expected 3, got 0",
		"(repair cycle 1/3)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("feedback missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestFormatFeedback_FallbackToOutput(t *testing.T) {
	r := Result{
		OK:     false,
		Failed: &Step{Name: "build", Args: []string{"go", "build", "./..."}},
		Output: "some unstructured\ncompiler noise",
	}
	got := FormatFeedback(r, 2, 3)
	if !strings.Contains(got, "some unstructured") || !strings.Contains(got, "compiler noise") {
		t.Errorf("should fall back to raw output, got %q", got)
	}
	if !strings.Contains(got, "(repair cycle 2/3)") {
		t.Errorf("missing cycle marker, got %q", got)
	}
}

func TestFormatFeedback_LSPDiags(t *testing.T) {
	// LSP diagnostics when build passes — informational, not failure
	r := Result{
		OK: true,
		LSPDiags: []Diagnostic{
			{File: "internal/foo/bar.go", Line: 10, Col: 2, Msg: "unused variable", Sev: "warning"},
			{File: "internal/foo/bar.go", Line: 15, Msg: "should use strings.Builder", Sev: "warning"},
		},
	}
	got := FormatFeedback(r, 1, 3)
	if got == "" {
		t.Fatal("LSP diags should produce feedback even when OK=true")
	}
	if !strings.Contains(got, "gopls reported issues") {
		t.Errorf("missing gopls header, got %q", got)
	}
	if !strings.Contains(got, "unused variable") {
		t.Errorf("missing LSP diagnostic, got %q", got)
	}
	if strings.Contains(got, "repair cycle") {
		t.Errorf("LSP-only feedback should NOT show repair cycle, got %q", got)
	}
}

func TestFormatFeedback_LSPDiagsEmpty(t *testing.T) {
	// Fully clean — no build failure, no LSP diags
	r := Result{OK: true}
	got := FormatFeedback(r, 1, 3)
	if got != "" {
		t.Errorf("fully clean should return empty, got %q", got)
	}
}
