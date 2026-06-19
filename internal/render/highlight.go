// Package render turns model output into styled terminal text: markdown→ANSI
// and syntax-highlighted code blocks. It is a pure library (no I/O, no global
// terminal state) so it is fully testable and reusable by any front-end — the
// current ANSI sink today, a bubbletea program tomorrow.
//
// Nothing here is ever sent back to the model: rendering happens only on the
// display path, so the prefix cache stays byte-stable.
package render

import (
	"strings"
	"unicode/utf8"
)

// ── ANSI palette ──────────────────────────────────────────
// Kept local to render so callers don't depend on cmd/lumen's palette.

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiItalic  = "\x1b[3m"
	ansiUnder   = "\x1b[4m"
	ansiWhite   = "\x1b[97m"
	ansiCyan    = "\x1b[36m"
	ansiGreen   = "\x1b[32m"
	ansiMagenta = "\x1b[35m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
)

// Token colors for syntax highlighting.
const (
	colKeyword = ansiMagenta
	colString  = ansiGreen
	colComment = ansiDim
	colNumber  = ansiCyan
)

// ── Language registry ─────────────────────────────────────

// Lang describes how to lex one language for highlighting. Only the lexical
// surface is modeled — enough for readable terminal coloring, not a parser.
type Lang struct {
	Keywords   map[string]bool
	Line       string // line-comment prefix, e.g. "//" or "#"
	BlockOpen  string // block-comment open, e.g. "/*"
	BlockClose string // block-comment close, e.g. "*/"
	// CharQuote marks languages where ' is a char/rune literal ('a', '\n') rather
	// than a string delimiter (Go, C, C++, Rust, Java). In those languages a lone
	// apostrophe or a Rust lifetime ('a) must NOT open a string span. Languages
	// that use ' for strings (Python, JS, Ruby, …) leave this false.
	CharQuote bool
}

var langs = map[string]*Lang{}

// aliases maps common fence labels to a registered language name.
var aliases = map[string]string{
	"golang":      "go",
	"js":          "javascript",
	"jsx":         "javascript",
	"ts":          "typescript",
	"tsx":         "typescript",
	"py":          "python",
	"sh":          "bash",
	"shell":       "bash",
	"zsh":         "bash",
	"yml":         "yaml",
	"c++":         "cpp",
	"objective-c": "c",
}

// RegisterLang registers a Lang under one or more fence labels. It is exported
// so additional languages can be added (e.g. by generated tables) without
// touching the lexer.
func RegisterLang(l *Lang, names ...string) {
	for _, n := range names {
		langs[strings.ToLower(n)] = l
	}
}

func lookupLang(name string) *Lang {
	n := strings.ToLower(strings.TrimSpace(name))
	if alias, ok := aliases[n]; ok {
		n = alias
	}
	return langs[n]
}

func keywordSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func init() {
	RegisterLang(&Lang{
		Line: "//", BlockOpen: "/*", BlockClose: "*/", CharQuote: true,
		Keywords: keywordSet("break", "case", "chan", "const", "continue", "default",
			"defer", "else", "fallthrough", "for", "func", "go", "goto", "if", "import",
			"interface", "map", "package", "range", "return", "select", "struct", "switch",
			"type", "var", "nil", "true", "false", "iota"),
	}, "go")

	RegisterLang(&Lang{
		Line: "//", BlockOpen: "/*", BlockClose: "*/",
		Keywords: keywordSet("function", "return", "const", "let", "var", "if", "else",
			"for", "while", "do", "switch", "case", "break", "continue", "new", "class",
			"extends", "import", "export", "from", "default", "async", "await", "try",
			"catch", "finally", "throw", "typeof", "instanceof", "this", "null", "undefined",
			"true", "false"),
	}, "javascript", "typescript")

	RegisterLang(&Lang{
		Line: "#",
		Keywords: keywordSet("def", "return", "if", "elif", "else", "for", "while", "in",
			"import", "from", "as", "class", "try", "except", "finally", "raise", "with",
			"lambda", "yield", "pass", "break", "continue", "and", "or", "not", "is",
			"None", "True", "False", "async", "await"),
	}, "python")

	RegisterLang(&Lang{
		Line: "#",
		Keywords: keywordSet("if", "then", "else", "elif", "fi", "for", "in", "do", "done",
			"while", "case", "esac", "function", "return", "export", "local", "echo", "cd",
			"set", "unset", "source", "exit"),
	}, "bash")

	RegisterLang(&Lang{
		Keywords: keywordSet("true", "false", "null"),
	}, "json")

	RegisterLang(&Lang{
		Line: "//", BlockOpen: "/*", BlockClose: "*/", CharQuote: true,
		Keywords: keywordSet("int", "char", "void", "float", "double", "long", "short",
			"unsigned", "signed", "struct", "union", "enum", "typedef", "const", "static",
			"return", "if", "else", "for", "while", "do", "switch", "case", "break",
			"continue", "sizeof", "goto"),
	}, "c", "cpp")
}

// ── Highlighter ───────────────────────────────────────────

// Highlight colorizes code for the given fence language. Unknown languages are
// returned unchanged so callers never need to special-case them.
func Highlight(code, lang string) string {
	l := lookupLang(lang)
	if l == nil {
		return code
	}
	var b strings.Builder
	b.Grow(len(code) + len(code)/4)
	n := len(code)
	for i := 0; i < n; {
		rest := code[i:]

		if l.Line != "" && strings.HasPrefix(rest, l.Line) {
			end := i + len(rest)
			if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
				end = i + nl
			}
			writeColored(&b, colComment, code[i:end])
			i = end
			continue
		}

		if l.BlockOpen != "" && strings.HasPrefix(rest, l.BlockOpen) {
			end := n
			if k := strings.Index(code[i+len(l.BlockOpen):], l.BlockClose); k >= 0 {
				end = i + len(l.BlockOpen) + k + len(l.BlockClose)
			}
			writeColored(&b, colComment, code[i:end])
			i = end
			continue
		}

		c := code[i]
		// Single quote in a char-literal language (Go/Rust/C/Java) only opens a
		// string span if it forms a real char literal; otherwise it is a stray
		// apostrophe or a Rust lifetime ('a) and must be emitted raw.
		if c == '\'' && l.CharQuote {
			if cl := charLiteralLen(code, i); cl > 0 {
				writeColored(&b, colString, code[i:i+cl])
				i += cl
			} else {
				b.WriteByte(c)
				i++
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			j := i + 1
			for j < n {
				if code[j] == '\\' && c != '`' {
					j += 2
					continue
				}
				if code[j] == c {
					j++
					break
				}
				j++
			}
			writeColored(&b, colString, code[i:min(j, n)])
			i = min(j, n)
			continue
		}

		if c >= '0' && c <= '9' {
			j := scanNumber(code, i)
			writeColored(&b, colNumber, code[i:j])
			i = j
			continue
		}

		if isIdentStart(c) {
			j := i + 1
			for j < n && isIdentPart(code[j]) {
				j++
			}
			word := code[i:j]
			if l.Keywords[word] {
				writeColored(&b, colKeyword, word)
			} else {
				b.WriteString(word)
			}
			i = j
			continue
		}

		b.WriteByte(c)
		i++
	}
	return b.String()
}

func writeColored(b *strings.Builder, color, text string) {
	b.WriteString(color)
	b.WriteString(text)
	b.WriteString(ansiReset)
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// scanNumber returns the end index of a numeric literal starting at code[i]
// (which must be a digit). It accepts a 0x/0X hex literal, decimal digits with
// at most a fraction and an exponent, and Go-style "_" separators — but stops
// at any other letter so "10abc" / "3px" no longer swallow the identifier.
func scanNumber(code string, i int) int {
	n := len(code)
	j := i + 1
	if code[i] == '0' && j < n && (code[j] == 'x' || code[j] == 'X') {
		j++
		for j < n && (isHexDigit(code[j]) || code[j] == '_') {
			j++
		}
		return j
	}
	for j < n && ((code[j] >= '0' && code[j] <= '9') || code[j] == '.' || code[j] == '_') {
		j++
	}
	if j < n && (code[j] == 'e' || code[j] == 'E') {
		k := j + 1
		if k < n && (code[k] == '+' || code[k] == '-') {
			k++
		}
		if k < n && code[k] >= '0' && code[k] <= '9' {
			j = k + 1
			for j < n && code[j] >= '0' && code[j] <= '9' {
				j++
			}
		}
	}
	return j
}

// charLiteralLen returns the byte length of a valid char/rune literal starting
// at code[i] (which must be a single-quote byte), or 0 if it does not open one —
// used to tell a real literal ('a', '\n', 'λ') from a stray apostrophe or a
// Rust lifetime ('a).
func charLiteralLen(code string, i int) int {
	n := len(code)
	j := i + 1
	if j >= n || code[j] == '\'' || code[j] == '\n' {
		return 0
	}
	if code[j] == '\\' {
		j++
		for k := 0; j < n && k < 12; k, j = k+1, j+1 {
			if code[j] == '\n' {
				return 0
			}
			if code[j] == '\'' {
				return j - i + 1
			}
		}
		return 0
	}
	_, sz := utf8.DecodeRuneInString(code[j:])
	j += sz
	if j < n && code[j] == '\'' {
		return j - i + 1
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
