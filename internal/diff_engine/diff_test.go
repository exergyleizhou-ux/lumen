package diff_engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeEmpty(t *testing.T) {
	d := Compute("", "")
	if len(d.Hunks) != 0 {
		t.Error("empty diff should have no hunks")
	}
}

func TestComputeNoChange(t *testing.T) {
	d := Compute("hello\nworld", "hello\nworld")
	if len(d.Hunks) != 0 {
		t.Error("no change should have no hunks")
	}
}

func TestComputeAddition(t *testing.T) {
	d := Compute("hello", "hello\nworld")
	if len(d.Hunks) == 0 {
		t.Error("addition should produce a hunk")
	}
}

func TestComputeDeletion(t *testing.T) {
	d := Compute("hello\nworld", "hello")
	if len(d.Hunks) == 0 {
		t.Error("deletion should produce a hunk")
	}
}

func TestComputeChange(t *testing.T) {
	d := Compute("hello", "world")
	if len(d.Hunks) == 0 {
		t.Error("change should produce a hunk")
	}
}

func TestUnifiedDiff(t *testing.T) {
	d := Compute("old", "new")
	d.OldPath = "/tmp/old.txt"
	d.NewPath = "/tmp/new.txt"
	out := d.UnifiedDiff()
	if !strings.Contains(out, "---") || !strings.Contains(out, "+++") {
		t.Error("unified diff should have headers")
	}
}

func TestSimpleDiff(t *testing.T) {
	out := SimpleDiff("a\nb\nc", "a\nx\nc")
	if !strings.Contains(out, "- b") || !strings.Contains(out, "+ x") {
		t.Errorf("simple diff: %q", out)
	}
}

func TestSideBySide(t *testing.T) {
	out := SideBySide("old line", "new line", 80)
	if out == "" {
		t.Error("side-by-side should not be empty")
	}
}

func TestDiffFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	os.WriteFile(a, []byte("hello"), 0o644)
	os.WriteFile(b, []byte("world"), 0o644)

	d, err := DiffFiles(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Hunks) == 0 {
		t.Error("file diff should produce a hunk")
	}
}
