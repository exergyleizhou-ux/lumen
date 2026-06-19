package builtin

import "unicode/utf8"

// cutRunes returns s truncated to at most n bytes WITHOUT splitting a multibyte
// rune. Tool-output truncations used a raw s[:n] byte slice, which can cut
// through the middle of a CJK/emoji rune and emit invalid UTF-8 to the model
// (and the terminal). When n lands mid-rune, back up to the preceding rune
// boundary.
func cutRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	// Back up from n to the start of the rune that straddles the cap so we never
	// emit a partial rune.
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
