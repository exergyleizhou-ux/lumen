package editverify

import (
	"context"
	"testing"
)

// A verify step whose tool isn't installed (e.g. ruff/tsc absent) must be SKIPPED
// (treated as ok), never reported as a failure — otherwise activating verify in a
// project with a partial toolchain would trigger bogus self-repair cycles.
func TestExecRunner_MissingToolIsSkipped(t *testing.T) {
	r := execRunner{}
	step := Step{Name: "lint", Dir: t.TempDir(), Args: []string{"lumen-no-such-tool-xyz123", "check", "."}}
	out, ok := r.Run(context.Background(), step)
	if !ok {
		t.Errorf("missing tool should be skipped (ok=true), got ok=false (out=%q)", out)
	}
}

// A tool that IS present but exits non-zero is a real failure, not a skip.
func TestExecRunner_RealFailureIsNotOK(t *testing.T) {
	r := execRunner{}
	step := Step{Name: "test", Dir: t.TempDir(), Args: []string{"sh", "-c", "exit 1"}}
	if _, ok := r.Run(context.Background(), step); ok {
		t.Error("a present command exiting non-zero must be ok=false")
	}
}
