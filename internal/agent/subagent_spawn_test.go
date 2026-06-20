package agent

import (
	"context"
	"sync"
	"testing"
)

func TestSpawnReturnsFinalAnswer(t *testing.T) {
	// mockProvider streams "ok" then done — the spawned sub-agent's final
	// assistant message should be returned verbatim.
	out, err := Spawn(context.Background(), SpawnConfig{
		Provider:  &mockProvider{name: "test"},
		ParentReg: testRegistry(),
		MaxSteps:  2,
	}, "do the thing")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if out != "ok" {
		t.Errorf("Spawn = %q, want %q", out, "ok")
	}
}

func TestSpawnIsConcurrencySafe(t *testing.T) {
	// Several sub-agents spawned at once must not race (run under -race).
	cfg := SpawnConfig{Provider: &mockProvider{name: "test"}, ParentReg: testRegistry(), MaxSteps: 2}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Spawn(context.Background(), cfg, "x"); err != nil {
				t.Errorf("Spawn: %v", err)
			}
		}()
	}
	wg.Wait()
}
