package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewLocalIsolatesRootsAndEnvironment(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	env := map[string]string{"RUN_MARKER": "alpha"}

	wsA, err := NewLocal("a", rootA, "user-a", env)
	if err != nil {
		t.Fatal(err)
	}
	wsB, err := NewLocal("b", rootB, "user-b", map[string]string{"RUN_MARKER": "beta"})
	if err != nil {
		t.Fatal(err)
	}
	env["RUN_MARKER"] = "mutated"

	gotA, err := wsA.Backend.Resolve("note.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := wsB.Backend.Resolve("note.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if gotA != filepath.Join(wsA.Root, "note.txt") || gotB != filepath.Join(wsB.Root, "note.txt") {
		t.Fatalf("roots crossed: A=%q B=%q", gotA, gotB)
	}
	if strings.Join(wsA.Environment([]string{"BASE=1"}), "\n") != "BASE=1\nRUN_MARKER=alpha" {
		t.Fatalf("environment was not copied/overlaid: %v", wsA.Environment([]string{"BASE=1"}))
	}

	ctx := WithContext(context.Background(), wsA)
	from, ok := FromContext(ctx)
	if !ok {
		t.Fatal("workspace missing from context")
	}
	from.Env["RUN_MARKER"] = "changed"
	fromAgain, _ := FromContext(ctx)
	if fromAgain.Env["RUN_MARKER"] != "alpha" {
		t.Fatalf("context workspace was mutable: %#v", fromAgain.Env)
	}
}

func TestLocalBackendRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ws, err := NewLocal("ws", root, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ws.Backend.Resolve(filepath.Join("..", "escape.txt"), true); err == nil {
		t.Fatal("expected traversal to be rejected")
	}

	link := filepath.Join(root, "outside")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := ws.Backend.Resolve(filepath.Join("outside", "escape.txt"), true); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestNewLocalRejectsFileRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocal("bad", path, "", nil); err == nil {
		t.Fatal("expected file root to be rejected")
	}
}
