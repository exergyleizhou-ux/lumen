package render

import (
	"strings"
	"testing"
)

// Single-underscore _emphasis_ is standard CommonMark and common in model
// output, but was rendered as a literal. It must italicize — WITHOUT mangling
// snake_case identifiers (CommonMark forbids intraword underscore emphasis).
func TestMarkdownUnderscoreItalic(t *testing.T) {
	if out := Markdown("this is _important_ text"); !strings.Contains(out, ansiItalic) {
		t.Errorf("underscore italic not applied: %q", out)
	}
	// snake_case must NOT be italicized
	if out := Markdown("call my_var_name now"); strings.Contains(out, ansiItalic) {
		t.Errorf("snake_case wrongly italicized: %q", out)
	}
	// __bold__ must still be bold, not italic (consumed first)
	if out := Markdown("this is __strong__ text"); !strings.Contains(out, ansiBold) {
		t.Errorf("double-underscore bold lost: %q", out)
	}
}
