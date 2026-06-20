package editverify

import (
	"context"
	"testing"
)

// A verify step whose tool isn't installed (e.g. ruff/tsc absent) must report
// available()==false so the plan SKIPS it — never a failure (which would trigger
// bogus self-repair) and never a pass (which would be a false "✓ verified").
func TestExecRunner_MissingToolIsUnavailable(t *testing.T) {
	r := execRunner{}
	missing := Step{Name: "lint", Dir: t.TempDir(), Args: []string{"lumen-no-such-tool-xyz123", "check", "."}}
	if r.available(missing) {
		t.Error("a missing tool must report available()=false")
	}
	present := Step{Name: "test", Dir: t.TempDir(), Args: []string{"sh", "-c", "true"}}
	if !r.available(present) {
		t.Error("an installed tool (sh) must report available()=true")
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
