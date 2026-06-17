package editverify

import (
	"path/filepath"
	"strings"
)

// Detect builds the ordered verification plan for the given changed files.
//
// Language detection is automatic based on file extensions:
//   - .go files → go build + go vet + (optional) go test
//   - .py files → ruff check + pytest (if ast/ruff present in project)
//   - .js/.ts files → tsc --noEmit + jest (if package.json present)
//   - Mixed files → build/vet/test steps for each detected language
//
// cfg.Command override takes priority over all auto-detection.
func Detect(root string, changed []string, cfg Config) []Step {
	// Rule 1: custom override
	if cfg.Command != "" {
		return []Step{{Name: "custom", Dir: root, Args: []string{"sh", "-c", cfg.Command}}}
	}

	var steps []Step

	// Detect which languages were affected
	langs := detectLanguages(changed)
	for _, lang := range langs {
		switch lang {
		case "go":
			steps = append(steps, goSteps(root, changed, cfg)...)
		case "python":
			steps = append(steps, pythonSteps(root, changed, cfg)...)
		case "js":
			steps = append(steps, jsSteps(root, changed, cfg)...)
		}
	}

	// If nothing detected, fall back to Go (project default)
	if len(steps) == 0 {
		steps = goSteps(root, changed, cfg)
	}

	return steps
}

func detectLanguages(changed []string) []string {
	hasGo, hasPy, hasJS := false, false, false
	for _, f := range changed {
		switch {
		case strings.HasSuffix(f, ".go"):
			hasGo = true
		case strings.HasSuffix(f, ".py"):
			hasPy = true
		case strings.HasSuffix(f, ".js") || strings.HasSuffix(f, ".ts") || strings.HasSuffix(f, ".tsx") || strings.HasSuffix(f, ".jsx"):
			hasJS = true
		}
	}
	var langs []string
	if hasGo { langs = append(langs, "go") }
	if hasPy { langs = append(langs, "python") }
	if hasJS { langs = append(langs, "js") }
	return langs
}

func goSteps(root string, changed []string, cfg Config) []Step {
	// build + vet are always module-wide. `go build ./...` is cheap (compile
	// cache) and is the reliable signal that catches a DEPENDENT package the edit
	// just broke — narrowing to the changed package alone (e.g. a signature change)
	// would let an importer break slip through as a false success. Only the test
	// step is scoped: a pre-existing/flaky test in an unrelated package shouldn't
	// be blamed on this edit (and the feedback already caveats test failures).
	steps := []Step{
		{Name: "build", Dir: root, Args: []string{"go", "build", "./..."}},
		{Name: "vet", Dir: root, Args: []string{"go", "vet", "./..."}},
	}
	if cfg.RunTests && len(changed) > 0 {
		if cfg.Scope == "all" {
			steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"go", "test", "./..."}})
		} else {
			for _, pkg := range changedPkgs(root, changed) {
				steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"go", "test", pkg}})
			}
		}
	}
	return steps
}

func pythonSteps(root string, changed []string, cfg Config) []Step {
	steps := []Step{
		{Name: "lint", Dir: root, Args: []string{"ruff", "check", "."}},
	}
	if cfg.RunTests && len(changed) > 0 {
		if cfg.Scope == "all" {
			steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"pytest", "-q"}})
		} else {
			// Run pytest only on the specific test file
			for _, f := range changed {
				if strings.HasSuffix(f, ".py") {
					testFile := strings.TrimSuffix(f, ".py") + "_test.py"
					if _, err := filepath.Glob(filepath.Join(root, testFile)); err == nil {
						steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"pytest", "-q", testFile}})
					}
				}
			}
		}
	}
	return steps
}

func jsSteps(root string, changed []string, cfg Config) []Step {
	steps := []Step{
		{Name: "typecheck", Dir: root, Args: []string{"npx", "tsc", "--noEmit"}},
	}
	if cfg.RunTests && len(changed) > 0 {
		if cfg.Scope == "all" {
			steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"npx", "jest", "--passWithNoTests"}})
		} else {
			for _, f := range changed {
				if strings.HasSuffix(f, ".ts") || strings.HasSuffix(f, ".tsx") || strings.HasSuffix(f, ".js") || strings.HasSuffix(f, ".jsx") {
					steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"npx", "jest", "--passWithNoTests", f}})
				}
			}
		}
	}
	return steps
}

// changedPkgs returns the sorted, deduplicated list of Go package directories
// relative to root for the .go files in changed. Non-.go files are ignored.
func changedPkgs(root string, changed []string) []string {
	seen := map[string]bool{}
	var pkgs []string
	for _, f := range changed {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		dir := filepath.Dir(f)
		pkg := "./" + dir
		if dir == "." {
			pkg = "."
		}
		if !seen[pkg] {
			seen[pkg] = true
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}
