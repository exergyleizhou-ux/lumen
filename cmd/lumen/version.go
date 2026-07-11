package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Build metadata. Overridden at release time via -ldflags by goreleaser:
//
//	-X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}}
//
// A plain `go build` leaves them at these dev defaults, which is honest.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// resolveVersion prefers ldflags, then LUMEN_VERSION env, then a VERSION file
// next to the executable or in the working tree (repo root).
func resolveVersion() string {
	if v := strings.TrimSpace(os.Getenv("LUMEN_VERSION")); v != "" {
		return v
	}
	if version != "" && version != "dev" {
		return version
	}
	candidates := []string{"VERSION"}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "VERSION"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "VERSION"),
			filepath.Join(wd, "..", "VERSION"),
			filepath.Join(wd, "..", "..", "VERSION"),
		)
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	return version
}

// versionString renders the human-readable version line for `lumen version`.
// A released build shows the tag plus the commit and build date; a dev build
// shows just "Lumen vdev".
func versionString() string {
	v := resolveVersion()
	if commit == "none" && date == "unknown" {
		return "Lumen v" + v
	}
	return fmt.Sprintf("Lumen v%s (%s, %s)", v, commit, date)
}
