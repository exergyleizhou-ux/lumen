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

// pytest's dominant convention is test_<name>.py (not just <name>_test.py); a
// changed module with a sibling test_<name>.py must schedule pytest on it.
func TestDetect_python_findsTestUnderscorePrefix(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "app.py"), "x = 1\n")
	mustWrite(t, filepath.Join(root, "test_app.py"), "def test_x():\n    assert True\n")
	steps := Detect(root, []string{"app.py"}, DefaultConfig())
	if !hasPytestOn(steps, "test_app.py") {
		t.Errorf("sibling test_app.py present → expected pytest on it, got %v", stepNames(steps))
	}
}

// Editing a test file itself must run that test. The old code derived
// <name>_test.py from the changed path → test_app_test.py, which never exists,
// so editing a test silently ran no test.
func TestDetect_python_changedFileIsTest(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "test_app.py"), "def test_x():\n    assert True\n")
	steps := Detect(root, []string{"test_app.py"}, DefaultConfig())
	if !hasPytestOn(steps, "test_app.py") {
		t.Errorf("editing a test file → expected pytest on it, got %v", stepNames(steps))
	}
}

func hasPytestOn(steps []Step, file string) bool {
	for _, s := range steps {
		if s.Name == "test" && len(s.Args) > 0 && s.Args[0] == "pytest" {
			for _, a := range s.Args {
				if a == file {
					return true
				}
			}
		}
	}
	return false
}
