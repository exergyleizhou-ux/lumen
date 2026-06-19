package editverify

import (
	"fmt"
	"strings"
)

// FormatFeedback renders a failing Result (or LSP-warning Result) into the
// compact observation the agent loop injects back into the model for self-repair
// (spec §2.4). cycle is the current repair attempt (1-based) and max is the
// configured ceiling.
//
// Returns "" when the result is fully clean (OK and no LSP diagnostics).
func FormatFeedback(r Result, cycle, max int) string {
	// Fully clean — nothing to feed back.
	if r.OK && len(r.LSPDiags) == 0 {
		return ""
	}

	var b strings.Builder

	// Build failure takes priority
	if !r.OK {
		step := "?"
		cmd := ""
		if r.Failed != nil {
			step = r.Failed.Name
			cmd = strings.Join(r.Failed.Args, " ")
		}
		if cmd != "" {
			fmt.Fprintf(&b, "⚠ verify failed at step `%s` (%s):\n", step, cmd)
		} else {
			fmt.Fprintf(&b, "⚠ verify failed at step `%s`:\n", step)
		}

		if len(r.Diagnostics) > 0 {
			for _, d := range r.Diagnostics {
				switch {
				case d.File != "" && d.Col > 0:
					fmt.Fprintf(&b, "  %s:%d:%d: %s\n", d.File, d.Line, d.Col, d.Msg)
				case d.File != "":
					fmt.Fprintf(&b, "  %s:%d: %s\n", d.File, d.Line, d.Msg)
				default:
					fmt.Fprintf(&b, "  %s\n", d.Msg)
				}
			}
		} else if strings.TrimSpace(r.Output) != "" {
			for _, line := range strings.Split(strings.TrimRight(r.Output, "\n"), "\n") {
				fmt.Fprintf(&b, "  %s\n", line)
			}
		}
	}

	// LSP diagnostics (even when build passes) — gopls warnings the model should see
	if len(r.LSPDiags) > 0 {
		if r.OK {
			fmt.Fprintf(&b, "ℹ build+vet+test passed, but gopls reported issues:\n")
		}
		for _, d := range r.LSPDiags {
			switch {
			case d.File != "" && d.Col > 0:
				fmt.Fprintf(&b, "  %s:%d:%d: %s\n", d.File, d.Line, d.Col, d.Msg)
			case d.File != "":
				fmt.Fprintf(&b, "  %s:%d: %s\n", d.File, d.Line, d.Msg)
			default:
				fmt.Fprintf(&b, "  %s\n", d.Msg)
			}
		}
	}

	if !r.OK {
		// A failing test (flaky/pre-existing) or a whole-repo lint (ruff check .
		// scans files the edit never touched) may be unrelated to the edit; a
		// build/vet failure is almost always the edit's fault (and build is the
		// dependent-break signal). Don't let the model "fix" unrelated files.
		if r.Failed != nil && (r.Failed.Name == "test" || r.Failed.Name == "lint") {
			fmt.Fprintf(&b, "Note: these failures may predate your edit, be flaky, or come from a whole-repo scan of files you didn't touch. Confirm your change actually caused them before fixing; if they are unrelated to your edit, say so and stop rather than modifying unrelated code. (repair cycle %d/%d)", cycle, max)
		} else {
			fmt.Fprintf(&b, "Fix these, then continue. (repair cycle %d/%d)", cycle, max)
		}
	}
	return b.String()
}
