package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func pwrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A task says "don't modify the tests". If the agent edits a *_test.go to make a
// failing assertion pass, the check would go green — a cheat. The guard must flag it.
func TestProtectedTestsUnchanged_FlagsModifiedTest(t *testing.T) {
	orig, run := t.TempDir(), t.TempDir()
	pwrite(t, orig, "calc/calc.go", "package calc\nfunc Average() int { return 0 }\n")
	pwrite(t, orig, "calc/calc_test.go", "package calc\n// asserts Average()==10000\n")
	pwrite(t, run, "calc/calc.go", "package calc\nfunc Average() int { return 1 }\n")
	pwrite(t, run, "calc/calc_test.go", "package calc\n// assertion deleted\n") // tampered

	ok, changed := ProtectedTestsUnchanged(orig, run)
	if ok {
		t.Fatal("expected a modified test file to be flagged")
	}
	if len(changed) != 1 || filepath.Base(changed[0]) != "calc_test.go" {
		t.Fatalf("expected calc_test.go flagged, got %v", changed)
	}
}

// Deleting the test file entirely also removes the failing assertion — must flag.
func TestProtectedTestsUnchanged_FlagsDeletedTest(t *testing.T) {
	orig, run := t.TempDir(), t.TempDir()
	pwrite(t, orig, "x_test.go", "package x\nfunc TestX(t *testing.T) {}\n")
	pwrite(t, run, "x.go", "package x\n") // test file gone in run

	ok, changed := ProtectedTestsUnchanged(orig, run)
	if ok || len(changed) != 1 {
		t.Fatalf("expected deleted test flagged, ok=%v changed=%v", ok, changed)
	}
}

// Editing only non-test source (the legitimate fix) must NOT be flagged.
func TestProtectedTestsUnchanged_OkWhenOnlySourceChanged(t *testing.T) {
	orig, run := t.TempDir(), t.TempDir()
	pwrite(t, orig, "x.go", "package x\n// bug\n")
	pwrite(t, orig, "x_test.go", "package x\nfunc TestX(t *testing.T) {}\n")
	pwrite(t, run, "x.go", "package x\n// fixed\n")
	pwrite(t, run, "x_test.go", "package x\nfunc TestX(t *testing.T) {}\n") // identical

	ok, changed := ProtectedTestsUnchanged(orig, run)
	if !ok || len(changed) != 0 {
		t.Fatalf("a source-only change must pass, got ok=%v changed=%v", ok, changed)
	}
}
