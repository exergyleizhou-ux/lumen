package render

import (
	"strings"
	"testing"
)

func TestHighlightRust(t *testing.T) {
	out := Highlight("fn main() { let x = 1; return x; }", "rust")
	if out == "" { t.Fatal("empty output") }
	if !strings.Contains(plain(out), "fn main()") { t.Errorf("content lost: %q", plain(out)) }
}

func TestHighlightJava(t *testing.T) {
	out := Highlight("public class Test { private int x; }", "java")
	if out == "" { t.Fatal("empty output") }
	if !strings.Contains(plain(out), "class Test") { t.Errorf("content lost: %q", plain(out)) }
}

func TestHighlightRuby(t *testing.T) {
	out := Highlight("def hello\n  return 'hi'\nend", "ruby")
	if out == "" { t.Fatal("empty output") }
	if !strings.Contains(plain(out), "hello") { t.Errorf("content lost: %q", plain(out)) }
}

func TestHighlightSQL(t *testing.T) {
	out := Highlight("SELECT * FROM users WHERE id = 1", "sql")
	if out == "" { t.Fatal("empty output") }
	if !strings.Contains(plain(out), "users") { t.Errorf("content lost: %q", plain(out)) }
}

func TestHighlightYAML(t *testing.T) {
	out := Highlight("key: true\nother: false", "yaml")
	if out == "" { t.Fatal("empty output") }
	if !strings.Contains(plain(out), "key") { t.Errorf("content lost: %q", plain(out)) }
}

func TestHighlightTOML(t *testing.T) {
	out := Highlight("[section]\nkey = true", "toml")
	if out == "" { t.Fatal("empty output") }
	if !strings.Contains(plain(out), "[section]") { t.Errorf("content lost: %q", plain(out)) }
}

func TestHighlightHTML(t *testing.T) {
	out := Highlight("<div>hello</div>", "html")
	if out == "" { t.Fatal("empty output") }
	if !strings.Contains(plain(out), "hello") { t.Errorf("content lost: %q", plain(out)) }
}
