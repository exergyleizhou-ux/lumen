package lab

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/runstate"
)

func TestTenantRegistryIsolatesRootsAndRejectsTraversal(t *testing.T) {
	r, err := newTenantRegistry(t.TempDir(), nil, 4, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a := runstate.Owner{UserID: "alice", WorkspaceID: "same"}
	b := runstate.Owner{UserID: "bob", WorkspaceID: "same"}
	ta, err := r.acquire(a)
	if err != nil {
		t.Fatal(err)
	}
	defer r.release(a)
	tb, err := r.acquire(b)
	if err != nil {
		t.Fatal(err)
	}
	defer r.release(b)
	if ta.Root == tb.Root {
		t.Fatal("tenant roots collide")
	}
	if err := os.WriteFile(filepath.Join(ta.Root, "same.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tb.Root, "same.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.acquire(runstate.Owner{UserID: "../escape", WorkspaceID: "x"}); err == nil {
		t.Fatal("traversal identity accepted")
	}
}

func TestTenantRegistrySymlinkCannotEscapeAndCapacityIsBounded(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	r, err := newTenantRegistry(root, nil, 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	ta, err := r.acquire(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ta.Root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := ta.Workspace.Backend.Resolve("escape/file", true); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if _, err := r.acquire(runstate.Owner{UserID: "b", WorkspaceID: "w"}); err == nil {
		t.Fatal("busy capacity exceeded")
	}
	r.release(a)
	if _, err := r.acquire(runstate.Owner{UserID: "b", WorkspaceID: "w"}); err != nil {
		t.Fatalf("idle LRU not evicted: %v", err)
	}
}

func TestTenantRegistryExistingOwnerSurvivesCapacityCleanup(t *testing.T) {
	now := time.Now()
	r, err := newTenantRegistry(t.TempDir(), nil, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	first, err := r.acquire(a)
	if err != nil {
		t.Fatal(err)
	}
	r.release(a)
	now = now.Add(2 * time.Minute)
	second, err := r.acquire(a)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("existing tenant was evicted before lookup")
	}
}

func TestTenantRegistryEvictionNotifiesOwnerCleanup(t *testing.T) {
	now := time.Now()
	r, err := newTenantRegistry(t.TempDir(), nil, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	a := runstate.Owner{UserID: "a", WorkspaceID: "w"}
	evicted := make(chan runstate.Owner, 1)
	r.onEvict = func(owner runstate.Owner) { evicted <- owner }
	if _, err := r.acquire(a); err != nil {
		t.Fatal(err)
	}
	r.release(a)
	now = now.Add(2 * time.Minute)
	if _, err := r.acquire(runstate.Owner{UserID: "b", WorkspaceID: "w"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-evicted:
		if got != a {
			t.Fatalf("evicted %v", got)
		}
	default:
		t.Fatal("owner cleanup not notified")
	}
}
