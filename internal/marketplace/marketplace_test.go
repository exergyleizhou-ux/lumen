package marketplace

import (
	"testing"
	"time"
)

// TestMutatorsDoNotDeadlock is a regression test for the re-entrant lock bug
// where AddItem/MarkInstalled/MarkUninstalled held s.mu (write) and then called
// saveCatalog, which took s.mu.RLock — a guaranteed self-deadlock. The mutators
// now call saveCatalogLocked, which assumes the lock is held.
func TestMutatorsDoNotDeadlock(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	done := make(chan struct{})
	go func() {
		store.AddItem(&Item{Name: "ripgrep", Type: "skill", Version: "1.0.0"})
		store.MarkInstalled("ripgrep", "1.0.0")
		store.MarkUninstalled("ripgrep")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("marketplace mutators deadlocked (re-entrant lock regression)")
	}

	// The catalog should have been persisted to disk by saveCatalogLocked.
	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.items["ripgrep"]; !ok {
		t.Fatal("expected ripgrep to be persisted in catalog")
	}
}
