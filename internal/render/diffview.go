package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const ansiRed = "\x1b[31m"

// langByExt maps a file extension to a fence language for highlighting. Some
// targets (rust, ruby, sql, yaml…) may not be registered in the highlighter yet
// — Highlight returns such content unchanged, so this degrades gracefully.
var langByExt = map[string]string{
	".go": "go", ".py": "python",
	".js": "javascript", ".jsx": "javascript", ".mjs": "javascript",
	".ts": "typescript", ".tsx": "typescript",
	".sh": "bash", ".bash": "bash", ".zsh": "bash",
	".json": "json", ".c": "c", ".h": "c", ".cpp": "cpp", ".cc": "cpp", ".hpp": "cpp",
	".rs": "rust", ".rb": "ruby", ".java": "java", ".sql": "sql",
	".yaml": "yaml", ".yml": "yaml", ".toml": "toml",
}

// LangForPath returns the highlight language for a file path, or "" if unknown.
func LangForPath(path string) string {
	return langByExt[strings.ToLower(filepath.Ext(path))]
}

// DiffLine renders a single diff line: a colored marker ('+' green, '-' red,
// ' ' dim) followed by the (possibly already-highlighted) content.
func DiffLine(kind byte, content string) string {
	var col string
	switch kind {
	case '+':
		col = ansiGreen
	case '-':
		col = ansiRed
	default:
		col = ansiDim
	}
	return col + string(kind) + ansiReset + " " + content
}

type diffLine struct {
	kind byte
	text string
}

// Diff renders a unified-style, syntax-highlighted diff between before and
// after for the given path. An empty before means a new file (all additions);
// an empty after means a deleted file (all deletions).
func Diff(path, before, after string) string {
	lang := LangForPath(path)

	var lines []diffLine
	switch {
	case before == "" && after != "":
		for _, l := range splitLines(after) {
			lines = append(lines, diffLine{'+', l})
		}
	case after == "" && before != "":
		for _, l := range splitLines(before) {
			lines = append(lines, diffLine{'-', l})
		}
	default:
		lines = lcsDiff(splitLines(before), splitLines(after))
	}

	const maxLines = 200
	var b strings.Builder
	for i, dl := range lines {
		if i >= maxLines {
			fmt.Fprintf(&b, "%s… (%d more lines)%s\n", ansiDim, len(lines)-maxLines, ansiReset)
			break
		}
		b.WriteString(DiffLine(dl.kind, Highlight(dl.text, lang)))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Strip a trailing CR so CRLF (\r\n) files don't render a literal ^M at the
	// end of every diff line.
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

// lcsDiff produces an ordered line diff (context/deletion/addition) using a
// longest-common-subsequence DP.
// lcsBudget caps the LCS DP matrix (n*m cells). Above it, lcsDiff falls back to
// a cheap prefix/suffix-trimmed diff: the O(n*m) matrix would blow up memory on
// large files (a 50k-line edit is ~20 GB), and the render is capped at maxLines
// anyway, so a precise LCS over a huge file is both dangerous and wasted.
const lcsBudget = 1_000_000

func lcsDiff(a, b []string) []diffLine {
	n, m := len(a), len(b)
	if int64(n)*int64(m) > lcsBudget {
		return cheapDiff(a, b)
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []diffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, diffLine{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, diffLine{'-', a[i]})
			i++
		default:
			out = append(out, diffLine{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, diffLine{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, diffLine{'+', b[j]})
	}
	return out
}

// cheapDiff is the O(n+m) fallback for large inputs: trim the common prefix and
// suffix, then emit the differing middle as deletions followed by additions. It
// drops the (equal) common context so the render cap shows the ACTUAL change
// rather than truncating in a sea of unchanged leading lines.
func cheapDiff(a, b []string) []diffLine {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	var out []diffLine
	for _, l := range a[p : len(a)-s] {
		out = append(out, diffLine{'-', l})
	}
	for _, l := range b[p : len(b)-s] {
		out = append(out, diffLine{'+', l})
	}
	return out
}

// TruncateVisible truncates s to at most max visible columns and appends an
// ellipsis if it cut anything. Width is the terminal DISPLAY width — CJK/wide
// runes count as 2 columns, ANSI escape sequences as 0 — so it never overflows
// or splits a multibyte rune.
func TruncateVisible(s string, max int) string {
	if ansi.StringWidth(s) <= max {
		return s
	}
	return ansi.Truncate(s, max, ansiReset+"…")
}
