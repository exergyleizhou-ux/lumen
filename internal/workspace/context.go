// Package workspace carries immutable per-run workspace boundaries and
// environment overlays through context.Context.
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Backend resolves paths against one immutable workspace boundary.
type Backend interface {
	Root() string
	Resolve(path string, allowMissing bool) (string, error)
}

// Context is the workspace identity and execution environment for one run.
// Construct it with NewLocal so Root, Env, and Backend agree.
type Context struct {
	WorkspaceID string
	Root        string
	UserID      string
	Env         map[string]string
	Backend     Backend
}

type localBackend struct{ root string }

type contextKey struct{}

// NewLocal creates a workspace backed by a local directory.
func NewLocal(workspaceID, root, userID string, env map[string]string) (Context, error) {
	if strings.TrimSpace(root) == "" {
		return Context{}, fmt.Errorf("workspace root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Context{}, fmt.Errorf("workspace root: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Context{}, fmt.Errorf("workspace root: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return Context{}, fmt.Errorf("workspace root: %w", err)
	}
	if !info.IsDir() {
		return Context{}, fmt.Errorf("workspace root %s is not a directory", canonical)
	}
	canonical = filepath.Clean(canonical)
	return Context{
		WorkspaceID: workspaceID,
		Root:        canonical,
		UserID:      userID,
		Env:         cloneEnv(env),
		Backend:     &localBackend{root: canonical},
	}, nil
}

// WithContext attaches an immutable copy of ws to parent.
func WithContext(parent context.Context, ws Context) context.Context {
	return context.WithValue(parent, contextKey{}, cloneContext(ws))
}

// FromContext returns a defensive copy of the run workspace.
func FromContext(ctx context.Context) (Context, bool) {
	if ctx == nil {
		return Context{}, false
	}
	ws, ok := ctx.Value(contextKey{}).(Context)
	if !ok || ws.Backend == nil || ws.Root == "" {
		return Context{}, false
	}
	return cloneContext(ws), true
}

// Environment applies the workspace overlay to base and returns a stable list.
func (ws Context) Environment(base []string) []string {
	values := make(map[string]string, len(base)+len(ws.Env))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			values[key] = value
		}
	}
	for key, value := range ws.Env {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

func (b *localBackend) Root() string { return b.root }

func (b *localBackend) Resolve(path string, allowMissing bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(b.root, candidate)
	}
	candidate = filepath.Clean(candidate)

	resolved, err := resolve(candidate, allowMissing)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(b.root, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve workspace boundary: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %s escapes workspace boundary %s", resolved, b.root)
	}
	return resolved, nil
}

func resolve(path string, allowMissing bool) (string, error) {
	if !allowMissing {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", path, err)
		}
		return filepath.Clean(resolved), nil
	}

	cur := path
	missing := make([]string, 0, 4)
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("resolve %s: no existing ancestor", path)
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}

func cloneContext(ws Context) Context {
	ws.Env = cloneEnv(ws.Env)
	return ws
}

func cloneEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	copy := make(map[string]string, len(env))
	for key, value := range env {
		copy[key] = value
	}
	return copy
}
