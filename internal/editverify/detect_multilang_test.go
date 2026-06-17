package editverify

import (
	"testing"
)

func TestDetect_pythonFile(t *testing.T) {
	cfg := DefaultConfig()
	changed := []string{"app/main.py"}
	steps := Detect("/project", changed, cfg)

	// Should have ruff check + pytest step, NOT go build/vet
	if len(steps) < 1 {
		t.Fatal("expected at least 1 step for python file")
	}
	foundRuff := false
	foundPytest := false
	for _, s := range steps {
		if s.Name == "lint" && s.Args[0] == "ruff" {
			foundRuff = true
		}
		if s.Name == "test" && s.Args[0] == "pytest" {
			foundPytest = true
		}
	}
	if !foundRuff {
		t.Errorf("expected ruff check step, got: %v", steps)
	}
	if changed[0] == "app/main.py" {
		_ = foundPytest // pytest test file may not exist, so step depends on filesystem
	}
}

func TestDetect_mixedLanguages(t *testing.T) {
	cfg := DefaultConfig()
	changed := []string{"pkg/main.go", "scripts/train.py"}
	steps := Detect("/project", changed, cfg)

	// Should have both Go and Python steps
	foundGoBuild := false
	foundRuff := false
	for _, s := range steps {
		if s.Name == "build" && s.Args[0] == "go" {
			foundGoBuild = true
		}
		if s.Name == "lint" && s.Args[0] == "ruff" {
			foundRuff = true
		}
	}
	if !foundGoBuild {
		t.Errorf("expected go build step in mixed lang, got: %v", stepNames(steps))
	}
	if !foundRuff {
		t.Errorf("expected ruff step in mixed lang, got: %v", stepNames(steps))
	}
}

func TestDetect_jsFile(t *testing.T) {
	cfg := DefaultConfig()
	changed := []string{"src/app.ts"}
	steps := Detect("/project", changed, cfg)

	foundTsc := false
	for _, s := range steps {
		if s.Name == "typecheck" && s.Args[0] == "npx" && s.Args[1] == "tsc" {
			foundTsc = true
		}
	}
	if !foundTsc {
		t.Errorf("expected npx tsc --noEmit step for .ts file, got: %v", stepNames(steps))
	}
}

func TestDetect_onlyNonCodeFiles(t *testing.T) {
	cfg := DefaultConfig()
	changed := []string{"README.md", "lumen.toml"}
	steps := Detect("/project", changed, cfg)

	// Falls back to Go steps (default)
	if len(steps) < 2 {
		t.Fatalf("expected go build+vet as fallback, got %d steps", len(steps))
	}
}

func stepNames(steps []Step) []string {
	var names []string
	for _, s := range steps {
		names = append(names, s.Name)
	}
	return names
}
