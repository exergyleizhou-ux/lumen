package runtime

import (
	"os"
	"path/filepath"

	"lumen/internal/science/paths"
)

// CondaEnv returns the dataDir/bin and dataDir/conda paths for the lab agent's
// bash execution environment.
func CondaEnv(sciDir string) []string {
	dataDir := paths.DataDir(sciDir)
	bin := filepath.Join(dataDir, "bin")
	condaBin := filepath.Join(dataDir, "conda", "bin")
	var paths []string
	for _, p := range []string{bin, condaBin} {
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	// Also check conda envs/operon-mcp/bin
	operon := filepath.Join(dataDir, "conda", "envs", "operon-mcp", "bin")
	if _, err := os.Stat(operon); err == nil {
		paths = append(paths, operon)
	}
	return paths
}

// LabPath returns a PATH value with Lab runtimes prepended. It never mutates
// the process environment; callers place the result in a run workspace overlay.
func LabPath(sciDir, basePATH string) string {
	paths := CondaEnv(sciDir)
	if len(paths) == 0 {
		return basePATH
	}
	cur := basePATH
	for i := len(paths) - 1; i >= 0; i-- {
		if cur == "" {
			cur = paths[i]
		} else {
			cur = paths[i] + string(os.PathListSeparator) + cur
		}
	}
	return cur
}
