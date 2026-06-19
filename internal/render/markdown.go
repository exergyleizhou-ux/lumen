package render

import (
	"regexp"
	"strings"
)

// Inline-formatting patterns. Applied in order so ** is consumed before *.
// The star emphasis patterns require flanking (no whitespace just inside the
// delimiters) so arithmetic ("a * b") and shell globs ("*.go") are not
// mistaken for emphasis — matching CommonMark's left/right-flanking rule.
var (
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reBoldStar   = regexp.MustCompile(`\*\*([^*\s]|[^*\s][^*]*?[^*\s])\*\*`)
	reBoldUnder  = regexp.MustCompile(`__([^_]+)__`)
	reItalicStar = regexp.MustCompile(`\*([^*\s]|[^*\s][^*]*?[^*\s])\*`)
	// Single-underscore italic, guarded so it doesn't fire on snake_case: the
	// delimiters must be flanked by a non-word char (start/space/punctuation),
	// which Go regexp (no lookaround) captures in groups 1 and 3 and the
	// replacement restores.
	reItalicUnder = regexp.MustCompile(`(^|[^\p{L}\p{N}_])_([^_\s]|[^_\s][^_]*?[^_\s])_($|[^\p{L}\p{N}_])`)
	reHeading     = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reOrderedList = regexp.MustCompile(`^(\s*)(\d+)([.)])\s+(.*)$`)
)

// Markdown renders a markdown string into ANSI-styled terminal text. It handles
// headings, bold/italic, inline code, bullet and blockquote lines, and fenced
// code blocks (which are syntax-highlighted via Highlight). It is line-based and
// resilient to partial/odd input — it never errors.
func Markdown(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Fenced code block.
		if strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			i++
			var code []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // consume closing fence
			}
			out = append(out, renderCodeBlock(strings.Join(code, "\n"), lang))
			continue
		}

		out = append(out, renderLine(line))
		i++
	}
	return strings.Join(out, "\n")
}

// renderCodeBlock highlights a fenced block and prefixes each line with a dim
// gutter for visual separation from prose.
func renderCodeBlock(code, lang string) string {
	hl := Highlight(code, lang)
	lines := strings.Split(hl, "\n")
	gutter := ansiDim + "│ " + ansiReset
	for i, l := range lines {
		lines[i] = gutter + l
	}
	return strings.Join(lines, "\n")
}

// renderLine applies block-level prose styling to a single line.
func renderLine(line string) string {
	trimmed := strings.TrimSpace(line)

	if m := reHeading.FindStringSubmatch(line); m != nil {
		// The heading style must span the whole line, but inline() emits its own
		// reset after each span. Re-assert the style after every such reset so
		// text following an inline span stays styled.
		style := ansiBold + ansiWhite + ansiUnder
		return style + strings.ReplaceAll(inline(m[2]), ansiReset, ansiReset+style) + ansiReset
	}

	if strings.HasPrefix(trimmed, "> ") {
		body := strings.TrimPrefix(trimmed, "> ")
		return ansiDim + "▏ " + ansiReset + inline(body)
	}

	if bullet := bulletPrefix(trimmed); bullet != "" {
		body := strings.TrimSpace(trimmed[len(bullet):])
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		return indent + ansiCyan + "• " + ansiReset + inline(body)
	}

	// Ordered (numbered) lists get the same marker styling as bullets, keeping
	// the original ordinal and delimiter (e.g. "1." or "2)").
	if m := reOrderedList.FindStringSubmatch(line); m != nil {
		return m[1] + ansiCyan + m[2] + m[3] + ansiReset + " " + inline(m[4])
	}

	return inline(line)
}

// bulletPrefix returns the matched unordered-list marker ("- ", "* ", "+ ") or "".
func bulletPrefix(trimmed string) string {
	for _, p := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(trimmed, p) {
			return p
		}
	}
	return ""
}

// inline applies inline styling: code spans first (so their content is not
// re-styled), then bold, then italic.
func inline(s string) string {
	s = reInlineCode.ReplaceAllString(s, ansiCyan+"$1"+ansiReset)
	s = reBoldStar.ReplaceAllString(s, ansiBold+"$1"+ansiReset)
	s = reBoldUnder.ReplaceAllString(s, ansiBold+"$1"+ansiReset)
	s = reItalicUnder.ReplaceAllString(s, "${1}"+ansiItalic+"${2}"+ansiReset+"${3}")
	s = reItalicStar.ReplaceAllString(s, ansiItalic+"$1"+ansiReset)
	return s
}
