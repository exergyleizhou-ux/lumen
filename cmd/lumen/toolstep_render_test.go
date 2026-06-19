package main

import (
	"strings"
	"testing"
)

// Two read_file calls dispatched in a parallel batch must each render as a
// SELF-CONTAINED line when their result arrives — never a bare orphaned "✓" on
// its own line (the §6 bug: with N parallel tools, N-1 ✓ were orphaned).
func TestToolStepRenderer_ParallelReadsNoOrphanedCheck(t *testing.T) {
	r := newToolStepRenderer()
	if d1 := r.dispatch("id1", "read_file", true, 1); d1 != "" {
		t.Errorf("read-only dispatch should buffer (return empty), got %q", d1)
	}
	if d2 := r.dispatch("id2", "read_file", true, 2); d2 != "" {
		t.Errorf("read-only dispatch should buffer (return empty), got %q", d2)
	}
	o1 := r.result("id1", "read_file", "", false)
	o2 := r.result("id2", "read_file", "", false)
	for i, o := range []string{o1, o2} {
		if !strings.Contains(o, "read_file") || !strings.Contains(o, "✓") {
			t.Errorf("result %d must be a complete line with name+✓ (not an orphaned ✓), got %q", i+1, o)
		}
	}
	if !strings.Contains(o1, "1.") || !strings.Contains(o2, "2.") {
		t.Errorf("step numbers lost: %q / %q", o1, o2)
	}
}

// Side-effecting tools (bash) must show their line immediately on dispatch so a
// slow command gives "started" feedback, then mark on result.
func TestToolStepRenderer_BashShowsStartedImmediately(t *testing.T) {
	r := newToolStepRenderer()
	d := r.dispatch("b1", "bash", false, 1)
	if !strings.Contains(d, "bash") {
		t.Errorf("non-read-only dispatch should print its line immediately, got %q", d)
	}
	res := r.result("b1", "bash", "", false)
	if !strings.Contains(res, "✓") {
		t.Errorf("bash result should show ✓, got %q", res)
	}
}

// A failed read tool still renders a complete line, with ✗ and the error.
func TestToolStepRenderer_BufferedFailureShowsError(t *testing.T) {
	r := newToolStepRenderer()
	r.dispatch("x", "read_file", true, 1)
	o := r.result("x", "read_file", "file not found", false)
	if !strings.Contains(o, "✗") || !strings.Contains(o, "file not found") || !strings.Contains(o, "read_file") {
		t.Errorf("buffered failure should be a complete line with ✗ + error, got %q", o)
	}
}
