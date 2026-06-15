// Package terminal provides terminal utilities: ANSI stripping, output
// formatting, color, width-aware truncation, Box, Table, ProgressBar.
// Pure Go — no external dependencies beyond the standard library.
package terminal

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// ── ANSI stripping ─────────────────────────────────────────

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][0-9;]*[^\a]*\a|\x1b\[[0-9;]*m|\x1b\][0-9;]*\x07|\x1b\([0-9;]*[a-zA-Z]|\r`)

// StripANSI removes ANSI escape sequences and carriage returns.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// ── Terminal detection ────────────────────────────────────

// DetectTermSize returns the current terminal dimensions, or 80x24.
func DetectTermSize() (cols, rows int) {
	if fd := int(os.Stdout.Fd()); fd > 0 {
		if w, h, err := term.GetSize(fd); err == nil {
			return w, h
		}
	}
	return 80, 24
}

// IsTerminal reports whether stdout is a terminal.
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// ── Style constants ────────────────────────────────────────

const (
	Reset    = "\x1b[0m"
	Bold     = "\x1b[1m"
	Dim      = "\x1b[2m"
	RedFG    = "\x1b[31m"
	GreenFG  = "\x1b[32m"
	YellowFG = "\x1b[33m"
	BlueFG   = "\x1b[34m"
	CyanFG   = "\x1b[36m"
	WhiteFG  = "\x1b[37m"
	GrayFG   = "\x1b[90m"
)

func Color(code, text string) string { return code + text + Reset }
func BoldText(text string) string   { return Bold + text + Reset }
func DimText(text string) string    { return Dim + text + Reset }

// ── Width-aware truncation ─────────────────────────────────

func TruncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	width := 0
	for i, r := range s {
		width += runeWidth(r)
		if width > maxWidth {
			return s[:i]
		}
	}
	return s
}

func DisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	switch {
	case r == utf8.RuneError:
		return 1
	case r >= 0x2e80 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff01 && r <= 0xff60,
		r >= 0x20000 && r <= 0x2fffd:
		return 2
	default:
		return 1
	}
}

// ── Output formatting ──────────────────────────────────────

func Box(title, content string) string {
	cols, _ := DetectTermSize()
	if cols < 40 {
		cols = 40
	}
	innerW := cols - 2
	var sb strings.Builder

	top := "┌"
	if title != "" {
		top += "─ " + BoldText(title) + " "
	}
	for DisplayWidth(StripANSI(top)) < cols-1 {
		top += "─"
	}
	top += "┐\n"
	sb.WriteString(top)

	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			sb.WriteString("│" + strings.Repeat(" ", innerW) + "│\n")
			continue
		}
		remaining := line
		for remaining != "" {
			cut := len(remaining)
			if DisplayWidth(remaining) > innerW-2 {
				cut = findWrap(remaining, innerW-2)
			}
			chunk := remaining[:cut]
			pad := innerW - 2 - DisplayWidth(chunk)
			if pad < 0 {
				pad = 0
			}
			sb.WriteString(fmt.Sprintf("│ %s%s │\n", chunk, strings.Repeat(" ", pad)))
			remaining = strings.TrimLeft(remaining[cut:], " ")
		}
	}
	sb.WriteString("└" + strings.Repeat("─", innerW) + "┘")
	return sb.String()
}

func findWrap(s string, maxW int) int {
	w := 0
	lastSpace := 0
	for i, r := range s {
		dw := runeWidth(r)
		if w+dw > maxW {
			if lastSpace > 0 {
				return lastSpace
			}
			return i
		}
		if r == ' ' {
			lastSpace = i
		}
		w += dw
	}
	return len(s)
}

func ProgressBar(current, total, width int) string {
	if total <= 0 {
		return ""
	}
	filled := width * current / total
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("[%s] %3d%%", bar, 100*current/total)
}

func Header(text string) string {
	cols, _ := DetectTermSize()
	line := strings.Repeat("─", cols)
	return DimText(line) + "\n" + BoldText(text) + "\n" + DimText(line)
}

func Table(headers []string, rows [][]string) string {
	if len(headers) == 0 || len(rows) == 0 {
		return ""
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = DisplayWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			if w := DisplayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	var sb strings.Builder
	for i, h := range headers {
		sb.WriteString(BoldText(padR(h, widths[i]+2)))
	}
	sb.WriteByte('\n')
	for _, w := range widths {
		sb.WriteString(strings.Repeat("─", w+2))
	}
	sb.WriteByte('\n')
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			sb.WriteString(padR(cell, widths[i]+2))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func padR(s string, w int) string { return s + strings.Repeat(" ", w-DisplayWidth(s)) }

func TruncateWithEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "…"
}

func FirstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func CountLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
