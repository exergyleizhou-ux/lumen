// Package marketplace provides a skill and MCP server marketplace with
// installation, update, and discovery capabilities. It supports indexing
// local and remote sources, version tracking, and auto-updates.
package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Item is one installable item in the marketplace.
type Item struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // "skill" or "mcp"
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Source      string   `json:"source"` // URL or "local"
	Tags        []string `json:"tags"`
	Installed   bool     `json:"installed"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

// Store manages the marketplace catalog.
type Store struct {
	mu    sync.RWMutex
	items map[string]*Item
	dir   string
}

// NewStore creates a marketplace store.
func NewStore(dir string) (*Store, error) {
	os.MkdirAll(dir, 0o755)
	s := &Store{items: map[string]*Item{}, dir: dir}
	s.loadCatalog()
	return s, nil
}

func (s *Store) catalogPath() string { return filepath.Join(s.dir, "catalog.json") }

func (s *Store) loadCatalog() {
	data, err := os.ReadFile(s.catalogPath())
	if err != nil { return }
	var items []*Item
	if json.Unmarshal(data, &items) != nil { return }
	s.mu.Lock(); defer s.mu.Unlock()
	for _, item := range items { s.items[item.Name] = item }
}

func (s *Store) saveCatalog() {
	s.mu.RLock()
	items := make([]*Item, 0, len(s.items))
	for _, item := range s.items { items = append(items, item) }
	s.mu.RUnlock()
	data, _ := json.MarshalIndent(items, "", "  ")
	os.WriteFile(s.catalogPath(), data, 0o644)
}

// AddItem registers an item in the marketplace.
func (s *Store) AddItem(item *Item) {
	s.mu.Lock(); defer s.mu.Unlock()
	s.items[item.Name] = item; s.saveCatalog()
}

// Get returns an item by name.
func (s *Store) Get(name string) (*Item, bool) {
	s.mu.RLock(); defer s.mu.RUnlock()
	item, ok := s.items[name]; return item, ok
}

// List returns all items, optionally filtered by type.
func (s *Store) List(itemType string) []*Item {
	s.mu.RLock(); defer s.mu.RUnlock()
	var out []*Item
	for _, item := range s.items {
		if itemType == "" || item.Type == itemType { out = append(out, item) }
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Search finds items matching a query in name, description, or tags.
func (s *Store) Search(query string) []*Item {
	s.mu.RLock(); defer s.mu.RUnlock()
	query = strings.ToLower(query)
	var out []*Item
	for _, item := range s.items {
		if strings.Contains(strings.ToLower(item.Name), query) ||
			strings.Contains(strings.ToLower(item.Description), query) {
			out = append(out, item)
			continue
		}
		for _, tag := range item.Tags {
			if strings.Contains(strings.ToLower(tag), query) { out = append(out, item); break }
		}
	}
	return out
}

// MarkInstalled records that an item is installed at a certain version.
func (s *Store) MarkInstalled(name, version string) {
	s.mu.Lock(); defer s.mu.Unlock()
	if item, ok := s.items[name]; ok { item.Installed = true; item.InstalledVersion = version }
	s.saveCatalog()
}

// MarkUninstalled records removal.
func (s *Store) MarkUninstalled(name string) {
	s.mu.Lock(); defer s.mu.Unlock()
	if item, ok := s.items[name]; ok { item.Installed = false; item.InstalledVersion = "" }
	s.saveCatalog()
}

// Outdated returns installed items with newer versions available.
func (s *Store) Outdated() []*Item {
	s.mu.RLock(); defer s.mu.RUnlock()
	var out []*Item
	for _, item := range s.items {
		if item.Installed && item.InstalledVersion != item.Version { out = append(out, item) }
	}
	return out
}

// FormatCatalog formats the marketplace catalog for display.
func (s *Store) FormatCatalog() string {
	items := s.List("")
	var sb strings.Builder
	fmt.Fprintf(&sb, "Marketplace (%d items):\n\n", len(items))
	for _, item := range items {
		installed := ""
		if item.Installed { installed = " [installed]" }
		fmt.Fprintf(&sb, "  %s (%s v%s)%s\n", item.Name, item.Type, item.Version, installed)
		if item.Description != "" { fmt.Fprintf(&sb, "    %s\n", item.Description) }
	}
	return sb.String()
}

// FormatOutdated formats outdated items.
func (s *Store) FormatOutdated() string {
	items := s.Outdated()
	if len(items) == 0 { return "All items up to date.\n" }
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d outdated item(s):\n\n", len(items))
	for _, item := range items {
		fmt.Fprintf(&sb, "  %s: %s → %s\n", item.Name, item.InstalledVersion, item.Version)
	}
	return sb.String()
}

// ── Indexer ───────────────────────────────────────────────

// Indexer scans local directories for skills and MCP servers.
type Indexer struct{ store *Store }

func NewIndexer(store *Store) *Indexer { return &Indexer{store: store} }

func (ix *Indexer) IndexSkillDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil { return err }
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") { continue }
		name := strings.TrimSuffix(e.Name(), ".md")
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		info, _ := e.Info()
		ix.store.AddItem(&Item{
			Name: name, Type: "skill", Source: "local",
			Version: fmt.Sprintf("local-%d", info.ModTime().Unix()),
			Description: firstLine(string(data)),
		})
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 { s = s[:i] }
	s = strings.TrimSpace(s)
	if len(s) > 120 { s = s[:117] + "..." }
	return s
}
