package builtin

import (
	"testing"
	"unicode/utf8"
)

func TestCutRunes_NeverSplitsAMultibyteRune(t *testing.T) {
	// Each of 你好世界 is 3 bytes (12 total). A byte cap of 7 lands inside the
	// 3rd rune; the result must back up to a rune boundary (6 bytes = 你好).
	const s = "你好世界"

	got := cutRunes(s, 7)
	if !utf8.ValidString(got) {
		t.Fatalf("result is not valid UTF-8: %q (a rune was split)", got)
	}
	if got != "你好" {
		t.Fatalf("cutRunes(%q, 7) = %q, want 你好", s, got)
	}

	// Exact boundary, passthrough, ascii, and zero.
	if g := cutRunes(s, 6); g != "你好" {
		t.Fatalf("exact-boundary cut = %q, want 你好", g)
	}
	if g := cutRunes(s, 100); g != s {
		t.Fatalf("under-cap should pass through, got %q", g)
	}
	if g := cutRunes("abcdef", 3); g != "abc" {
		t.Fatalf("ascii cut = %q, want abc", g)
	}
	if g := cutRunes(s, 0); g != "" {
		t.Fatalf("zero cap = %q, want empty", g)
	}
}
