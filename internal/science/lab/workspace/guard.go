package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Walk traverses without exposing host absolute paths. Platform primitives
// re-open every component with no-follow semantics on Unix.
func (g *Guard) Walk(rel string, fn func(string, fs.FileInfo, error) error) error {
	if rel == "" {
		rel = "."
	}
	st, err := g.Stat(rel)
	if err != nil {
		return fn(rel, nil, err)
	}
	if err = fn(filepath.ToSlash(rel), st, nil); err != nil {
		if err == filepath.SkipDir {
			return nil
		}
		return err
	}
	if !st.IsDir() {
		return nil
	}
	entries, err := g.ReadDir(rel)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := e.Name()
		if rel != "." {
			child = filepath.Join(rel, child)
		}
		if err = g.Walk(child, fn); err != nil {
			return err
		}
	}
	return nil
}

// Guard ensures paths stay within a project workspace root.
type Guard struct {
	root string
}

// NewGuard creates a workspace path guard.
func NewGuard(root string) (*Guard, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Guard{root: abs}, nil
}

// Resolve validates and resolves a relative path inside the workspace.
func (g *Guard) Resolve(rel string) (string, error) {
	if g == nil || g.root == "" {
		return "", fmt.Errorf("workspace guard not configured")
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return g.root, nil
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths not allowed")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	abs := filepath.Join(g.root, clean)
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, g.root+string(os.PathSeparator)) && abs != g.root {
		return "", fmt.Errorf("path escapes workspace")
	}
	// Reject a symlink in any existing component, not only at the leaf. A
	// writable endpoint may target a missing leaf below a symlinked directory.
	current := g.root
	if relFromRoot, err := filepath.Rel(g.root, abs); err == nil && relFromRoot != "." {
		for _, component := range strings.Split(relFromRoot, string(os.PathSeparator)) {
			current = filepath.Join(current, component)
			fi, err := os.Lstat(current)
			if err != nil {
				if os.IsNotExist(err) {
					break
				}
				return "", err
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("symlinks not allowed")
			}
		}
	}
	return abs, nil
}
