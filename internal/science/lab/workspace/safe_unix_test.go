//go:build !windows

package workspace

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteFileResistsSymlinkParentSwap(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	parent := filepath.Join(root, "dir")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	g, _ := NewGuard(root)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Remove(parent)
			_ = os.Symlink(out, parent)
			_ = os.Remove(parent)
			_ = os.Mkdir(parent, 0o700)
		}
	}()
	for i := 0; i < 500; i++ {
		_ = g.WriteFile("dir/pwned", []byte("x"), 0o600)
	}
	close(stop)
	wg.Wait()
	if _, err := os.Stat(filepath.Join(out, "pwned")); !os.IsNotExist(err) {
		t.Fatalf("write escaped root: %v", err)
	}
}
