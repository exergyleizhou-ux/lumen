package checkpoint

import (
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/diff"
)

func TestSaveAndRewind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	original := "original content"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New()
	s.Save(path, original)

	os.WriteFile(path, []byte("modified"), 0o644)

	rewound, err := s.Rewind()
	if err != nil {
		t.Fatalf("rewind failed: %v", err)
	}
	if len(rewound) != 1 {
		t.Errorf("expected 1 rewound file, got %d", len(rewound))
	}

	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("expected %q after rewind, got %q", original, string(data))
	}
}

func TestSaveFromChangeNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	s := New()
	ch := diff.Change{Path: path, Before: "", After: "new content", New: true}
	s.SaveFromChange(ch)

	if !s.HasSnapshots() {
		t.Error("should have snapshots after SaveFromChange")
	}
	if s.Count() != 1 {
		t.Errorf("expected 1 snapshot, got %d", s.Count())
	}
}

func TestHasSnapshots(t *testing.T) {
	s := New()
	if s.HasSnapshots() {
		t.Error("new store should not have snapshots")
	}
	s.Save("/tmp/test.txt", "content")
	if !s.HasSnapshots() {
		t.Error("should have snapshots after Save")
	}
}

func TestClear(t *testing.T) {
	s := New()
	s.Save("/tmp/a.txt", "a")
	s.Save("/tmp/b.txt", "b")
	s.Clear()
	if s.HasSnapshots() {
		t.Error("should not have snapshots after Clear")
	}
	if s.Count() != 0 {
		t.Errorf("Count should be 0 after Clear, got %d", s.Count())
	}
}

func TestRewindTwiceIsSafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("v1"), 0o644)

	s := New()
	s.Save(path, "v1")
	os.WriteFile(path, []byte("v2"), 0o644)

	s.Rewind()
	rewound, err := s.Rewind()
	if err != nil {
		t.Fatalf("second rewind should not error: %v", err)
	}
	if len(rewound) != 0 {
		t.Errorf("second rewind should return empty list, got %v", rewound)
	}
}

func TestList(t *testing.T) {
	s := New()
	s.Save("/tmp/a.txt", "a")
	s.Save("/tmp/b.txt", "b")

	list := s.List()
	if len(list) != 2 {
		t.Errorf("expected 2 paths in list, got %d", len(list))
	}
}
