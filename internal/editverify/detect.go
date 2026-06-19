package editverify

import (
	"path/filepath"
	"strings"
)

// Detect builds the ordered verification plan for the given changed files.
//
// Language detection is automatic based on file extensions:
//   - .go files → go build + go vet + (optional) go test
//   - .py files → ruff check + pytest (only with a sibling <name>_test.py)
//   - .js/.ts files → tsc --noEmit (local tsc + tsconfig) + jest (local jest)
//   - Mixed files → steps for each detected language
//
// Uninstalled tools are skipped at run time, never reported as a failure.
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

	// If a change touched no recognized source file, fall back to Go's build/vet
	// ONLY when root is actually a Go module. Running `go build` in a non-Go
	// project (e.g. a pure Python/JS repo) would be a guaranteed spurious failure
	// that triggers a bogus self-repair cycle, so otherwise we run nothing.
	if len(steps) == 0 {
		for _, l := range projectLanguages(root) {
			if l == "go" {
				steps = goSteps(root, changed, cfg)
				break
			}
		}
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
		case strings.HasSuffix(f, ".js") || strings.HasSuffix(f, ".ts") || strings.HasSuffix(f, ".tsx") || strings.HasSuffix(f, ".jsx") ||
			strings.HasSuffix(f, ".mjs") || strings.HasSuffix(f, ".cjs") || strings.HasSuffix(f, ".mts") || strings.HasSuffix(f, ".cts"):
			hasJS = true
		}
	}
	var langs []string
	if hasGo {
		langs = append(langs, "go")
	}
	if hasPy {
		langs = append(langs, "python")
	}
	if hasJS {
		langs = append(langs, "js")
	}
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
			// Schedule pytest on the test file(s) for each changed .py, covering
			// both conventions (test_<name>.py and <name>_test.py) and a changed
			// file that is itself a test. Only existing files are scheduled —
			// `pytest <missing>` errors "file not found" and would false-fail.
			seen := map[string]bool{}
			for _, f := range changed {
				if !strings.HasSuffix(f, ".py") {
					continue
				}
				for _, tf := range pytestTargets(root, f) {
					if !seen[tf] {
						seen[tf] = true
						steps = append(steps, Step{Name: "test", Dir: root, Args: []string{"pytest", "-q", tf}})
					}
				}
			}
		}
	}
	return steps
}

// pytestTargets returns the existing pytest target files for a changed .py file:
// the file itself when it is a test (test_*.py or *_test.py), otherwise its
// sibling tests under both common conventions (test_<name>.py, <name>_test.py).
func pytestTargets(root, f string) []string {
	base := filepath.Base(f)
	if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
		if fileExists(filepath.Join(root, f)) {
			return []string{f}
		}
		return nil
	}
	dir := filepath.Dir(f)
	name := strings.TrimSuffix(base, ".py")
	var out []string
	for _, cand := range []string{
		filepath.Join(dir, "test_"+name+".py"),
		filepath.Join(dir, name+"_test.py"),
	} {
		if fileExists(filepath.Join(root, cand)) {
			out = append(out, cand)
		}
	}
	return out
}

func jsSteps(root string, changed []string, cfg Config) []Step {
	var steps []Step
	// Typecheck only when TypeScript is installed locally AND a tsconfig.json
	// exists. Scheduling `npx tsc` unconditionally would auto-download tsc from
	// the network (or fail in a plain-JS repo with no tsconfig) — a false verify
	// failure. Run the resolved binary, never npx.
	if tsc := nodeBin(root, "tsc"); tsc != "" && fileExists(filepath.Join(root, "tsconfig.json")) {
		steps = append(steps, Step{Name: "typecheck", Dir: root, Args: []string{tsc, "--noEmit"}})
	}
	if cfg.RunTests && len(changed) > 0 {
		// Run jest only when it is installed locally (resolved binary, not npx).
		if jest := nodeBin(root, "jest"); jest != "" {
			if cfg.Scope == "all" {
				steps = append(steps, Step{Name: "test", Dir: root, Args: []string{jest, "--passWithNoTests"}})
			} else {
				for _, f := range changed {
					if strings.HasSuffix(f, ".ts") || strings.HasSuffix(f, ".tsx") || strings.HasSuffix(f, ".js") || strings.HasSuffix(f, ".jsx") ||
						strings.HasSuffix(f, ".mjs") || strings.HasSuffix(f, ".cjs") || strings.HasSuffix(f, ".mts") || strings.HasSuffix(f, ".cts") {
						steps = append(steps, Step{Name: "test", Dir: root, Args: []string{jest, "--passWithNoTests", f}})
					}
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
