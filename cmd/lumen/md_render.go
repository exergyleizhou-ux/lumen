package main

import (
	"fmt"
	"strings"
)

// mdState tracks Markdown tokens across streaming chunks.
var md struct {
	buf      []byte
	inBold   bool
	inItalic bool
	inCode   bool
	stars    int // consecutive * seen, reset when non-* char appears
}

// renderClean strips Markdown syntax from streaming agent output.
func renderClean(text string) string {
	for _, ch := range text {
		switch {
		case ch == '`':
			md.inCode = !md.inCode
			md.stars = 0

		case ch == '*':
			md.stars++
			if md.stars == 1 {
				// Wait to see if it's * or **
			} else if md.stars == 2 {
				md.inBold = !md.inBold
				md.stars = 0
			}

		default:
			// Non-star, non-backtick character
			if md.stars == 1 {
				// Single * followed by text = italic toggle
				if md.inBold {
					md.inBold = false
				} else {
					md.inItalic = !md.inItalic
				}
				md.stars = 0
			}
			if md.stars > 1 {
				md.stars = 0
			}
			md.buf = append(md.buf, byte(ch))
		}
	}

	// Flush on newline
	if strings.ContainsRune(text, '\n') {
		return flushMD()
	}
	return ""
}

func flushMD() string {
	if len(md.buf) == 0 { return "" }
	out := string(md.buf)
	fmt.Print(out)
	md.buf = md.buf[:0]
	return out
}

func flushBuffer() { flushMD() }
