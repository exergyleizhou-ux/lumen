package diffengine

import (
	"strings"
	"testing"
)

func TestLineDiff(t *testing.T) {
	e := NewEngine()
	r := e.LineDiff("a\nb\nc", "a\nx\nc")
	if r.Added != 1 || r.Removed != 1 {
		t.Error("counts")
	}
	s := FormatDiff(r)
	if !strings.Contains(s, "+ x") {
		t.Error("format")
	}
}
func TestJSONDiff(t *testing.T) {
	e := NewEngine()
	r := e.JSONDiff(map[string]any{"a": 1, "b": 2}, map[string]any{"a": 1, "c": 3})
	if r.Removals != 1 || r.Additions != 1 {
		t.Error("json diff")
	}
}
func TestWordDiff(t *testing.T) {
	e := NewEngine()
	lines := e.WordDiff("hello world", "hello universe")
	t.Logf("word diff: %v lines", len(lines))
}
func TestFormatChanges(t *testing.T) {
	e := NewEngine()
	r := e.JSONDiff(map[string]any{"x": 1}, map[string]any{"x": 2})
	s := FormatChanges(r)
	if s == "" {
		t.Error("format")
	}
}
