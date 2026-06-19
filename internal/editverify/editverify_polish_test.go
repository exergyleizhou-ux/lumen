package editverify

import (
	"strings"
	"testing"
)

// ruff lints the WHOLE repo (`ruff check .`), so a lint failure may be a
// pre-existing violation unrelated to the edit. Like the test step, it must get
// the "may predate your edit" caveat so the model doesn't go "fix" unrelated
// files. (build stays the edit's responsibility — that's the dependent-break
// signal.)
func TestFormatFeedback_LintFailureGetsPredateCaveat(t *testing.T) {
	out := FormatFeedback(Result{Failed: &Step{Name: "lint"}}, 1, 3)
	if !strings.Contains(out, "predate") {
		t.Errorf("a whole-repo lint failure should get the predate caveat, got: %q", out)
	}
}

// A build failure must NOT get the caveat — an edit that breaks the build (incl.
// a dependent package) is the edit's responsibility.
func TestFormatFeedback_BuildFailureNoCaveat(t *testing.T) {
	out := FormatFeedback(Result{Failed: &Step{Name: "build"}}, 1, 3)
	if strings.Contains(out, "predate") {
		t.Errorf("a build failure should NOT get the predate caveat, got: %q", out)
	}
}

// Modern Node/TS extensions (.mjs/.cjs/.mts/.cts) must be detected as JS/TS so
// the verify loop runs for them instead of silently doing nothing.
func TestDetectLanguages_ModernJSExtensions(t *testing.T) {
	for _, ext := range []string{".mjs", ".cjs", ".mts", ".cts"} {
		langs := detectLanguages([]string{"mod" + ext})
		ok := false
		for _, l := range langs {
			if l == "js" {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s should be detected as js, got %v", ext, langs)
		}
	}
}
