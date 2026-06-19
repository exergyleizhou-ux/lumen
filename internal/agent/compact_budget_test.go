package agent

import "testing"

// The model-compaction summary budget must scale with the content and have a
// sane floor/ceiling — the old min(keepFirst*200, 4096) gave only 600 tokens
// for the default keepFirst=3, truncating summaries of long sessions.
func TestCompactSummaryBudget(t *testing.T) {
	if b := compactSummaryBudget(0); b < 1024 {
		t.Errorf("tiny input should still get at least the floor budget, got %d", b)
	}
	if b := compactSummaryBudget(100_000_000); b > 4096 {
		t.Errorf("huge input should be capped at the ceiling, got %d", b)
	}
	// a moderately large session should get more than the old 600-token cap
	if b := compactSummaryBudget(200_000); b <= 600 {
		t.Errorf("a large session should get a bigger budget than the old 600, got %d", b)
	}
}
