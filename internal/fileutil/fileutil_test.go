package fileutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsBinaryFile(t *testing.T) {
	dir := t.TempDir()

	// Text file
	textPath := filepath.Join(dir, "text.txt")
	os.WriteFile(textPath, []byte("hello world"), 0o644)
	bin, err := IsBinaryFile(textPath)
	if err != nil {
		t.Fatalf("IsBinaryFile: %v", err)
	}
	if bin {
		t.Error("text file should not be detected as binary")
	}

	// Binary file (with NUL byte)
	binPath := filepath.Join(dir, "binary.bin")
	os.WriteFile(binPath, []byte{0x48, 0x00, 0x4c, 0x4c}, 0o644)
	bin, err = IsBinaryFile(binPath)
	if err != nil {
		t.Fatalf("IsBinaryFile binary: %v", err)
	}
	if !bin {
		t.Error("binary file with NUL should be detected")
	}
}

func TestValidateReadSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	// Write a file larger than MaxReadSize
	f, _ := os.Create(path)
	f.Truncate(MaxReadSize + 1)
	f.Close()

	err := ValidateReadSize(path)
	if err == nil {
		t.Error("file exceeding MaxReadSize should be rejected")
	}
}

func TestValidateWriteSize(t *testing.T) {
	huge := make([]byte, MaxWriteSize+1)
	err := ValidateWriteSize(huge)
	if err == nil {
		t.Error("content exceeding MaxWriteSize should be rejected")
	}

	small := []byte("ok")
	err = ValidateWriteSize(small)
	if err != nil {
		t.Errorf("small content should pass: %v", err)
	}
}

func TestWorkspaceRoot(t *testing.T) {
	root := WorkspaceRoot()
	t.Logf("WorkspaceRoot = %q", root)
	// Without LUMEN_WORKSPACE_ROOT set, should be empty
	if os.Getenv("LUMEN_WORKSPACE_ROOT") == "" && root != "" {
		t.Log("workspace root detected from git root")
	}
}

func TestValidateWorkspaceBoundary(t *testing.T) {
	err := ValidateWorkspaceBoundary("/tmp/test.txt", "/Users/lei/lumen")
	if err == nil {
		t.Error("path outside workspace should be rejected")
	}

	err = ValidateWorkspaceBoundary("/Users/lei/lumen/internal/test.go", "/Users/lei/lumen")
	if err != nil {
		t.Errorf("path inside workspace should be accepted: %v", err)
	}
}

func TestIsTextFile(t *testing.T) {
	fi := &stubFileInfo{name: "main.go", dir: false}
	if !IsTextFile(fi) {
		t.Error(".go files should be text")
	}

	fi2 := &stubFileInfo{name: "image.png", dir: false}
	if IsTextFile(fi2) {
		t.Error(".png files should not be text")
	}
}

type stubFileInfo struct {
	name string
	dir  bool
}

func (s *stubFileInfo) Name() string       { return s.name }
func (s *stubFileInfo) IsDir() bool        { return s.dir }
func (s *stubFileInfo) Size() int64        { return 0 }
func (s *stubFileInfo) Mode() os.FileMode  { return 0 }
func (s *stubFileInfo) ModTime() time.Time { return time.Time{} }
func (s *stubFileInfo) Sys() any           { return nil }

func TestSafeWriteFileAtomicAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SafeWriteFile(path, "", []byte("new content")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new content" {
		t.Errorf("content = %q, want %q", got, "new content")
	}
	// Existing file mode (0600) must be preserved across the temp+rename.
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 (preserved)", fi.Mode().Perm())
	}
	// No temp file must be left behind in the directory.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "f.txt" {
			t.Errorf("unexpected leftover file after atomic write: %s", e.Name())
		}
	}
}

func TestNewFileMode(t *testing.T) {
	cases := []struct{ umask, want os.FileMode }{
		{0o022, 0o644}, // default umask → 0644
		{0o077, 0o600}, // restrictive umask → 0600 (not a wide-open 0644)
		{0o000, 0o644},
		{0o027, 0o640},
	}
	for _, c := range cases {
		if got := newFileMode(c.umask); got != c.want {
			t.Errorf("newFileMode(%#o) = %#o, want %#o", c.umask, got, c.want)
		}
	}
}
