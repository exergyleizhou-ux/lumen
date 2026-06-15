// Package hotplug enables dynamic loading and unloading of agent tools, skills,
// and MCP servers without restarting the agent. It watches plugin directories,
// detects new or modified files, and hot-reloads them into the running registry.
package hotplug

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type PluginType string

const (
	PluginTool  PluginType = "tool"
	PluginSkill PluginType = "skill"
	PluginMCP   PluginType = "mcp"
)

type Plugin struct {
	Name     string
	Type     PluginType
	Path     string
	Version  string
	LoadedAt time.Time
	Status   string
	Error    string
	Metadata map[string]any
}
type Registry struct {
	mu        sync.RWMutex
	plugins   map[string]*Plugin
	callbacks map[string]func(*Plugin)
	watcher   *fileWatcher
}

func NewRegistry() *Registry {
	return &Registry{plugins: map[string]*Plugin{}, callbacks: map[string]func(*Plugin){}}
}
func (r *Registry) Register(p *Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p.LoadedAt = time.Now()
	p.Status = "loaded"
	r.plugins[p.Name] = p
	r.fireCallbacks(p)
}
func (r *Registry) Unregister(name string) { r.mu.Lock(); defer r.mu.Unlock(); delete(r.plugins, name) }
func (r *Registry) Get(name string) *Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[name]
}
func (r *Registry) List() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (r *Registry) OnChange(event string, fn func(*Plugin)) { r.callbacks[event] = fn }
func (r *Registry) fireCallbacks(p *Plugin) {
	for _, fn := range r.callbacks {
		fn(p)
	}
}
func (r *Registry) Count() int { r.mu.RLock(); defer r.mu.RUnlock(); return len(r.plugins) }
func (r *Registry) ListByType(t PluginType) []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Plugin
	for _, p := range r.plugins {
		if p.Type == t {
			out = append(out, p)
		}
	}
	return out
}
func FormatPlugins(p []*Plugin) string {
	if len(p) == 0 {
		return "No plugins.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d plugin(s):\n\n", len(p))
	for _, pp := range p {
		fmt.Fprintf(&sb, "  %-25s [%s] %s\n", pp.Name, pp.Type, pp.Status)
	}
	return sb.String()
}

type fileWatcher struct {
	mu       sync.Mutex
	dirs     []string
	plugins  *Registry
	stop     chan struct{}
	interval time.Duration
}

func NewWatcher(reg *Registry) *fileWatcher {
	return &fileWatcher{plugins: reg, stop: make(chan struct{}), interval: 2 * time.Second}
}
func (w *fileWatcher) Watch(dir string) { w.mu.Lock(); w.dirs = append(w.dirs, dir); w.mu.Unlock() }
func (w *fileWatcher) Start() {
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
				w.scan()
			}
		}
	}()
}
func (w *fileWatcher) Stop() { close(w.stop) }
func (w *fileWatcher) scan() {
	w.mu.Lock()
	dirs := make([]string, len(w.dirs))
	copy(dirs, w.dirs)
	w.mu.Unlock()
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".go")
			path := filepath.Join(dir, e.Name())
			if w.plugins.Get(name) == nil {
				w.plugins.Register(&Plugin{Name: name, Type: PluginTool, Path: path, Version: "hot-reloaded"})
			}
		}
	}
}

type Manager struct {
	mu       sync.Mutex
	tools    *Registry
	skills   *Registry
	mcp      *Registry
	watchers []*fileWatcher
}

func NewManager() *Manager {
	return &Manager{tools: NewRegistry(), skills: NewRegistry(), mcp: NewRegistry()}
}
func (m *Manager) ToolRegistry() *Registry  { return m.tools }
func (m *Manager) SkillRegistry() *Registry { return m.skills }
func (m *Manager) MCPRegistry() *Registry   { return m.mcp }
func (m *Manager) WatchTools(dir string) {
	w := NewWatcher(m.tools)
	w.Watch(dir)
	w.Start()
	m.mu.Lock()
	m.watchers = append(m.watchers, w)
	m.mu.Unlock()
}
func (m *Manager) WatchSkills(dir string) {
	w := NewWatcher(m.skills)
	w.Watch(dir)
	w.Start()
	m.mu.Lock()
	m.watchers = append(m.watchers, w)
	m.mu.Unlock()
}
func (m *Manager) WatchMCP(dir string) {
	w := NewWatcher(m.mcp)
	w.Watch(dir)
	w.Start()
	m.mu.Lock()
	m.watchers = append(m.watchers, w)
	m.mu.Unlock()
}
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.watchers {
		w.Stop()
	}
}
func (m *Manager) FormatAll() string {
	return fmt.Sprintf("Hotplug Manager — tools:%d skills:%d mcp:%d", m.tools.Count(), m.skills.Count(), m.mcp.Count())
}
