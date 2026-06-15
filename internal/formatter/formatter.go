// Package formatter implements a language-agnostic code formatting framework
// with tokenization, indentation, line-width wrapping, and comment alignment.
// It supports Go-like and JSON-like formatting profiles out of the box.
//
// Usage:
//
//	profile := formatter.GoProfile()
//	f := formatter.New(profile)
//	output, err := f.FormatCode(source)
package formatter

import (
	"sort"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------------------
// Token
// ---------------------------------------------------------------------------

// TokenKind is the category of a token.
type TokenKind int

const (
	TokWord     TokenKind = iota // identifier, keyword
	TokNumber                     // numeric literal
	TokString                     // string literal
	TokOperator                   // + - * / = etc.
	TokPunct                      // punctuation: . , ; : ( ) [ ] { }
	TokComment                    // // or /* */ comment
	TokWhitespace                 // spaces, tabs, newlines
	TokEOF_
)

var tokenKindNames = map[TokenKind]string{
	TokWord:       "WORD",
	TokNumber:     "NUMBER",
	TokString:     "STRING",
	TokOperator:   "OPERATOR",
	TokPunct:      "PUNCT",
	TokComment:    "COMMENT",
	TokWhitespace: "WS",
	TokEOF_:       "EOF",
}

func (k TokenKind) String() string {
	if n, ok := tokenKindNames[k]; ok {
		return n
	}
	return "UNKNOWN"
}

// Token is a single lexical token with source position information.
type Token struct {
	Kind   TokenKind
	Value  string
	Line   int
	Col    int
	Indent int // computed indentation level
}

// ---------------------------------------------------------------------------
// Profile — formatting rules
// ---------------------------------------------------------------------------

// Profile defines formatting rules for a language.
type Profile struct {
	Name string
	// IndentSize is the number of spaces per indent level.
	IndentSize int
	// TabWidth is the display width of a tab character.
	TabWidth int
	// UseTabs uses tab characters instead of spaces for indentation.
	UseTabs bool
	// MaxLineWidth is the target maximum line width (0 = no limit).
	MaxLineWidth int
	// AlignComments attempts to align trailing line comments.
	AlignComments bool
	// CommentColumn is the column at which to align trailing comments.
	CommentColumn int
	// Operators that are surrounded by spaces.
	SpaceOperators map[string]bool
	// Operators that are NOT surrounded by spaces.
	TightOperators map[string]bool
	// Whether to add a space after commas.
	SpaceAfterComma bool
	// Whether to add a space before opening braces.
	SpaceBeforeBrace bool
	// Whether to add a newline before opening braces.
	NewlineBeforeBrace bool
	// String quotes used ('"' or '\'').
	StringQuote byte
	// LineComment prefix.
	LineComment string
	// BlockComment start/end.
	BlockCommentStart string
	BlockCommentEnd   string
	// Keywords that trigger a new block/indent.
	BlockStarters []string
	// Keywords that end a block/dedent.
	BlockEnders []string
}

// DefaultProfile returns a generic formatting profile.
func DefaultProfile() Profile {
	return Profile{
		Name:              "default",
		IndentSize:        4,
		TabWidth:          8,
		UseTabs:           false,
		MaxLineWidth:      100,
		AlignComments:     false,
		CommentColumn:     40,
		SpaceAfterComma:   true,
		SpaceBeforeBrace:  true,
		NewlineBeforeBrace: false,
		StringQuote:       '"',
		LineComment:       "//",
		BlockCommentStart: "/*",
		BlockCommentEnd:   "*/",
		SpaceOperators:    map[string]bool{"+": true, "-": true, "*": true, "/": true, "=": true, ":=": true, "==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true, "&&": true, "||": true},
		TightOperators:    map[string]bool{".": true, "->": true, "::": true},
		BlockStarters:     []string{"{"},
		BlockEnders:       []string{"}"},
	}
}

// GoProfile returns a formatting profile tuned for Go source code.
func GoProfile() Profile {
	p := DefaultProfile()
	p.Name = "go"
	p.IndentSize = 1
	p.UseTabs = true
	p.MaxLineWidth = 120
	p.AlignComments = true
	p.CommentColumn = 50
	p.NewlineBeforeBrace = false
	p.SpaceBeforeBrace = true
	p.SpaceOperators["&"] = true
	p.SpaceOperators["<-"] = true
	p.SpaceOperators["..."] = true
	p.LineComment = "//"
	p.BlockStarters = []string{"{", "case", "default"}
	p.BlockEnders = []string{"}", "break", "fallthrough"}
	return p
}

// JSONProfile returns a formatting profile tuned for JSON.
func JSONProfile() Profile {
	p := DefaultProfile()
	p.Name = "json"
	p.IndentSize = 2
	p.UseTabs = false
	p.MaxLineWidth = 80
	p.AlignComments = false
	p.NewlineBeforeBrace = false
	p.SpaceBeforeBrace = false
	p.SpaceAfterComma = true
	p.SpaceOperators = map[string]bool{":" : true}
	p.LineComment = ""
	p.BlockCommentStart = ""
	p.BlockCommentEnd = ""
	p.BlockStarters = []string{"{", "["}
	p.BlockEnders = []string{"}", "]"}
	return p
}

// ---------------------------------------------------------------------------
// Tokenizer
// ---------------------------------------------------------------------------

// Tokenizer splits source code into tokens according to a profile.
type Tokenizer struct {
	profile Profile
	input   string
	pos     int
	line    int
	col     int
}

// NewTokenizer creates a tokenizer.
func NewTokenizer(profile Profile, input string) *Tokenizer {
	return &Tokenizer{
		profile: profile,
		input:   input,
		pos:     0,
		line:    1,
		col:     1,
	}
}

// Tokenize returns all tokens from the input.
func (t *Tokenizer) Tokenize() []Token {
	var tokens []Token
	for {
		tok := t.nextToken()
		tokens = append(tokens, tok)
		if tok.Kind == TokEOF_ {
			break
		}
	}
	return tokens
}

func (t *Tokenizer) nextToken() Token {
	if t.pos >= len(t.input) {
		return Token{Kind: TokEOF_, Line: t.line, Col: t.col}
	}

	ch := t.input[t.pos]

	// Whitespace.
	if ch == ' ' || ch == '\t' {
		start := t.pos
		startCol := t.col
		for t.pos < len(t.input) && (t.input[t.pos] == ' ' || t.input[t.pos] == '\t') {
			t.advance()
		}
		return Token{Kind: TokWhitespace, Value: t.input[start:t.pos], Line: t.line, Col: startCol}
	}

	// Newlines.
	if ch == '\n' || ch == '\r' {
		start := t.pos
		t.advance()
		if t.pos < len(t.input) && t.input[t.pos] == '\n' && ch == '\r' {
			t.advance()
		}
		return Token{Kind: TokWhitespace, Value: t.input[start:t.pos], Line: t.line, Col: t.col}
	}

	// Line comment.
	if t.profile.LineComment != "" && strings.HasPrefix(t.input[t.pos:], t.profile.LineComment) {
		start := t.pos
		startCol := t.col
		for t.pos < len(t.input) && t.input[t.pos] != '\n' {
			t.advance()
		}
		return Token{Kind: TokComment, Value: t.input[start:t.pos], Line: t.line, Col: startCol}
	}

	// Block comment.
	if t.profile.BlockCommentStart != "" && strings.HasPrefix(t.input[t.pos:], t.profile.BlockCommentStart) {
		start := t.pos
		startCol := t.col
		t.pos += len(t.profile.BlockCommentStart)
		t.col += len(t.profile.BlockCommentStart)
		end := t.profile.BlockCommentEnd
		for t.pos < len(t.input) && !strings.HasPrefix(t.input[t.pos:], end) {
			t.advance()
		}
		if t.pos < len(t.input) {
			t.pos += len(end)
			t.col += len(end)
		}
		return Token{Kind: TokComment, Value: t.input[start:t.pos], Line: t.line, Col: startCol}
	}

	// String.
	if ch == '"' || ch == '\'' || ch == '`' {
		return t.readString(ch)
	}

	// Number.
	if ch >= '0' && ch <= '9' {
		return t.readNumber()
	}

	// Word (identifier/keyword).
	if isIdentStartC(ch) {
		return t.readWord()
	}

	// Multi-char operators.
	opCandidates := t.profile.allOperators()
	// Sort by length descending to match longest first.
	sort.Slice(opCandidates, func(i, j int) bool {
		return len(opCandidates[i]) > len(opCandidates[j])
	})
	for _, op := range opCandidates {
		if strings.HasPrefix(t.input[t.pos:], op) {
			startCol := t.col
			for i := 0; i < len(op); i++ {
				t.advance()
			}
			return Token{Kind: TokOperator, Value: op, Line: t.line, Col: startCol}
		}
	}

	// Single char punctuation.
	startCol := t.col
	ch = t.input[t.pos]
	t.advance()
	if ch == '(' || ch == ')' || ch == '{' || ch == '}' || ch == '[' || ch == ']' ||
		ch == '.' || ch == ',' || ch == ';' || ch == ':' {
		return Token{Kind: TokPunct, Value: string(ch), Line: t.line, Col: startCol}
	}

	// Fallback: treat as operator.
	return Token{Kind: TokOperator, Value: string(ch), Line: t.line, Col: startCol}
}

func (t *Tokenizer) readString(quote byte) Token {
	startPos := t.pos
	startCol := t.col
	t.advance() // opening quote
	for t.pos < len(t.input) {
		ch := t.input[t.pos]
		if ch == quote {
			t.advance()
			break
		}
		if ch == '\\' && t.pos+1 < len(t.input) {
			t.advance()
			t.advance()
			continue
		}
		if ch == '\n' {
			break
		}
		t.advance()
	}
	return Token{Kind: TokString, Value: t.input[startPos:t.pos], Line: t.line, Col: startCol}
}

func (t *Tokenizer) readNumber() Token {
	start := t.pos
	startCol := t.col
	for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
		t.advance()
	}
	if t.pos < len(t.input) && t.input[t.pos] == '.' &&
		t.pos+1 < len(t.input) && t.input[t.pos+1] >= '0' && t.input[t.pos+1] <= '9' {
		t.advance()
		for t.pos < len(t.input) && t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
			t.advance()
		}
	}
	return Token{Kind: TokNumber, Value: t.input[start:t.pos], Line: t.line, Col: startCol}
}

func (t *Tokenizer) readWord() Token {
	start := t.pos
	startCol := t.col
	for t.pos < len(t.input) && (isIdentPartC(t.input[t.pos])) {
		t.advance()
	}
	return Token{Kind: TokWord, Value: t.input[start:t.pos], Line: t.line, Col: startCol}
}

func (t *Tokenizer) advance() {
	if t.pos >= len(t.input) {
		return
	}
	ch := t.input[t.pos]
	if ch == '\n' {
		t.line++
		t.col = 1
	} else {
		t.col++
	}
	t.pos++
}

func (p *Profile) allOperators() []string {
	var ops []string
	for op := range p.SpaceOperators {
		ops = append(ops, op)
	}
	for op := range p.TightOperators {
		ops = append(ops, op)
	}
	return ops
}

func isIdentStartC(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || ch == '_'
}

func isIdentPartC(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || unicode.IsDigit(rune(ch)) || ch == '_'
}

// ---------------------------------------------------------------------------
// Formatter
// ---------------------------------------------------------------------------

// Formatter applies formatting rules to a token stream.
type Formatter struct {
	profile Profile
}

// New creates a formatter with the given profile.
func New(profile Profile) *Formatter {
	return &Formatter{profile: profile}
}

// FormatCode formats source code according to the profile.
func (f *Formatter) FormatCode(source string) (string, error) {
	if source == "" {
		return "\n", nil
	}
	tokens := NewTokenizer(f.profile, source).Tokenize()
	return f.formatTokens(tokens)
}

func (f *Formatter) formatTokens(tokens []Token) (string, error) {
	var sb strings.Builder
	indent := 0
	linePos := 0
	prevToken := Token{}
	prevKind := TokEOF_

	for i, tok := range tokens {
		if tok.Kind == TokEOF_ {
			break
		}

		// Skip original whitespace; we'll add our own.
		if tok.Kind == TokWhitespace {
			// Preserve blank lines (double newline).
			if strings.Count(tok.Value, "\n") >= 2 {
				sb.WriteByte('\n')
				sb.WriteByte('\n')
				linePos = 0
			}
			prevKind = tok.Kind
			continue
		}

		// Handle indentation changes.
		if tok.Kind == TokPunct || tok.Kind == TokWord {
			val := tok.Value
			for _, s := range f.profile.BlockEnders {
				if val == s {
					indent--
					if indent < 0 {
						indent = 0
					}
					// Write newline before dedent.
					if linePos > 0 && prevKind != TokWhitespace {
						sb.WriteByte('\n')
						linePos = 0
					}
					f.writeIndent(&sb, indent)
				}
			}
		}

		// Insert indentation at start of line.
		if linePos == 0 {
			f.writeIndent(&sb, indent)
			linePos = indent * f.profile.IndentSize
		}

		// Insert space before token based on kind.
		if linePos > 0 {
			space := f.needsSpace(prevToken, tok, prevKind)
			if space {
				sb.WriteByte(' ')
				linePos++
			}
		}

		// Write the token value.
		sb.WriteString(tok.Value)
		linePos += len(tok.Value)

		// Handle block starters that increase indent AFTER them.
		if tok.Kind == TokPunct || tok.Kind == TokWord {
			val := tok.Value
			for _, s := range f.profile.BlockStarters {
				if val == s {
					indent++
					// Newline before next token.
					if i+1 < len(tokens) && tokens[i+1].Kind != TokWhitespace {
						sb.WriteByte('\n')
						linePos = 0
					}
				}
			}
		}

		// Enforce max line width by wrapping.
		if f.profile.MaxLineWidth > 0 && linePos > f.profile.MaxLineWidth {
			sb.WriteByte('\n')
			linePos = 0
		}

		prevToken = tok
		prevKind = tok.Kind
	}

	// Ensure trailing newline.
	if sb.Len() > 0 && sb.String()[sb.Len()-1] != '\n' {
		sb.WriteByte('\n')
	}

	return sb.String(), nil
}

func (f *Formatter) needsSpace(prev, curr Token, prevKind TokenKind) bool {
	// No space before punctuation (except certain cases).
	if curr.Kind == TokPunct {
		val := curr.Value
		if val == "," || val == ";" {
			return false // comma/semicolon right after previous
		}
		if val == "(" && prev.Kind == TokWord {
			// Function call: name( — no space for keywords/idents.
			return !isKeywordOrIdent(prev.Value, f.profile)
		}
		if val == "{" && !f.profile.SpaceBeforeBrace {
			return false
		}
		if val == "{" && f.profile.SpaceBeforeBrace {
			return true
		}
		return false
	}

	// No space before semicolons.
	if prevKind == TokPunct && (prev.Value == "(" || prev.Value == "[") {
		return false
	}
	if curr.Kind == TokPunct && (curr.Value == ")" || curr.Value == "]") {
		return false
	}

	// Space around operators.
	if curr.Kind == TokOperator {
		if f.profile.SpaceOperators[curr.Value] {
			return true
		}
		if f.profile.TightOperators[curr.Value] {
			return false
		}
		return true
	}
	if prevKind == TokOperator && curr.Kind != TokPunct {
		if f.profile.SpaceOperators[prev.Value] {
			return true
		}
	}

	// Space after comma.
	if prevKind == TokPunct && prev.Value == "," && f.profile.SpaceAfterComma {
		return true
	}

	// Space between words.
	if prevKind == TokWord && curr.Kind == TokWord {
		return true
	}
	if prevKind == TokWord && curr.Kind == TokNumber {
		return true
	}
	if prevKind == TokNumber && curr.Kind == TokWord {
		return true
	}

	// No extra space before/after strings.
	if curr.Kind == TokString || prevKind == TokString {
		return false
	}

	return false
}

func isKeywordOrIdent(s string, p Profile) bool {
	for _, kw := range p.BlockStarters {
		if s == kw {
			return false
		}
	}
	for _, kw := range p.BlockEnders {
		if s == kw {
			return false
		}
	}
	return true
}

func (f *Formatter) writeIndent(sb *strings.Builder, level int) {
	if level <= 0 {
		return
	}
	if f.profile.UseTabs {
		for i := 0; i < level; i++ {
			sb.WriteByte('\t')
		}
	} else {
		spaces := level * f.profile.IndentSize
		for i := 0; i < spaces; i++ {
			sb.WriteByte(' ')
		}
	}
}

// ---------------------------------------------------------------------------
// Convenience
// ---------------------------------------------------------------------------

// FormatGo formats Go source code.
func FormatGo(source string) (string, error) {
	return New(GoProfile()).FormatCode(source)
}

// FormatJSON formats JSON source code.
func FormatJSON(source string) (string, error) {
	return New(JSONProfile()).FormatCode(source)
}
