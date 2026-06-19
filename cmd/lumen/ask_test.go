package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"lumen/internal/event"
)

func TestParseAskChoice(t *testing.T) {
	cases := []struct {
		in    string
		n     int
		multi bool
		want  []int
	}{
		{"1", 3, false, []int{0}},
		{"2", 3, false, []int{1}},
		{"", 3, false, []int{0}},        // empty → default first
		{"9", 3, false, []int{0}},       // out of range → default first
		{"abc", 3, false, []int{0}},     // junk → default first
		{"1,3", 3, true, []int{0, 2}},   // multi, comma
		{"2 3", 3, true, []int{1, 2}},   // multi, space
		{"3,1", 3, false, []int{2}},     // single keeps first valid only
		{"2,2,2", 3, true, []int{1}},    // dedupe
	}
	for _, c := range cases {
		got := parseAskChoice(c.in, c.n, c.multi)
		if len(got) != len(c.want) {
			t.Fatalf("parseAskChoice(%q,%d,%v)=%v want %v", c.in, c.n, c.multi, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("parseAskChoice(%q,%d,%v)=%v want %v", c.in, c.n, c.multi, got, c.want)
			}
		}
	}
}

func TestReadUntilNewlineDoesNotOverRead(t *testing.T) {
	// Two lines in one stream: the first read must stop at the first newline so
	// the second line is still available (the line editor reads the same stdin).
	r := strings.NewReader("1\n2\n")
	if s, _ := readUntilNewline(r); s != "1" {
		t.Fatalf("first line = %q want 1", s)
	}
	if s, _ := readUntilNewline(r); s != "2" {
		t.Fatalf("second line = %q want 2", s)
	}
}

func TestLineAskerEndToEnd(t *testing.T) {
	in := strings.NewReader("2\n")
	var out bytes.Buffer
	a := &lineAsker{in: in, out: &out}
	ans, err := a.Ask(context.Background(), []event.AskQuestion{{
		Header:   "topic",
		Question: "哪个?",
		Options:  []event.AskOption{{Label: "A"}, {Label: "B"}},
	}})
	if err != nil {
		t.Fatalf("Ask err: %v", err)
	}
	if len(ans) != 1 || len(ans[0].Answers) != 1 || ans[0].Answers[0] != "B" {
		t.Fatalf("expected choice B, got %+v", ans)
	}
	// The rendered prompt shows the options.
	if !strings.Contains(out.String(), "A") || !strings.Contains(out.String(), "B") {
		t.Fatalf("prompt did not render options: %q", out.String())
	}
}
