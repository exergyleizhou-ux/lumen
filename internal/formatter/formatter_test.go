package formatter

import (
	"strings"
	"testing"
)

func TestGoProfile(t *testing.T) {
	p := GoProfile()
	if p.Name != "go" {
		t.Fatal("bad name")
	}
	if !p.UseTabs {
		t.Fatal("go profile should use tabs")
	}
}

func TestJSONProfile(t *testing.T) {
	p := JSONProfile()
	if p.Name != "json" {
		t.Fatal("bad name")
	}
	if p.UseTabs {
		t.Fatal("json profile should use spaces")
	}
	if p.SpaceBeforeBrace {
		t.Fatal("json should not space before brace")
	}
}

func TestTokenizer_Simple(t *testing.T) {
	tok := NewTokenizer(GoProfile(), "func main() {")
	tokens := tok.Tokenize()
	if len(tokens) < 5 {
		t.Fatalf("expected at least 5 tokens, got %d", len(tokens))
	}
}

func TestTokenizer_Strings(t *testing.T) {
	tok := NewTokenizer(GoProfile(), `"hello" 'c'`)
	tokens := tok.Tokenize()
	count := 0
	for _, tk := range tokens {
		if tk.Kind == TokString {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 strings, got %d", count)
	}
}

func TestTokenizer_Comments(t *testing.T) {
	tok := NewTokenizer(GoProfile(), "// comment\ncode")
	tokens := tok.Tokenize()
	hasComment := false
	for _, tk := range tokens {
		if tk.Kind == TokComment && strings.Contains(tk.Value, "comment") {
			hasComment = true
		}
	}
	if !hasComment {
		t.Fatal("expected comment token")
	}
}

func TestFormatGo_Simple(t *testing.T) {
	out, err := FormatGo("func main(){x:=1}")
	if err != nil {
		t.Fatal(err)
	}
	// Should not be empty.
	if len(out) == 0 {
		t.Fatal("empty output")
	}
}

func TestFormatJSON_Simple(t *testing.T) {
	out, err := FormatJSON(`{"a":1,"b":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("empty output")
	}
	// Should contain the keys.
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestFormatCode_Go(t *testing.T) {
	f := New(GoProfile())
	out, err := f.FormatCode("package main\nfunc main(){\nx:=1+2\n}")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "package") {
		t.Fatal("missing package")
	}
}

func TestFormatCode_JSON(t *testing.T) {
	f := New(JSONProfile())
	out, err := f.FormatCode(`{"name":"Alice","age":30}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "name") {
		t.Fatal("missing name")
	}
}

func TestFormatCode_Empty(t *testing.T) {
	f := New(DefaultProfile())
	out, err := f.FormatCode("")
	if err != nil {
		t.Fatal(err)
	}
	if out != "\n" {
		t.Fatalf("expected newline, got %q", out)
	}
}

func TestTokenizer_BlockComment(t *testing.T) {
	tok := NewTokenizer(GoProfile(), "/* block */ code")
	tokens := tok.Tokenize()
	hasBlock := false
	for _, tk := range tokens {
		if tk.Kind == TokComment && strings.Contains(tk.Value, "block") {
			hasBlock = true
		}
	}
	if !hasBlock {
		t.Fatal("expected block comment")
	}
}

func TestTokenizer_Operators(t *testing.T) {
	tok := NewTokenizer(GoProfile(), "+ - * / == != <= >= && ||")
	tokens := tok.Tokenize()
	opCount := 0
	for _, tk := range tokens {
		if tk.Kind == TokOperator {
			opCount++
		}
	}
	if opCount < 4 {
		t.Fatalf("expected operators, got %d", opCount)
	}
}

func TestNeedsSpace(t *testing.T) {
	f := New(GoProfile())
	tests := []struct {
		prev, curr Token
		prevKind   TokenKind
		expect     bool
	}{
		{Token{Kind: TokWord, Value: "if"}, Token{Kind: TokPunct, Value: "("}, TokWord, false},
		{Token{Kind: TokWord, Value: "foo"}, Token{Kind: TokWord, Value: "bar"}, TokWord, true},
		{Token{Kind: TokPunct, Value: ","}, Token{Kind: TokWord, Value: "bar"}, TokPunct, true},
		{Token{Kind: TokWord, Value: "x"}, Token{Kind: TokOperator, Value: "+"}, TokWord, true},
		{Token{Kind: TokOperator, Value: "+"}, Token{Kind: TokNumber, Value: "1"}, TokOperator, true},
		{Token{Kind: TokPunct, Value: "("}, Token{Kind: TokWord, Value: "x"}, TokPunct, false},
		{Token{Kind: TokWord, Value: "x"}, Token{Kind: TokPunct, Value: ")"}, TokWord, false},
	}

	for _, tt := range tests {
		got := f.needsSpace(tt.prev, tt.curr, tt.prevKind)
		if got != tt.expect {
			t.Fatalf("needsSpace(%v, %v, %v) = %v, want %v",
				tt.prev.Value, tt.curr.Value, tt.prevKind, got, tt.expect)
		}
	}
}

func TestTokenKind_String(t *testing.T) {
	if TokWord.String() != "WORD" {
		t.Fatal("bad")
	}
	if TokenKind(999).String() != "UNKNOWN" {
		t.Fatal("bad")
	}
}
