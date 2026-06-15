// renderer.go — clean terminal output. Strips Markdown markers.
package main

import (
	"fmt"
	"regexp"
	"strings"
)

// acc accumulates partial lines between streaming chunks.
var acc struct {
	buf strings.Builder
}

// renderText prints agent text, stripping Markdown on flush boundaries.
func renderText(text string) {
	for _, ch := range text {
		acc.buf.WriteRune(ch)
		// Flush on newline or period — natural sentence boundary
		if ch == '\n' || ch == '.' || ch == '。' {
			flushLine()
		}
	}
}

// flushBuffer forces any remaining accumulated text out.
func flushBuffer() {
	if acc.buf.Len() > 0 {
		line := acc.buf.String()
		acc.buf.Reset()
		line = cleanMarkdown(line)
		fmt.Print(line)
	}
}

func flushLine() {
	if acc.buf.Len() == 0 { return }
	line := acc.buf.String()
	acc.buf.Reset()
	line = cleanMarkdown(line)
	fmt.Print(line)
}

// cleanMarkdown strips common Markdown formatting from a text fragment.
func cleanMarkdown(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")

	// Strip inline code
	s = reCode.ReplaceAllString(s, "$1")

	// Strip table separators
	s = reTableSep.ReplaceAllString(s, "")

	// Strip leading # headings
	s = reHead.ReplaceAllString(s, "$1")

	return s
}

var (
	reCode     = regexp.MustCompile("`([^`\n]+)`")
	reTableSep = regexp.MustCompile(`(?m)^\|[\-:| ]+\|$`)
	reHead     = regexp.MustCompile(`(?m)^#{1,3}\s+`)
)
