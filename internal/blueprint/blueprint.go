// Package blueprint provides a dependency-injection-style application
// blueprint system. Define components, their dependencies, and lifecycle
// hooks. The blueprint resolves the DAG and instantiates components in
// the correct order with automatic cleanup.
package blueprint

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Component is a named unit with dependencies and lifecycle.
type Component struct {
	Name              string                          `json:"name"`
	Provides          []string                        `json:"provides"`
	DependsOn         []string                        `json:"depends_on"`
	OptionalDependsOn []string                        `json:"optional_depends_on,omitempty"`
	Factory           func(ctx *Context) (any, error) `json:"-"`
	Cleanup           func(instance any) error        `json:"-"`
	Priority          int                             `json:"priority"`
}

// Context provides dependency resolution during construction.
type Context struct {
	instances map[string]any
	mu        sync.RWMutex
	startTime time.Time
}

// Get retrieves a resolved dependency.
func (c *Context) Get(name string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.instances[name]
	return v, ok
}

// GetString retrieves a string dependency.
func (c *Context) GetString(name string) string {
	if v, ok := c.Get(name); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Blueprint is a set of components to wire together.
type Blueprint struct {
	Name       string       `json:"name"`
	Components []*Component `json:"components"`
}

// Resolver resolves component dependencies into a construction plan.
type Resolver struct {
	mu         sync.Mutex
	components map[string]*Component
	blueprints map[string]*Blueprint
}

// NewResolver creates a blueprint resolver.
func NewResolver() *Resolver {
	return &Resolver{components: map[string]*Component{}, blueprints: map[string]*Blueprint{}}
}

// Register adds a component.
func (r *Resolver) Register(c *Component) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.components[c.Name] = c
}

// RegisterBlueprint adds a blueprint.
func (r *Resolver) RegisterBlueprint(bp *Blueprint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blueprints[bp.Name] = bp
	for _, c := range bp.Components {
		r.components[c.Name] = c
	}
}

// Resolve determines the instantiation order for a set of component names.
func (r *Resolver) Resolve(names []string) ([]*Component, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Build subgraph
	included := map[string]bool{}
	var collect func(name string) error
	collect = func(name string) error {
		c, ok := r.components[name]
		if !ok {
			return fmt.Errorf("component %q not found", name)
		}
		if included[name] {
			return nil
		}
		included[name] = true
		for _, dep := range c.DependsOn {
			if err := collect(dep); err != nil {
				// Check if optional
				isOptional := false
				for _, od := range c.OptionalDependsOn {
					if od == dep {
						isOptional = true
						break
					}
				}
				if !isOptional {
					return err
				}
			}
		}
		return nil
	}
	for _, name := range names {
		if err := collect(name); err != nil {
			return nil, err
		}
	}

	// Topological sort
	inDegree := map[string]int{}
	for name := range included {
		inDegree[name] = 0
	}
	for name := range included {
		c := r.components[name]
		for _, dep := range c.DependsOn {
			if included[dep] {
				inDegree[name]++
			}
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue) // Deterministic order

	var order []*Component
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, r.components[name])

		for otherName := range included {
			c := r.components[otherName]
			for _, dep := range c.DependsOn {
				if dep == name {
					inDegree[otherName]--
					if inDegree[otherName] == 0 {
						queue = append(queue, otherName)
					}
				}
			}
		}
		sort.Strings(queue)
	}

	if len(order) != len(included) {
		return nil, fmt.Errorf("circular dependency detected")
	}
	return order, nil
}

// Build instantiates all components in the given list.
func (r *Resolver) Build(names []string) (*Context, func(), error) {
	order, err := r.Resolve(names)
	if err != nil {
		return nil, nil, err
	}

	ctx := &Context{instances: map[string]any{}, startTime: time.Now()}

	// Forward pass: create instances
	for _, c := range order {
		instance, err := c.Factory(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("component %q: %w", c.Name, err)
		}
		ctx.mu.Lock()
		ctx.instances[c.Name] = instance
		for _, p := range c.Provides {
			ctx.instances[p] = instance
		}
		ctx.mu.Unlock()
	}

	// Cleanup function (reverse order)
	cleanup := func() {
		for i := len(order) - 1; i >= 0; i-- {
			c := order[i]
			if c.Cleanup != nil {
				if inst, ok := ctx.instances[c.Name]; ok {
					_ = c.Cleanup(inst)
				}
			}
		}
	}

	return ctx, cleanup, nil
}

// Validate checks a blueprint for common issues.
func (r *Resolver) Validate(bp *Blueprint) []error {
	var errs []error
	names := map[string]bool{}
	for _, c := range bp.Components {
		if c.Name == "" {
			errs = append(errs, fmt.Errorf("component with empty name"))
		}
		if names[c.Name] {
			errs = append(errs, fmt.Errorf("duplicate component %q", c.Name))
		}
		names[c.Name] = true
		if c.Factory == nil {
			errs = append(errs, fmt.Errorf("%q: no factory function", c.Name))
		}
	}
	// Check deps exist
	for _, c := range bp.Components {
		for _, dep := range c.DependsOn {
			if !names[dep] {
				optional := false
				for _, od := range c.OptionalDependsOn {
					if od == dep {
						optional = true
						break
					}
				}
				if !optional {
					errs = append(errs, fmt.Errorf("%q: depends on unknown %q", c.Name, dep))
				}
			}
		}
	}
	return errs
}

// FormatBlueprint formats a blueprint for display.
func FormatBlueprint(bp *Blueprint) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Blueprint: %s (%d components)\n%s\n\n", bp.Name, len(bp.Components), strings.Repeat("─", 50))
	sort.Slice(bp.Components, func(i, j int) bool { return bp.Components[i].Name < bp.Components[j].Name })
	for _, c := range bp.Components {
		fmt.Fprintf(&sb, "  ● %s", c.Name)
		if len(c.DependsOn) > 0 {
			fmt.Fprintf(&sb, " → [%s]", strings.Join(c.DependsOn, ", "))
		}
		if len(c.Provides) > 0 {
			fmt.Fprintf(&sb, " provides [%s]", strings.Join(c.Provides, ", "))
		}
		sb.WriteByte('\n')
	}
	// Try to resolve and show order
	r := NewResolver()
	r.RegisterBlueprint(bp)
	order, err := r.Resolve(nil)
	if err == nil && len(order) > 0 {
		sb.WriteString("\nBuild Order:\n")
		for i, c := range order {
			fmt.Fprintf(&sb, "  %d. %s\n", i+1, c.Name)
		}
	}
	return sb.String()
}

// ── Standard Blueprints ────────────────────────────────────

// DefaultAgentBlueprint returns a common agent wiring blueprint.
func DefaultAgentBlueprint() *Blueprint {
	return &Blueprint{
		Name: "agent.default",
		Components: []*Component{
			{Name: "config", Provides: []string{"app.config"}, Factory: func(ctx *Context) (any, error) {
				return map[string]string{"version": "1.0"}, nil
			}},
			{Name: "logger", DependsOn: []string{"config"}, Factory: func(ctx *Context) (any, error) {
				return "logger-instance", nil
			}},
			{Name: "store", DependsOn: []string{"config"}, Factory: func(ctx *Context) (any, error) {
				return "store-instance", nil
			}},
			{Name: "agent", DependsOn: []string{"logger", "store"}, Factory: func(ctx *Context) (any, error) {
				return "agent-instance", nil
			}},
		},
	}
}
