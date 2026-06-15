// Package snapshot provides workspace snapshotting: capturing the state
// of files before and after agent modifications, with the ability to
// restore any previous snapshot. More granular than checkpoint (which
// operates per-turn), snapshot captures per-file or per-operation state.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Snapshot is a point-in-time capture of file contents.
type Snapshot struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Label     string            `json:"label,omitempty"`
	Files     map[string]string `json:"files"` // path → content
	Parent    string            `json:"parent,omitempty"` // parent snapshot ID
}

// Manager creates and restores file snapshots.
type Manager struct {
	mu        sync.Mutex
	snapshots map[string]*Snapshot
	order     []string
	dir       string // storage directory for persistent snapshots
}

// NewManager creates a snapshot manager.
func NewManager(storageDir string) (*Manager, error) {
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{snapshots: map[string]*Snapshot{}, dir: storageDir}, nil
}

// Capture creates a snapshot of the given files.
func (m *Manager) Capture(label string, paths []string) (*Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := fmt.Sprintf("snap-%d", time.Now().UnixNano())
	snap := &Snapshot{
		ID: id, Timestamp: time.Now(), Label: label,
		Files: map[string]string{},
	}
	if len(m.order) > 0 {
		snap.Parent = m.order[len(m.order)-1]
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				snap.Files[p] = "" // file didn't exist
				continue
			}
			return nil, fmt.Errorf("capture %s: %w", p, err)
		}
		snap.Files[p] = string(data)
	}

	m.snapshots[id] = snap
	m.order = append(m.order, id)
	return snap, nil
}

// CaptureAll captures all files in a directory (non-recursive by default).
func (m *Manager) CaptureAll(label string, dir string, recursive bool) (*Snapshot, error) {
	var paths []string
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if !recursive && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		paths = append(paths, path)
		return nil
	}
	if err := filepath.Walk(dir, walkFn); err != nil {
		return nil, err
	}
	return m.Capture(label, paths)
}

// Restore restores files to a given snapshot.
func (m *Manager) Restore(id string) ([]string, error) {
	m.mu.Lock()
	snap, ok := m.snapshots[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("snapshot %q not found", id)
	}

	var restored []string
	for path, content := range snap.Files {
		if content == "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return restored, fmt.Errorf("remove %s: %w", path, err)
			}
			restored = append(restored, path+" (deleted)")
		} else {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return restored, fmt.Errorf("mkdir: %w", err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return restored, fmt.Errorf("write %s: %w", path, err)
			}
			restored = append(restored, path)
		}
	}
	return restored, nil
}

// List returns all snapshot IDs in order.
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.order))
	copy(out, m.order)
	return out
}

// Get returns a snapshot by ID.
func (m *Manager) Get(id string) (*Snapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.snapshots[id]
	return s, ok
}

// Diff returns the files changed between two snapshots.
func (m *Manager) Diff(fromID, toID string) ([]string, error) {
	m.mu.Lock()
	from, ok1 := m.snapshots[fromID]
	to, ok2 := m.snapshots[toID]
	m.mu.Unlock()
	if !ok1 { return nil, fmt.Errorf("from snapshot %q not found", fromID) }
	if !ok2 { return nil, fmt.Errorf("to snapshot %q not found", toID) }

	var changed []string
	allPaths := map[string]bool{}
	for p := range from.Files { allPaths[p] = true }
	for p := range to.Files { allPaths[p] = true }

	for p := range allPaths {
		oldC := from.Files[p]
		newC := to.Files[p]
		if oldC != newC {
			changed = append(changed, p)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

// Prune removes snapshots older than the given duration, keeping at most maxKeep.
func (m *Manager) Prune(maxAge time.Duration, maxKeep int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var kept []string
	for _, id := range m.order {
		s, ok := m.snapshots[id]
		if !ok { continue }
		if s.Timestamp.After(cutoff) || len(kept) < maxKeep {
			kept = append(kept, id)
		} else {
			delete(m.snapshots, id)
		}
	}
	m.order = kept
}

// Count returns the number of stored snapshots.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.snapshots)
}

// FormatList formats snapshot IDs for display.
func FormatList(ids []string) string {
	if len(ids) == 0 {
		return "No snapshots.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d snapshot(s):\n", len(ids))
	for _, id := range ids {
		fmt.Fprintf(&sb, "  - %s\n", id)
	}
	return sb.String()
}
