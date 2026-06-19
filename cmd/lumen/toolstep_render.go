package main

import "fmt"

// toolStepRenderer renders tool dispatch/result lines for the non-interactive
// sink. Read-only tools — which the agent dispatches in PARALLEL batches — are
// buffered and emitted as a single complete line when their result arrives, so
// their ✓ can never be orphaned onto a bare line by an interleaved dispatch (the
// §6 parallel-checkmark bug). Side-effecting tools (bash, edit, write) print
// their line immediately on dispatch — preserving "started" feedback and the
// diff rendered beneath the tool name — then append their mark on result.
type toolStepRenderer struct {
	pending map[string]pendingStep
}

type pendingStep struct {
	n    int
	name string
}

func newToolStepRenderer() *toolStepRenderer {
	return &toolStepRenderer{pending: map[string]pendingStep{}}
}

// reset drops any in-flight steps (called between turns).
func (r *toolStepRenderer) reset() { r.pending = map[string]pendingStep{} }

// dispatch returns the text to print when a tool call starts. Read-only tools
// are buffered (return "") so their complete line is emitted on result, immune
// to interleaving from a parallel batch; other tools print their line at once.
func (r *toolStepRenderer) dispatch(id, name string, readOnly bool, n int) string {
	if readOnly {
		r.pending[id] = pendingStep{n: n, name: name}
		return ""
	}
	return dispatchLine(n, name)
}

// result returns the text to print when a tool call completes. A buffered
// (read-only) tool emits its whole line now — number, icon, name, and mark —
// as one self-contained unit; a non-buffered tool appends its mark to the open
// dispatch line.
func (r *toolStepRenderer) result(id, name, errMsg string, blocked bool) string {
	mark := resultMark(errMsg, blocked)
	if p, ok := r.pending[id]; ok {
		delete(r.pending, id)
		return dispatchLine(p.n, p.name) + "  " + mark + "\n"
	}
	return "  " + mark + "\n"
}

func dispatchLine(n int, name string) string {
	return fmt.Sprintf("\n  %s %s %s", col(D, fmt.Sprintf("%2d.", n)), toolIcon(name), col(Y, name))
}

func resultMark(errMsg string, blocked bool) string {
	switch {
	case errMsg != "":
		return col(Rd, "✗") + " " + errMsg
	case blocked:
		return col(Y, "⛔")
	default:
		return col(G, "✓")
	}
}

func col(code, s string) string { return code + s + R }
