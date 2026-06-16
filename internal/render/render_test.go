package render

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func hasANSI(s string) bool { return ansiRE.MatchString(s) }

func TestMarkdownPlainPassesThrough(t *testing.T) {
	in := "just a normal sentence."
	got := Markdown(in)
	if plain(got) != in {
		t.Fatalf("plain text changed: %q -> %q", in, plain(got))
	}
}

func TestMarkdownBold(t *testing.T) {
	got := Markdown("this is **bold** text")
	if want := "this is bold text"; plain(got) != want {
		t.Fatalf("plain = %q, want %q", plain(got), want)
	}
	if !hasANSI(got) {
		t.Fatal("expected ANSI styling for bold")
	}
	if strings.Contains(got, "**") {
		t.Fatal("bold markers ** should be removed")
	}
}

func TestMarkdownInlineCode(t *testing.T) {
	got := Markdown("run `go test` now")
	if want := "run go test now"; plain(got) != want {
		t.Fatalf("plain = %q, want %q", plain(got), want)
	}
	if strings.Contains(got, "`") {
		t.Fatal("backticks should be removed")
	}
	if !hasANSI(got) {
		t.Fatal("expected ANSI styling for inline code")
	}
}

func TestMarkdownHeading(t *testing.T) {
	got := Markdown("## Section Title")
	p := plain(got)
	if !strings.Contains(p, "Section Title") {
		t.Fatalf("heading text missing: %q", p)
	}
	if strings.Contains(p, "#") {
		t.Fatal("heading hashes should be removed from visible text")
	}
	if !hasANSI(got) {
		t.Fatal("expected ANSI styling for heading")
	}
}

func TestMarkdownBullet(t *testing.T) {
	got := Markdown("- first\n- second")
	p := plain(got)
	if !strings.Contains(p, "• first") || !strings.Contains(p, "• second") {
		t.Fatalf("bullets not rendered: %q", p)
	}
}

func TestMarkdownFencedCodeIsHighlighted(t *testing.T) {
	md := "before\n```go\nfunc main() {}\n```\nafter"
	got := Markdown(md)
	p := plain(got)
	if strings.Contains(p, "```") {
		t.Fatal("code fences should be removed")
	}
	if !strings.Contains(p, "func main() {}") {
		t.Fatalf("code content missing: %q", p)
	}
	if !strings.Contains(p, "before") || !strings.Contains(p, "after") {
		t.Fatal("surrounding text lost")
	}
	if !hasANSI(got) {
		t.Fatal("expected syntax highlighting inside code fence")
	}
}

func TestHighlightGoKeyword(t *testing.T) {
	got := Highlight("func x() {}", "go")
	if plain(got) != "func x() {}" {
		t.Fatalf("highlight altered code text: %q", plain(got))
	}
	if !hasANSI(got) {
		t.Fatal("expected keyword coloring")
	}
}

func TestHighlightGoString(t *testing.T) {
	in := `s := "hi"`
	got := Highlight(in, "go")
	if plain(got) != in {
		t.Fatalf("highlight altered code text: %q", plain(got))
	}
	if !hasANSI(got) {
		t.Fatal("expected string coloring")
	}
}

func TestHighlightUnknownLangPassesThrough(t *testing.T) {
	in := "lorem ipsum dolor"
	got := Highlight(in, "no-such-lang")
	if got != in {
		t.Fatalf("unknown lang should pass through unchanged: %q", got)
	}
}

func TestHighlightAliasGolang(t *testing.T) {
	got := Highlight("package main", "golang")
	if !hasANSI(got) {
		t.Fatal("golang alias should resolve to go and highlight 'package'")
	}
}
