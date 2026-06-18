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

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
