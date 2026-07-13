package jupyter

import (
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	labworkspace "lumen/internal/science/lab/workspace"
)

func TestGuardedNotebookRejectsTraversalAndSymlinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink race semantics are Unix-specific")
	}
	root, outside := t.TempDir(), t.TempDir()
	g, err := labworkspace.NewGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.MkdirAll("notebooks", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGuarded(g, "../../../outside.ipynb"); err == nil {
		t.Fatal("traversal accepted")
	}
	if err := os.WriteFile(filepath.Join(outside, "victim.ipynb"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			_ = os.RemoveAll(filepath.Join(root, "notebooks"))
			_ = os.Symlink(outside, filepath.Join(root, "notebooks"))
			_ = os.Remove(filepath.Join(root, "notebooks"))
			_ = os.Mkdir(filepath.Join(root, "notebooks"), 0o700)
		}
	}()
	nb := New("safe")
	for i := 0; i < 300; i++ {
		_ = nb.SaveGuarded(g, "notebooks/victim.ipynb")
	}
	stop.Store(true)
	<-done
	b, err := os.ReadFile(filepath.Join(outside, "victim.ipynb"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "outside" {
		t.Fatal("write escaped through swapped symlink")
	}
}
