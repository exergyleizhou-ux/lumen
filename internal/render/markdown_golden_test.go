package render

import (
	"strings"
	"testing"
)

func TestMarkdownOrderedList(t *testing.T) {
	in := "1. first\n2. second\n3. third"
	out := Markdown(in)
	plain := plain(out)
	if !strings.Contains(plain, "first") || !strings.Contains(plain, "second") {
		t.Errorf("ordered list items missing from output: %q", plain)
	}
}

func TestMarkdownNestedInline(t *testing.T) {
	in := "**bold with `code` inside**"
	out := Markdown(in)
	if out == "" {
		t.Fatal("empty output for nested inline")
	}
	plain := plain(out)
	if !strings.Contains(plain, "code") || !strings.Contains(plain, "inside") {
		t.Errorf("nested inline content lost: %q", plain)
	}
}

func TestMarkdownTable(t *testing.T) {
	in := "| a | b |\n|---|---|\n| 1 | 2 |\n"
	out := Markdown(in)
	plain := plain(out)
	if !strings.Contains(plain, "a") || !strings.Contains(plain, "b") {
		t.Errorf("table headers lost: %q", plain)
	}
	if !strings.Contains(plain, "1") || !strings.Contains(plain, "2") {
		t.Errorf("table cells lost: %q", plain)
	}
	// Tables pass through — the renderer does not strip table syntax yet.
	// This test just confirms no panic and content is preserved.
	t.Logf("table output: %q (plain: %q)", out, plain)
}

func TestMarkdownMultipleCodeBlocks(t *testing.T) {
	in := "```go\nfunc a() {}\n```\n\ntext between\n\n```go\nfunc b() {}\n```"
	out := Markdown(in)
	plain := plain(out)
	if !strings.Contains(plain, "func a()") {
		t.Errorf("first code block lost: %q", plain)
	}
	if !strings.Contains(plain, "func b()") {
		t.Errorf("second code block lost: %q", plain)
	}
	if !strings.Contains(plain, "text between") {
		t.Errorf("text between code blocks lost: %q", plain)
	}
}

func TestMarkdownEmptyInputs(t *testing.T) {
	if out := Markdown(""); out != "" {
		t.Errorf("empty string should produce empty output, got %q", out)
	}
	Markdown("\n\n\n") // must not panic
	Markdown("#")      // must not panic
}

func TestHighlightNewLanguages(t *testing.T) {
	tests := []struct {
		lang, snippet string
	}{
		{"rust", "fn main() { let x = 1; }"},
		{"java", "public class Test { private int x; }"},
		{"ruby", "def hello\n  puts 'hi'\nend"},
		{"sql", "SELECT * FROM users WHERE id = 1"},
		{"yaml", "key: true\nother: false"},
		{"toml", "[section]\nkey = true"},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			out := Highlight(tt.snippet, tt.lang)
			if out == "" {
				t.Errorf("Highlight(%q, %q) returned empty", tt.snippet, tt.lang)
			}
			plain := plain(out)
			if len(plain) < 3 {
				t.Errorf("output too short for %s: %q", tt.lang, plain)
			}
		})
	}
}
