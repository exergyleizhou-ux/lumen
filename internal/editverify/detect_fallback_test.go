package editverify

import (
	"os"
	"path/filepath"
	"testing"
)

// When a change touches no recognized source file, the verifier must not blindly
// run `go build` — in a non-Go project that is a guaranteed spurious failure.
// Fall back to Go steps ONLY when the root is actually a Go module.
func TestDetect_noCodeFiles_nonGoRoot_skips(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := Detect(root, []string{"README.md"}, DefaultConfig())
	if len(steps) != 0 {
		t.Errorf("non-Go root + non-code change should yield no steps, got %v", stepNames(steps))
	}
}

func TestDetect_noCodeFiles_goRoot_fallsBackToGo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := Detect(root, []string{"README.md"}, DefaultConfig())
	foundBuild := false
	for _, s := range steps {
		if s.Name == "build" && len(s.Args) > 0 && s.Args[0] == "go" {
			foundBuild = true
		}
	}
	if !foundBuild {
		t.Errorf("Go root + non-code change should fall back to go build, got %v", stepNames(steps))
	}
}
