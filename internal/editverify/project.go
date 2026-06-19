package editverify

import (
	"os"
	"path/filepath"
)

// projectLanguages returns the languages whose project markers are present at
// root: "go" (go.mod), "js" (package.json), "python" (pyproject.toml / setup.py
// / setup.cfg / requirements.txt). Used to gate activation and to pick a safe
// fallback when a change touches no recognized source file.
func projectLanguages(root string) []string {
	var langs []string
	if fileExists(filepath.Join(root, "go.mod")) {
		langs = append(langs, "go")
	}
	if fileExists(filepath.Join(root, "package.json")) {
		langs = append(langs, "js")
	}
	for _, m := range []string{"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt"} {
		if fileExists(filepath.Join(root, m)) {
			langs = append(langs, "python")
			break
		}
	}
	return langs
}

// IsSupportedProject reports whether root is a recognized Go/JS/Python project —
// i.e. whether the verify-after-edit loop should activate there.
func IsSupportedProject(root string) bool {
	return len(projectLanguages(root)) > 0
}

// FindProjectRoot walks upward from start (bounded) to the nearest directory
// containing a recognized project marker, so the verify loop activates even when
// lumen runs from a monorepo subdirectory. Returns "" if none is found.
func FindProjectRoot(start string) string {
	dir := start
	for i := 0; i < 16; i++ {
		if IsSupportedProject(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// nodeBin returns the path to a project-local node tool
// (root/node_modules/.bin/<tool>), or "" if absent. We deliberately do NOT fall
// back to a global install or `npx`: a verify loop must use the project's pinned
// toolchain and must never auto-fetch a tool from the network.
func nodeBin(root, tool string) string {
	p := filepath.Join(root, "node_modules", ".bin", tool)
	if fileExists(p) {
		return p
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
