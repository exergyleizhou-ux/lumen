// Package checkpoint captures pre-edit file snapshots so the user can rewind
// an agent turn. Each writer tool's onPreEdit hook feeds snapshots into the
// Store; /rewind (or Esc-Esc in TUI) restores all tracked files to their
// pre-edit state and clears the store for the next turn.
package checkpoint

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"lumen/internal/diff"
)

// Store holds pre-edit file snapshots for one turn, keyed by absolute path.
// It is safe for concurrent use (the agent records snapshots in parallel
// goroutines during read-only batch execution).
type Store struct {
	mu        sync.Mutex
	snapshots map[string]snapshot
	order     []string // insertion order for rewind
}

type snapshot struct {
	content string
	new     bool // file was created this turn (didn't exist before)
}

// New creates an empty checkpoint store.
func New() *Store {
	return &Store{snapshots: map[string]snapshot{}}
}

// Save records the pre-edit content of a file. Call from the agent's onPreEdit
// hook before a writer tool runs. path is the absolute or relative path the
// tool will write to.
func (s *Store) Save(path string, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	abs := absPath(path)
	if _, exists := s.snapshots[abs]; !exists {
		s.order = append(s.order, abs)
	}
	s.snapshots[abs] = snapshot{content: content, new: false}
}

// SaveFromChange is a convenience wrapper that extracts the pre-edit content
// from a diff.Change and records it.
func (s *Store) SaveFromChange(ch diff.Change) {
	s.mu.Lock()
	defer s.mu.Unlock()

	abs := absPath(ch.Path)
	if _, exists := s.snapshots[abs]; !exists {
		s.order = append(s.order, abs)
	}
	s.snapshots[abs] = snapshot{content: ch.Before, new: ch.New}
}

// HasSnapshots reports whether there are any files that can be rewound.
func (s *Store) HasSnapshots() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snapshots) > 0
}

// Count returns the number of tracked files.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snapshots)
}

// Rewind restores all tracked files to their pre-edit state and clears the
// store. Files that were created this turn (new=true) are deleted. Returns
// a summary of what was rewound.
func (s *Store) Rewind() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var rewound []string
	var errs []error

	for _, path := range s.order {
		snap, ok := s.snapshots[path]
		if !ok {
			continue
		}
		if snap.new {
			// File was created this turn — delete it
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
				continue
			}
			rewound = append(rewound, path+" (deleted)")
		} else {
			// Restore pre-edit content
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				errs = append(errs, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err))
				continue
			}
			if err := os.WriteFile(path, []byte(snap.content), 0o644); err != nil {
				errs = append(errs, fmt.Errorf("write %s: %w", path, err))
				continue
			}
			rewound = append(rewound, path)
		}
	}

	// Clear
	s.snapshots = map[string]snapshot{}
	s.order = nil

	if len(errs) > 0 {
		return rewound, fmt.Errorf("rewind partial: %v", errs)
	}
	return rewound, nil
}

// Clear discards all snapshots without restoring.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = map[string]snapshot{}
	s.order = nil
}

// List returns the tracked file paths in insertion order.
func (s *Store) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

func absPath(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return filepath.Clean(abs)
}
