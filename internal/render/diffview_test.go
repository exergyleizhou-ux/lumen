package render

import (
	"strings"
	"testing"
)

func TestLangForPath(t *testing.T) {
	cases := map[string]string{
		"main.go": "go", "app.py": "python", "x.ts": "typescript",
		"run.sh": "bash", "data.json": "json", "README.md": "", "noext": "",
	}
	for path, want := range cases {
		if got := LangForPath(path); got != want {
			t.Errorf("LangForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestDiffLineMarkers(t *testing.T) {
	if p := plain(DiffLine('+', "added")); p != "+ added" {
		t.Errorf("add line plain = %q", p)
	}
	if p := plain(DiffLine('-', "gone")); p != "- gone" {
		t.Errorf("del line plain = %q", p)
	}
	if !hasANSI(DiffLine('+', "x")) {
		t.Error("expected colored marker")
	}
}

func TestDiffModifiedLine(t *testing.T) {
	got := Diff("x.go", "a\nb\nc\n", "a\nB\nc\n")
	p := plain(got)
	if !strings.Contains(p, "- b") {
		t.Errorf("missing deletion: %q", p)
	}
	if !strings.Contains(p, "+ B") {
		t.Errorf("missing addition: %q", p)
	}
	if !strings.Contains(p, "a") || !strings.Contains(p, "c") {
		t.Error("context lines lost")
	}
}

func TestDiffNewFile(t *testing.T) {
	got := Diff("x.go", "", "hello\nworld\n")
	p := plain(got)
	if !strings.Contains(p, "+ hello") || !strings.Contains(p, "+ world") {
		t.Errorf("new file lines not all additions: %q", p)
	}
}

func TestDiffRemovedFile(t *testing.T) {
	got := Diff("x.go", "bye\n", "")
	if !strings.Contains(plain(got), "- bye") {
		t.Errorf("removed file not shown as deletion: %q", plain(got))
	}
}

func TestDiffHighlightsCodeContent(t *testing.T) {
	got := Diff("x.go", "", "func main() {}\n")
	if !strings.Contains(got, colKeyword) {
		t.Error("expected keyword highlighting inside diff content")
	}
	if !strings.Contains(plain(got), "func main() {}") {
		t.Errorf("code text lost: %q", plain(got))
	}
}

func TestTruncateVisibleSkipsANSI(t *testing.T) {
	styled := "\x1b[31mhello\x1b[0m world"
	out := TruncateVisible(styled, 5)
	if vis := plain(out); len([]rune(strings.TrimSuffix(vis, "…"))) > 5 {
		t.Errorf("visible length exceeds budget: %q (%d)", vis, len([]rune(vis)))
	}
	if !strings.Contains(plain(out), "hel") {
		t.Errorf("truncated too aggressively: %q", plain(out))
	}
}

func TestTruncateVisibleShortStringUnchanged(t *testing.T) {
	if out := TruncateVisible("hi", 10); out != "hi" {
		t.Errorf("short string changed: %q", out)
	}
}
