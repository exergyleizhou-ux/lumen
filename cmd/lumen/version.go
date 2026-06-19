package main

import "fmt"

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

// versionString renders the human-readable version line for `lumen version`.
// A released build shows the tag plus the commit and build date; a dev build
// shows just "Lumen vdev".
func versionString() string {
	if commit == "none" && date == "unknown" {
		return "Lumen v" + version
	}
	return fmt.Sprintf("Lumen v%s (%s, %s)", version, commit, date)
}
