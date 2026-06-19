package editverify

import (
	"os"
	"path/filepath"
	"testing"
)

// IsSupportedProject decides whether the verify-after-edit loop should activate
// for a working directory. It must recognize Go, JS/TS, and Python projects by
// their root markers — not just go.mod (the old Go-only gate left the loop dead
// in Python/JS repos even though Detect supports those languages).
func TestIsSupportedProject(t *testing.T) {
	cases := []struct {
		name   string
		marker string // file created at root ("" = none)
		want   bool
	}{
		{"go module", "go.mod", true},
		{"npm/js package", "package.json", true},
		{"python pyproject", "pyproject.toml", true},
		{"python setup.py", "setup.py", true},
		{"python setup.cfg", "setup.cfg", true},
		{"python requirements", "requirements.txt", true},
		{"empty dir", "", false},
		{"only unrelated files", "README.md", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.marker != "" {
				if err := os.WriteFile(filepath.Join(dir, tc.marker), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := IsSupportedProject(dir); got != tc.want {
				t.Errorf("IsSupportedProject(dir with %q) = %v, want %v", tc.marker, got, tc.want)
			}
		})
	}
}
