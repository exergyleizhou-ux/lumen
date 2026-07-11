package main

import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	saveV, saveC, saveD := version, commit, date
	defer func() { version, commit, date = saveV, saveC, saveD }()

	// pin via env so a repo VERSION file cannot override unit expectations
	t.Setenv("LUMEN_VERSION", "dev")
	version, commit, date = "dev", "none", "unknown"
	if got := versionString(); !strings.Contains(got, "Lumen") || !strings.Contains(got, "dev") {
		t.Errorf("dev version line should name Lumen + version, got %q", got)
	}

	// released build: version + commit + date all surfaced (env cleared)
	t.Setenv("LUMEN_VERSION", "")
	version, commit, date = "1.0.0", "abc1234", "2026-06-20"
	got := versionString()
	for _, want := range []string{"1.0.0", "abc1234", "2026-06-20"} {
		if !strings.Contains(got, want) {
			t.Errorf("release version line missing %q: %q", want, got)
		}
	}
}

func TestResolveVersionEnvAndLdflags(t *testing.T) {
	saveV := version
	defer func() { version = saveV }()
	t.Setenv("LUMEN_VERSION", "from-env")
	version = "1.2.3"
	if got := resolveVersion(); got != "from-env" {
		t.Fatalf("env should win, got %q", got)
	}
	t.Setenv("LUMEN_VERSION", "")
	version = "1.2.3"
	if got := resolveVersion(); got != "1.2.3" {
		t.Fatalf("ldflags version should win over file when non-dev, got %q", got)
	}
}
