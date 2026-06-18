package editverify

import (
	"path/filepath"
	"testing"
)

// A pytest step must only be scheduled when the convention-named test file
// actually exists. The original gate used filepath.Glob's error (nil even when
// nothing matches), so it scheduled `pytest <missing>_test.py`, which errors
// "file not found" and false-fails verification on any .py edit lacking a
// sibling test.
func TestDetect_python_noTestStepWhenTestFileAbsent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "app.py"), "x = 1\n")
	steps := Detect(root, []string{"app.py"}, DefaultConfig())
	for _, s := range steps {
		if s.Name == "test" {
			t.Errorf("no test file present → must not add a pytest step, got %v", stepNames(steps))
		}
	}
}

// When the sibling test file IS present, the pytest step is scheduled.
func TestDetect_python_addsTestStepWhenTestFilePresent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "app.py"), "x = 1\n")
	mustWrite(t, filepath.Join(root, "app_test.py"), "def test_x():\n    assert True\n")
	steps := Detect(root, []string{"app.py"}, DefaultConfig())
	found := false
	for _, s := range steps {
		if s.Name == "test" && len(s.Args) > 0 && s.Args[0] == "pytest" {
			found = true
		}
	}
	if !found {
		t.Errorf("sibling app_test.py present → expected a pytest step, got %v", stepNames(steps))
	}
}
