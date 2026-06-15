// Package di provides a lightweight dependency injection container with
// constructor-based injection, lifecycle management (Init/Start/Stop),
// and named bindings. Inspired by uber-go/fx but minimal.
package di

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Container holds registered services and resolves dependencies.
type Container struct {
	mu        sync.RWMutex
	bindings  map[string]*binding
	instances map[string]any
	started   []string // ordered by startup
}

type binding struct {
	name      string
	ctor      any      // constructor function
	deps      []string // dependency names
	instance  any      // resolved singleton
	resolved  bool
	lifecycle Lifecycle
}

// Lifecycle allows services to execute Init/Start/Stop hooks.
type Lifecycle interface {
	Init() error
	Start() error
	Stop() error
}

// NewContainer creates a dependency injection container.
func NewContainer() *Container {
	return &Container{bindings: map[string]*binding{}, instances: map[string]any{}}
}

// Register adds a service constructor with its dependency names.
// The constructor must return (service, error).
func (c *Container) Register(name string, ctor any, deps ...string) *Container {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[name] = &binding{name: name, ctor: ctor, deps: deps}
	return c
}

// Get resolves a named service, constructing it and its dependencies.
// Returns the resolved instance or an error if a dependency is missing.
func (c *Container) Get(name string) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resolve(name, map[string]bool{})
}

func (c *Container) resolve(name string, resolving map[string]bool) (any, error) {
	if resolving[name] {
		return nil, fmt.Errorf("circular dependency: %s", name)
	}

	b, ok := c.bindings[name]
	if !ok {
		return nil, fmt.Errorf("binding not found: %s", name)
	}

	if b.resolved {
		return b.instance, nil
	}

	resolving[name] = true
	defer func() { delete(resolving, name) }()

	// Resolve dependencies first
	var depValues []reflect.Value
	for _, dep := range b.deps {
		val, err := c.resolve(dep, resolving)
		if err != nil {
			return nil, fmt.Errorf("%s → %w", name, err)
		}
		depValues = append(depValues, reflect.ValueOf(val))
	}

	// Call constructor
	ctorVal := reflect.ValueOf(b.ctor)
	if ctorVal.Kind() != reflect.Func {
		return nil, fmt.Errorf("constructor for %s is not a function", name)
	}

	results := ctorVal.Call(depValues)
	if len(results) != 2 {
		return nil, fmt.Errorf("constructor for %s must return (service, error)", name)
	}

	if !results[1].IsNil() {
		return nil, fmt.Errorf("construct %s: %w", name, results[1].Interface().(error))
	}

	instance := results[0].Interface()
	b.instance = instance
	b.resolved = true
	c.instances[name] = instance
	return instance, nil
}

// Start calls Init then Start on all registered services in dependency order.
func (c *Container) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Resolve all bindings to build dependency graph
	for name := range c.bindings {
		if _, err := c.resolve(name, map[string]bool{}); err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}
	}

	// Topological sort by dependencies
	order := c.topologicalSort()

	// Init phase
	for _, name := range order {
		instance := c.instances[name]
		if lc, ok := instance.(Lifecycle); ok {
			if err := lc.Init(); err != nil {
				return fmt.Errorf("init %s: %w", name, err)
			}
		}
	}

	// Start phase
	for _, name := range order {
		instance := c.instances[name]
		if lc, ok := instance.(Lifecycle); ok {
			if err := lc.Start(); err != nil {
				return fmt.Errorf("start %s: %w", name, err)
			}
			c.started = append(c.started, name)
		}
	}
	return nil
}

// Stop calls Stop on all started services in reverse order.
func (c *Container) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := len(c.started) - 1; i >= 0; i-- {
		name := c.started[i]
		if instance, ok := c.instances[name]; ok {
			if lc, ok := instance.(Lifecycle); ok {
				if err := lc.Stop(); err != nil {
					return fmt.Errorf("stop %s: %w", name, err)
				}
			}
		}
	}
	return nil
}

func (c *Container) topologicalSort() []string {
	visited := map[string]bool{}
	var order []string

	var visit func(string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		if b, ok := c.bindings[name]; ok {
			for _, dep := range b.deps {
				visit(dep)
			}
		}
		order = append(order, name)
	}

	for name := range c.bindings {
		visit(name)
	}
	return order
}

// ListBindings returns all registered binding names.
func (c *Container) ListBindings() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.bindings))
	for n := range c.bindings {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// MustGet is like Get but panics on error.
func (c *Container) MustGet(name string) any {
	v, err := c.Get(name)
	if err != nil {
		panic(err)
	}
	return v
}

// FormatBindings returns a human-readable list of bindings.
func (c *Container) FormatBindings() string {
	var sb strings.Builder
	sb.WriteString("Registered bindings:\n")
	for _, name := range c.ListBindings() {
		b := c.bindings[name]
		fmt.Fprintf(&sb, "  %s", name)
		if len(b.deps) > 0 {
			fmt.Fprintf(&sb, " → [%s]", strings.Join(b.deps, ", "))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
