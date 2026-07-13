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

func TestGuardOperationsResistConcurrentSymlinkSwap(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "victim"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := NewGuard(root)
	if err != nil {
		t.Fatal(err)
	}
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
			_ = os.RemoveAll(filepath.Join(root, "swap"))
			_ = os.Symlink(outside, filepath.Join(root, "swap"))
			_ = os.Remove(filepath.Join(root, "swap"))
			_ = os.Mkdir(filepath.Join(root, "swap"), 0700)
		}
	}()
	for i := 0; i < 500; i++ {
		_ = g.WriteFile("swap/victim", []byte("inside"), 0600)
		_, _ = g.ReadFile("swap/victim")
		_ = g.WriteFile("source", []byte("source"), 0600)
		_ = g.Copy("source", "swap/copied")
		_ = g.Rename("source", "swap/renamed")
		_ = g.RemoveAll("swap/victim")
	}
	close(stop)
	wg.Wait()
	b, err := os.ReadFile(filepath.Join(outside, "victim"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "outside" {
		t.Fatalf("outside file modified: %q", b)
	}
	for _, name := range []string{"copied", "renamed"} {
		if _, err := os.Stat(filepath.Join(outside, name)); !os.IsNotExist(err) {
			t.Fatalf("outside %s created", name)
		}
	}
}
