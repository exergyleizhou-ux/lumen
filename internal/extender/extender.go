// Package extender provides a plugin extension system for Lumen agents.
// It supports loading external modules, registering custom tools, and
// managing extension lifecycles with hot-reload capability.
package extender

import ("fmt";"sort";"strings";"sync";"time")

// Extension is a loadable plugin module.
type Extension struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	Path        string            `json:"path"`
	Enabled     bool              `json:"enabled"`
	LoadedAt    time.Time         `json:"loaded_at,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	InitFn      func() error      `json:"-"`
	CloseFn     func() error      `json:"-"`
	Tools       []string          `json:"tools,omitempty"`
}

// Registry manages loaded extensions.
type Registry struct {
	mu          sync.RWMutex
	extensions  map[string]*Extension
	depOrder    []string
}

// NewRegistry creates an extension registry.
func NewRegistry() *Registry {
	return &Registry{extensions: map[string]*Extension{}}
}

// Register adds an extension.
func (r *Registry) Register(ext *Extension) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if _, ok := r.extensions[ext.Name]; ok {
		return fmt.Errorf("extension %q already registered", ext.Name)
	}
	r.extensions[ext.Name] = ext
	return nil
}

// Enable loads and activates an extension.
func (r *Registry) Enable(name string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	ext, ok := r.extensions[name]; if !ok { return fmt.Errorf("extension %q not found", name) }
	if ext.InitFn != nil {
		if err := ext.InitFn(); err != nil { return fmt.Errorf("init %q: %w", name, err) }
	}
	ext.Enabled = true
	ext.LoadedAt = time.Now()
	return nil
}

// Disable deactivates an extension.
func (r *Registry) Disable(name string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	ext, ok := r.extensions[name]; if !ok { return fmt.Errorf("extension %q not found", name) }
	if ext.Enabled && ext.CloseFn != nil {
		if err := ext.CloseFn(); err != nil { return fmt.Errorf("close %q: %w", name, err) }
	}
	ext.Enabled = false
	return nil
}

// Reload disables and re-enables an extension.
func (r *Registry) Reload(name string) error {
	if err := r.Disable(name); err != nil { return err }
	return r.Enable(name)
}

// Get returns an extension by name.
func (r *Registry) Get(name string) (*Extension, bool) {
	r.mu.RLock(); defer r.mu.RUnlock()
	ext, ok := r.extensions[name]; return ext, ok
}

// List returns all extensions.
func (r *Registry) List() []*Extension {
	r.mu.RLock(); defer r.mu.RUnlock()
	var out []*Extension
	for _, e := range r.extensions { out = append(out, e) }
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Enabled returns enabled extensions.
func (r *Registry) Enabled() []*Extension {
	r.mu.RLock(); defer r.mu.RUnlock()
	var out []*Extension
	for _, e := range r.extensions { if e.Enabled { out = append(out, e) } }
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Tools returns all tools provided by enabled extensions.
func (r *Registry) Tools() map[string]*Extension {
	r.mu.RLock(); defer r.mu.RUnlock()
	out := map[string]*Extension{}
	for _, e := range r.extensions {
		if e.Enabled {
			for _, t := range e.Tools { out[t] = e }
		}
	}
	return out
}

// FormatList formats the extension registry.
func (r *Registry) FormatList() string {
	r.mu.RLock(); defer r.mu.RUnlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Extensions (%d):\n%s\n\n", len(r.extensions), strings.Repeat("─", 40))
	for _, e := range r.List() {
		icon := "⬜"; if e.Enabled { icon = "✅" }
		fmt.Fprintf(&sb, "  %s %-25s v%-10s", icon, e.Name, e.Version)
		if e.Enabled { fmt.Fprintf(&sb, " loaded at %s", e.LoadedAt.Format(time.RFC3339)) }
		sb.WriteByte('\n')
		if len(e.Tools) > 0 { fmt.Fprintf(&sb, "     tools: %s\n", strings.Join(e.Tools, ", ")) }
	}
	return sb.String()
}

// ── Dependency Resolution ─────────────────────────────────

// ResolveDeps sorts extensions by declared dependencies.
func (r *Registry) ResolveDeps(deps map[string][]string) ([]string, error) {
	r.mu.RLock(); defer r.mu.RUnlock()

	inDegree := map[string]int{}
	for name := range deps { inDegree[name] = 0 }
	for name, ds := range deps { for range ds { inDegree[name]++ } }

	var queue []string
	for name, deg := range inDegree { if deg == 0 { queue = append(queue, name) } }

	var order []string
	for len(queue) > 0 {
		name := queue[0]; queue = queue[1:]
		order = append(order, name)
		for oname, odeps := range deps {
			for _, d := range odeps {
				if d == name { inDegree[oname]--; if inDegree[oname] == 0 { queue = append(queue, oname) } }
			}
		}
	}
	if len(order) != len(deps) { return nil, fmt.Errorf("circular dependency detected") }
	return order, nil
}
