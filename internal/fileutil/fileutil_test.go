package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeWriteBlocksSymlinkEscape(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Fatal(err)
	}

	// Writing a new file through a workspace symlink that points outside must
	// be blocked when the boundary is enforced.
	err := SafeWriteFile(filepath.Join(ws, "link", "planted.txt"), ws, []byte("PWNED"))
	if err == nil {
		t.Error("write through a workspace symlink to outside must be blocked")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "planted.txt")); statErr == nil {
		t.Error("a file was planted OUTSIDE the workspace — boundary escaped")
	}
}

func TestSafeWriteAllowsNormalFile(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	if err := SafeWriteFile(filepath.Join(ws, "sub", "ok.txt"), ws, []byte("hi")); err != nil {
		t.Fatalf("normal in-workspace write should succeed: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, "sub", "ok.txt"))
	if string(got) != "hi" {
		t.Errorf("content: want %q, got %q", "hi", string(got))
	}
}

func TestSafeReadBlocksOutsideAndSymlink(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("SECRET"), 0o644)
	os.Symlink(outside, filepath.Join(ws, "link"))

	if _, _, _, err := SafeReadFile(filepath.Join(outside, "secret.txt"), ws, 0, 0); err == nil {
		t.Error("reading an absolute path outside the workspace must be blocked")
	}
	if _, _, _, err := SafeReadFile(filepath.Join(ws, "link", "secret.txt"), ws, 0, 0); err == nil {
		t.Error("reading through a workspace symlink to outside must be blocked")
	}
}

func TestSafeReadSizeAndBinary(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	// binary file (NUL byte) is rejected
	os.WriteFile(filepath.Join(ws, "bin"), []byte{0x00, 0x01, 0x02}, 0o644)
	if _, _, _, err := SafeReadFile(filepath.Join(ws, "bin"), ws, 0, 0); err == nil {
		t.Error("binary file should be rejected by SafeReadFile")
	}
}
