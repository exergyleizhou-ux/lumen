package render

import (
	"strings"
	"testing"
)

// Rust lifetimes ('a) must not be treated as string/char openers. Previously the
// single-quote scanner paired unrelated apostrophes and painted whole spans of a
// signature green. A lifetime-only signature has no real strings → no green.
func TestHighlightRustLifetimeNotString(t *testing.T) {
	out := Highlight("fn f<'a>(x: &'a str) -> &'a str { x }", "rust")
	if strings.Contains(out, colString) {
		t.Errorf("rust lifetimes mis-colored as strings (green present): %q", out)
	}
}

// A stray apostrophe in a char-literal language must not swallow the rest of the
// line into a green string span.
func TestHighlightGoStrayApostropheNoSwallow(t *testing.T) {
	out := Highlight("a 'b c d", "go")
	if strings.Contains(out, colString) {
		t.Errorf("stray apostrophe swallowed the line as a string: %q", out)
	}
}

// Guard: a genuine Go rune/char literal is still colorized.
func TestHighlightGoCharLiteralStillColored(t *testing.T) {
	out := Highlight("c := 'a'", "go")
	if !strings.Contains(out, colString+"'a'"+ansiReset) {
		t.Errorf("go char literal lost its string color: %q", out)
	}
}

// Guard: single-quoted STRINGS in Python (not a char-literal language) must
// still be colorized.
func TestHighlightPythonSingleQuoteString(t *testing.T) {
	out := Highlight("x = 'hello'", "python")
	if !strings.Contains(out, colString+"'hello'"+ansiReset) {
		t.Errorf("python single-quoted string lost its color: %q", out)
	}
}

// A number must stop at the first non-numeric letter, not greedily swallow a
// trailing identifier (10abc, 3px were all colored as one number).
func TestHighlightNumberStopsAtLetters(t *testing.T) {
	out := Highlight("y := 10abc", "go")
	if !strings.Contains(out, colNumber+"10"+ansiReset) {
		t.Errorf("number did not stop at letters (expected 10 colored alone): %q", out)
	}
}

// Guard: hex literals are still colored as a single number.
func TestHighlightHexNumberStillWorks(t *testing.T) {
	out := Highlight("x := 0xFF", "go")
	if !strings.Contains(out, colNumber+"0xFF"+ansiReset) {
		t.Errorf("hex literal lost its number color: %q", out)
	}
}
