// Package memory manages project memory files (AGENTS.md, REASONIX.md, etc.)
// and provides the remember/forget tools that update a durable per-project
// memory store. Memory is loaded into the cache-stable system prompt prefix.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// File is one loaded memory document.
type File struct {
	Path    string // absolute path
	Content string // full content
	Source  string // "project", "user", "ancestor", "scoped"
	Chars   int    // content length
}

// Store loads project memory from AGENTS.md / REASONIX.md files in the
// workspace and user home directory. It is safe for concurrent reads.
type Store struct {
	mu      sync.RWMutex
	files   []File
	root    string
	homeDir string
}

// NewStore creates a memory store for the given project root.
func NewStore(projectRoot string) (*Store, error) {
	home, _ := os.UserHomeDir()
	s := &Store{root: projectRoot, homeDir: home}
	s.load()
	return s, nil
}

func (s *Store) load() {
	var files []File

	// Project: REASONIX.md (top priority), then AGENTS.md, CLAUDE.md, CLAW.md
	if s.root != "" {
		for _, name := range []string{"REASONIX.md", "AGENTS.md", "CLAUDE.md", "CLAW.md"} {
			p := filepath.Join(s.root, name)
			if f := s.readFile(p, "project"); f != nil {
				files = append(files, *f)
			}
		}
	}

	// User home
	if s.homeDir != "" {
		for _, dir := range []string{".reasonix", ".agents", ".agent", ".claude"} {
			for _, name := range []string{"REASONIX.md", "AGENTS.md", "CLAUDE.md"} {
				p := filepath.Join(s.homeDir, dir, name)
				if f := s.readFile(p, "user"); f != nil {
					// Don't duplicate if same content as project
					found := false
					for _, pf := range files {
						if pf.Content == f.Content {
							found = true
							break
						}
					}
					if !found {
						files = append(files, *f)
					}
				}
			}
		}
	}

	s.mu.Lock()
	s.files = files
	s.mu.Unlock()
}

func (s *Store) readFile(path, source string) *File {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}
	return &File{
		Path:    path,
		Content: content,
		Source:  source,
		Chars:   len(content),
	}
}

// Prompt returns the memory content to append to the system prompt.
// It is byte-stable across calls so the prefix cache stays hot.
func (s *Store) Prompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.files) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, f := range s.files {
		sb.WriteString("\n\n## Project Memory (" + f.Source + ": " + filepath.Base(f.Path) + ")\n\n")
		sb.WriteString(f.Content)
	}
	return sb.String()
}

// Files returns a snapshot of loaded memory files.
func (s *Store) Files() []File {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]File, len(s.files))
	copy(out, s.files)
	return out
}

// ── Durable memory (remember / forget tools) ───────────────

// DurableStore is a per-project durable memory store backed by Markdown
// files in .reasonix/memory/ with a MEMORY.md index.
type DurableStore struct {
	mu   sync.Mutex
	root string
	dir  string
}

// NewDurableStore creates a durable store for the given project root.
func NewDurableStore(projectRoot string) *DurableStore {
	dir := filepath.Join(projectRoot, ".reasonix", "memory")
	os.MkdirAll(dir, 0o755)
	idxPath := filepath.Join(dir, "MEMORY.md")
	if _, err := os.Stat(idxPath); os.IsNotExist(err) {
		os.WriteFile(idxPath, []byte("# Project Memory\n\n"), 0o644)
	}
	return &DurableStore{root: projectRoot, dir: dir}
}

// Remember saves a fact to the durable memory store and updates the index.
// Returns the slug and a description of what was changed.
func (s *DurableStore) Remember(title, body string) (slug string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug = slugify(title)
	if slug == "" {
		return "", fmt.Errorf("empty title")
	}

	path := filepath.Join(s.dir, slug+".md")
	content := fmt.Sprintf("# %s\n\n%s\n", title, body)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}

	// Update index
	idxPath := filepath.Join(s.dir, "MEMORY.md")
	idx, _ := os.ReadFile(idxPath)
	entry := fmt.Sprintf("- [%s](%s.md) — %s\n", title, slug, firstLineOr(body, 80))
	newIdx := string(idx) + entry
	os.WriteFile(idxPath, []byte(newIdx), 0o644)
	return slug, nil
}

// Forget removes a memory by slug.
func (s *DurableStore) Forget(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, slug+".md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Remove from index
	idxPath := filepath.Join(s.dir, "MEMORY.md")
	data, _ := os.ReadFile(idxPath)
	lines := strings.Split(string(data), "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if !strings.Contains(line, slug+".md") {
			filtered = append(filtered, line)
		}
	}
	os.WriteFile(idxPath, []byte(strings.Join(filtered, "\n")+"\n"), 0o644)
	return nil
}

// Prompt returns the durable memory content for the system prompt prefix.
func (s *DurableStore) Prompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, _ := os.ReadDir(s.dir)
	var files []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "MEMORY.md" || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	var sb strings.Builder
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "" {
			continue
		}
		sb.WriteString("\n\n")
		sb.WriteString(trimmed)
	}
	return sb.String()
}

// ── Helpers ────────────────────────────────────────────────

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var out []rune
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			out = append(out, r)
		} else if r == ' ' || r == '.' {
			out = append(out, '-')
		}
	}
	result := strings.Trim(string(out), "-")
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}

func firstLineOr(s string, maxLen int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
