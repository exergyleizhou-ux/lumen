package editverify

import (
	"os"
	"path/filepath"
	"testing"
)

// golden helpers

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return string(data)
}

func TestParse_buildError(t *testing.T) {
	out := readFixture(t, "build_error.txt")
	step := Step{Name: "build"}
	diags := Parse(step, out)

	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(diags))
	}
	// First: main.go:10:5: undefined: foo
	if diags[0].File != "main.go" || diags[0].Line != 10 || diags[0].Col != 5 {
		t.Errorf("diag[0] = %s:%d:%d, want main.go:10:5", diags[0].File, diags[0].Line, diags[0].Col)
	}
	if diags[0].Sev != "error" {
		t.Errorf("diag[0].Sev = %q, want error", diags[0].Sev)
	}
	// Second: pkg/helper.go:23:2: imported and not used
	if diags[1].File != "pkg/helper.go" || diags[1].Line != 23 || diags[1].Col != 2 {
		t.Errorf("diag[1] = %s:%d:%d, want pkg/helper.go:23:2", diags[1].File, diags[1].Line, diags[1].Col)
	}
}

func TestParse_buildClean(t *testing.T) {
	out := readFixture(t, "build_clean.txt")
	step := Step{Name: "build"}
	diags := Parse(step, out)
	if len(diags) != 0 {
		t.Fatalf("got %d diagnostics from clean build, want 0", len(diags))
	}
}

func TestParse_vetWarning(t *testing.T) {
	out := readFixture(t, "vet_warning.txt")
	step := Step{Name: "vet"}
	diags := Parse(step, out)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Sev != "warning" {
		t.Errorf("vet diag severity = %q, want warning", diags[0].Sev)
	}
	if diags[0].File != "cmd/lumen/main.go" || diags[0].Line != 42 || diags[0].Col != 2 {
		t.Errorf("vet diag = %s:%d:%d, want cmd/lumen/main.go:42:2", diags[0].File, diags[0].Line, diags[0].Col)
	}
}

func TestParse_testFail(t *testing.T) {
	out := readFixture(t, "test_fail.txt")
	step := Step{Name: "test"}
	diags := Parse(step, out)

	if len(diags) < 1 {
		t.Fatalf("got %d diagnostics from test failure, want at least 1", len(diags))
	}
	// Must contain FAIL: TestMath
	found := false
	for _, d := range diags {
		if d.Msg == "FAIL: TestMath" {
			found = true
			if d.Sev != "error" {
				t.Errorf("FAIL severity = %q, want error", d.Sev)
			}
		}
	}
	if !found {
		t.Errorf("diagnostics missing FAIL: TestMath, got: %+v", diags)
	}
}

func TestParse_testPanic(t *testing.T) {
	out := readFixture(t, "test_panic.txt")
	step := Step{Name: "test"}
	diags := Parse(step, out)

	if len(diags) < 1 {
		t.Fatalf("got %d diagnostics from panic, want at least 1", len(diags))
	}
	found := false
	for _, d := range diags {
		if len(d.Msg) > 0 && d.Msg[:7] == "panic: " {
			found = true
			if d.Sev != "error" {
				t.Errorf("panic severity = %q, want error", d.Sev)
			}
		}
	}
	if !found {
		t.Errorf("diagnostics missing panic, got: %+v", diags)
	}
}

func TestParse_testClean(t *testing.T) {
	out := readFixture(t, "test_clean.txt")
	step := Step{Name: "test"}
	diags := Parse(step, out)
	if len(diags) != 0 {
		t.Fatalf("got %d diagnostics from clean test, want 0: %+v", len(diags), diags)
	}
}

func TestParse_emptyOutput(t *testing.T) {
	diags := Parse(Step{Name: "build"}, "")
	if len(diags) != 0 {
		t.Fatalf("empty output should give no diagnostics, got %d", len(diags))
	}
}

func TestParse_noCol(t *testing.T) {
	// "file.go:LINE: msg" (no column) → Col should be 0
	out := "pkg/main.go:10: cannot use x"
	step := Step{Name: "build"}
	diags := Parse(step, out)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Line != 10 {
		t.Errorf("Line=%d, want 10", diags[0].Line)
	}
	if diags[0].Col != 0 {
		t.Errorf("Col=%d, want 0 (no column in input)", diags[0].Col)
	}
	if diags[0].File != "pkg/main.go" {
		t.Errorf("File=%q, want pkg/main.go", diags[0].File)
	}
}

func TestParse_customSeverity(t *testing.T) {
	// custom step → severity "error" (same as build)
	out := "main.go:1:1: something wrong"
	step := Step{Name: "custom"}
	diags := Parse(step, out)
	if len(diags) != 1 || diags[0].Sev != "error" {
		t.Errorf("custom step diag severity = %q, want error", diags[0].Sev)
	}
}
