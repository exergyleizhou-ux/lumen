package editverify

import (
	"path/filepath"
	"strings"
)

// Detect builds the ordered verification plan for the given changed files.
//
// Rules (in priority order):
//  1. If cfg.Command is non-empty, the whole plan is a single "custom" step.
//  2. Otherwise: build + vet always.
//  3. If cfg.RunTests: append test steps.
//     - cfg.Scope=="all" → single ["go","test","./..."]
//     - Otherwise, for each distinct Go package directory among .go files,
//       append ["go","test","./<pkgDir>"]; duplicates are deduplicated.
//     - No .go files changed → skip tests.
//
// Returns at least build+vet even when changed is empty.
func Detect(root string, changed []string, cfg Config) []Step {
	// Rule 1: custom override
	if cfg.Command != "" {
		return []Step{{Name: "custom", Dir: root, Args: []string{"sh", "-c", cfg.Command}}}
	}

	steps := []Step{
		{Name: "build", Dir: root, Args: []string{"go", "build", "./..."}},
		{Name: "vet", Dir: root, Args: []string{"go", "vet", "./..."}},
	}

	// Rule 3: test
	if cfg.RunTests && len(changed) > 0 {
		if cfg.Scope == "all" {
			steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"go", "test", "./..."}})
		} else {
			pkgs := changedPkgs(root, changed)
			for _, pkg := range pkgs {
				steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"go", "test", pkg}})
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
