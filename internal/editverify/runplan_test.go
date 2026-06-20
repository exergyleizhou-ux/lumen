package editverify

import (
	"context"
	"testing"
)

// planRunner is a fake that can mark named steps unavailable (tool absent) and
// fail one named step, recording what actually ran.
type planRunner struct {
	absent map[string]bool
	fail   string
	ran    []string
}

func (r *planRunner) available(s Step) bool { return !r.absent[s.Name] }
func (r *planRunner) Run(_ context.Context, s Step) (string, bool) {
	r.ran = append(r.ran, s.Name)
	return "", s.Name != r.fail
}

func steps(names ...string) []Step {
	out := make([]Step, len(names))
	for i, n := range names {
		out[i] = Step{Name: n, Args: []string{n}}
	}
	return out
}

func TestRunPlan_SkippedStepsNotRunNorCounted(t *testing.T) {
	r := &planRunner{absent: map[string]bool{"build": true, "test": true}}
	ran, failed, _ := runPlan(context.Background(), r, steps("build", "vet", "test"))
	if failed != nil {
		t.Fatalf("no step fails, but got failure %v", failed)
	}
	if ran != 1 { // only "vet" was available
		t.Errorf("ran = %d, want 1 (only the available step)", ran)
	}
	if len(r.ran) != 1 || r.ran[0] != "vet" {
		t.Errorf("only 'vet' should have executed, ran=%v", r.ran)
	}
}

// THE false-pass fix: when every step's tool is absent, the plan ran nothing —
// ran must be 0 (so the caller shows "skipped", not a green ✓ over unverified code).
func TestRunPlan_AllToolsAbsentRunsNothing(t *testing.T) {
	r := &planRunner{absent: map[string]bool{"build": true, "vet": true, "test": true}}
	ran, failed, _ := runPlan(context.Background(), r, steps("build", "vet", "test"))
	if ran != 0 || failed != nil {
		t.Errorf("all-absent plan: ran=%d failed=%v, want ran=0 failed=nil", ran, failed)
	}
}

func TestRunPlan_FirstFailureStopsAndReports(t *testing.T) {
	r := &planRunner{fail: "vet"}
	ran, failed, _ := runPlan(context.Background(), r, steps("build", "vet", "test"))
	if failed == nil || failed.Name != "vet" {
		t.Fatalf("expected failure at 'vet', got %v", failed)
	}
	if ran != 2 { // build + vet ran; test never reached
		t.Errorf("ran = %d, want 2 (stop at first failure)", ran)
	}
}
