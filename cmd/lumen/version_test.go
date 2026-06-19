package main

import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	saveV, saveC, saveD := version, commit, date
	defer func() { version, commit, date = saveV, saveC, saveD }()

	// dev build: just the version, no build metadata noise
	version, commit, date = "dev", "none", "unknown"
	if got := versionString(); !strings.Contains(got, "Lumen") || !strings.Contains(got, "dev") {
		t.Errorf("dev version line should name Lumen + version, got %q", got)
	}

	// released build: version + commit + date all surfaced
	version, commit, date = "1.0.0", "abc1234", "2026-06-20"
	got := versionString()
	for _, want := range []string{"1.0.0", "abc1234", "2026-06-20"} {
		if !strings.Contains(got, want) {
			t.Errorf("release version line missing %q: %q", want, got)
		}
	}
}
