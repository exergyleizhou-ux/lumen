package main

import (
	"strings"
	"testing"
)

// /undo and /rewind printed the rewound files as a raw Go slice ("[a.go b.go]").
// formatRewound renders them for humans.
func TestFormatRewound(t *testing.T) {
	if got := formatRewound(nil); !strings.Contains(got, "nothing") {
		t.Errorf("empty rewind should say nothing to undo, got %q", got)
	}
	got := formatRewound([]string{"a.go", "b.go"})
	if strings.Contains(got, "[a.go") {
		t.Errorf("should not print a raw Go slice ([a.go b.go]), got %q", got)
	}
	if !strings.Contains(got, "a.go") || !strings.Contains(got, "b.go") || !strings.Contains(got, "2") {
		t.Errorf("should list the files + count, got %q", got)
	}
}
