// Package memory provides persistent cross-session user memory.
// Memories are stored as individual JSON files in ~/.lumen/memories/
// and auto-loaded into the system prompt at session start.
//
// The agent uses three tools to manage memories:
//   - remember  — save a new memory or update an existing one
//   - forget    — delete a memory by name
//   - memories  — list all stored memories
package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind classifies a memory for filtering and display.
type Kind string

const (
	KindUser      Kind = "user"      // personal facts, preferences
	KindFeedback  Kind = "feedback"  // explicit user feedback/guidance
	KindProject   Kind = "project"   // project-specific constraints/goals
	KindReference Kind = "reference" // external links, docs, resources
)

// Entry is a single memory.
type Entry struct {
	Name        string    `json:"name"`        // machine-friendly slug (kebab-case)
	Title       string    `json:"title"`       // human-readable label
	Description string    `json:"description"` // one-line summary for the index
	Body        string    `json:"body"`        // full content (markdown)
	Kind        Kind      `json:"kind"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Store persists memories to disk.
type Store struct {
	mu   sync.Mutex
	dir  string
	list []Entry // cached on load, kept in sync
}

// NewStore opens (or creates) the memory directory.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	s.reload()
	return s, nil
}

// ── CRUD ─────────────────────────────────────────────────────

// Save writes a memory. If one with the same Name exists, it is updated.
func (s *Store) Save(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	e.Name = sanitizeName(e.Name)

	// Update in-memory list
	found := false
	for i := range s.list {
		if s.list[i].Name == e.Name {
			s.list[i] = e
			found = true
			break
		}
	}
	if !found {
		s.list = append(s.list, e)
	}

	return s.writeFile(e)
}

// Delete removes a memory by name. No-op if not found.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = sanitizeName(name)
	for i := range s.list {
		if s.list[i].Name == name {
			s.list = append(s.list[:i], s.list[i+1:]...)
			break
		}
	}
	return os.Remove(filepath.Join(s.dir, name+".json"))
}

// Get returns a single memory by name, or nil.
func (s *Store) Get(name string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].Name == sanitizeName(name) {
			e := s.list[i]
			return &e
		}
	}
	return nil
}

// List returns all memories, newest first.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.list))
	copy(out, s.list)
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// SystemPrompt builds a compact block to inject into the system prompt.
// Returns "" when there are no memories.
func (s *Store) SystemPrompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.list) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## User Memories (persistent across sessions)\n\n")
	sb.WriteString("These are facts, preferences, and guidance the user has explicitly saved.\n")
	sb.WriteString("Use forget/remember tools to manage them. Always respect these preferences.\n\n")

	for _, e := range s.list {
		sb.WriteString("### ")
		sb.WriteString(e.Title)
		sb.WriteString(" (")
		sb.WriteString(string(e.Kind))
		sb.WriteString(")\n")
		sb.WriteString(e.Body)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// ── internal ─────────────────────────────────────────────────

func (s *Store) reload() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.list = nil
	entries, _ := os.ReadDir(s.dir)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		s.list = append(s.list, e)
	}
}

func (s *Store) writeFile(e Entry) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, e.Name+".json"), data, 0o600)
}

func sanitizeName(s string) string {
	// Keep only lowercase letters, digits, hyphens, underscores
	s = strings.ToLower(strings.TrimSpace(s))
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else if c == ' ' {
			result = append(result, '-')
		}
	}
	if len(result) == 0 {
		return "untitled"
	}
	return string(result)
}
