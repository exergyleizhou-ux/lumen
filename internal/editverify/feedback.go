package editverify

import (
	"fmt"
	"strings"
)

// FormatFeedback renders a failing Result into the compact observation the agent
// loop injects back into the model for self-repair (spec §2.4). cycle is the
// current repair attempt (1-based) and max is the configured ceiling.
//
// Returns "" when the result is OK (nothing to feed back).
func FormatFeedback(r Result, cycle, max int) string {
	if r.OK {
		return ""
	}

	var b strings.Builder
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
		// No structured diagnostics — fall back to the raw (already truncated) output.
		for _, line := range strings.Split(strings.TrimRight(r.Output, "\n"), "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}

	fmt.Fprintf(&b, "Fix these, then continue. (repair cycle %d/%d)", cycle, max)
	return b.String()
}
