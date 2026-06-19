package agent

import (
	"context"
	"testing"
)

// The auto-compaction circuit breaker (consecutiveCompacts >= 3 → compactStuck)
// must be a PER-TURN guard, not a lifetime kill-switch. If it is never reset,
// a long healthy session that legitimately compacts 3 times over its life trips
// the breaker permanently — compaction is then disabled exactly when it is most
// needed, the session grows unbounded, and every later turn 400s on the context
// limit. Run() must reset the breaker each turn.
func TestRunResetsCompactionBreakerPerTurn(t *testing.T) {
	a := New(&mockProvider{name: "test"}, testRegistry(), NewSession(""), Options{MaxSteps: 1})

	// Simulate prior turns that tripped the lifetime breaker.
	a.compactStuck = true
	a.consecutiveCompacts = 3
	a.softCompactNoticed = true

	if err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}

	if a.compactStuck {
		t.Fatal("compactStuck must reset each turn (else compaction is permanently disabled)")
	}
	if a.consecutiveCompacts != 0 {
		t.Fatalf("consecutiveCompacts = %d, want 0 (per-turn reset)", a.consecutiveCompacts)
	}
	if a.softCompactNoticed {
		t.Fatal("softCompactNoticed must reset each turn")
	}
}
