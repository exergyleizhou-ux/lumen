// Package filewatcher provides filesystem change monitoring for Lumen.
// It uses efficient polling (no external dependency) to detect file
// creations, modifications, and deletions. Used by the TUI for live
// change indicators and by the agent for external tool detection.
package filewatcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event represents one filesystem change.
type Event struct {
	Path      string    `json:"path"`
	Op        Op        `json:"op"`
	Size      int64     `json:"size"`
	Timestamp time.Time `json:"timestamp"`
}

// Op describes what kind of change occurred.
type Op string

const (
	OpCreate Op = "create"
	OpWrite  Op = "write"
	OpRemove Op = "remove"
)

// Watcher monitors a directory tree for file changes using polling.
type Watcher struct {
	mu       sync.Mutex
	root     string
	events   chan Event
	done     chan struct{}
	snapshot map[string]snapshotEntry
	interval time.Duration
	ignore   []string
}

type snapshotEntry struct {
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// NewWatcher creates a file watcher that polls the given root directory.
func NewWatcher(root string, interval time.Duration) (*Watcher, error) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}

	w := &Watcher{
		root:     root,
		events:   make(chan Event, 256),
		done:     make(chan struct{}),
		snapshot: map[string]snapshotEntry{},
		interval: interval,
		ignore:   []string{".git", "node_modules", ".build", "target", "dist", "vendor", ".lumen"},
	}

	// Take initial snapshot
	w.takeSnapshot()

	go w.loop()
	return w, nil
}

func (w *Watcher) takeSnapshot() {
	w.snapshot = map[string]snapshotEntry{}
	filepath.Walk(w.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			for _, ig := range w.ignore {
				if name == ig {
					return filepath.SkipDir
				}
			}
			return nil
		}
		w.snapshot[path] = snapshotEntry{Size: info.Size(), ModTime: info.ModTime(), IsDir: false}
		return nil
	})
}

func (w *Watcher) loop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	defer close(w.done)
	defer close(w.events)

	for {
		select {
		case <-ticker.C:
			w.scan()
		case <-w.events: // drained — channel closed
			return
		}
	}
}

func (w *Watcher) scan() {
	w.mu.Lock()
	defer w.mu.Unlock()

	current := map[string]snapshotEntry{}

	filepath.Walk(w.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".") && info.IsDir() {
			return filepath.SkipDir
		}
		if info.IsDir() {
			for _, ig := range w.ignore {
				if name == ig {
					return filepath.SkipDir
				}
			}
			return nil
		}
		current[path] = snapshotEntry{Size: info.Size(), ModTime: info.ModTime(), IsDir: false}
		return nil
	})

	// Detect changes
	for path, cur := range current {
		prev, ok := w.snapshot[path]
		if !ok {
			w.events <- Event{Path: path, Op: OpCreate, Size: cur.Size, Timestamp: time.Now()}
		} else if cur.Size != prev.Size || !cur.ModTime.Equal(prev.ModTime) {
			w.events <- Event{Path: path, Op: OpWrite, Size: cur.Size, Timestamp: time.Now()}
		}
	}
	for path := range w.snapshot {
		if _, ok := current[path]; !ok {
			w.events <- Event{Path: path, Op: OpRemove, Size: 0, Timestamp: time.Now()}
		}
	}

	w.snapshot = current
}

// Events returns the channel of file change events.
func (w *Watcher) Events() <-chan Event { return w.events }

// Done returns a channel closed when the watcher stops.
func (w *Watcher) Done() <-chan struct{} { return w.done }

// Close stops the watcher.
func (w *Watcher) Close() error {
	w.done <- struct{}{}
	return nil
}

// ── Change aggregator ──────────────────────────────────────

// ChangeSummary aggregates file changes for display.
type ChangeSummary struct {
	mu       sync.Mutex
	changes  map[string]*FileActivity
	window   time.Duration
	onChange func(path string, count int)
}

type FileActivity struct {
	Path       string    `json:"path"`
	Count      int       `json:"count"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	Operations []Op      `json:"operations"`
}

func NewChangeSummary(window time.Duration, onChange func(string, int)) *ChangeSummary {
	return &ChangeSummary{changes: map[string]*FileActivity{}, window: window, onChange: onChange}
}

func (cs *ChangeSummary) Record(e Event) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now()
	for path, a := range cs.changes {
		if now.Sub(a.LastSeen) > cs.window {
			delete(cs.changes, path)
		}
	}
	a, ok := cs.changes[e.Path]
	if !ok {
		a = &FileActivity{Path: e.Path, FirstSeen: now}
		cs.changes[e.Path] = a
	}
	a.Count++
	a.LastSeen = now
	a.Operations = append(a.Operations, e.Op)
	if cs.onChange != nil {
		cs.onChange(e.Path, a.Count)
	}
}

func (cs *ChangeSummary) Active() []FileActivity {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]FileActivity, 0, len(cs.changes))
	for _, a := range cs.changes {
		out = append(out, *a)
	}
	return out
}

func (cs *ChangeSummary) Reset() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.changes = map[string]*FileActivity{}
}

// ── Session tracker ────────────────────────────────────────

type SessionTracker struct {
	mu      sync.Mutex
	files   map[string]*SessionFile
	started time.Time
}

type SessionFile struct {
	Path          string    `json:"path"`
	ChangeCount   int       `json:"change_count"`
	FirstModified time.Time `json:"first_modified"`
	LastModified  time.Time `json:"last_modified"`
	TurnsModified []int     `json:"turns_modified"`
}

func NewSessionTracker() *SessionTracker {
	return &SessionTracker{files: map[string]*SessionFile{}, started: time.Now()}
}

func (st *SessionTracker) RecordFileChange(path string, turn int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	f, ok := st.files[path]
	if !ok {
		f = &SessionFile{Path: path, FirstModified: time.Now()}
		st.files[path] = f
	}
	f.ChangeCount++
	f.LastModified = time.Now()
	if len(f.TurnsModified) == 0 || f.TurnsModified[len(f.TurnsModified)-1] != turn {
		f.TurnsModified = append(f.TurnsModified, turn)
	}
}

func (st *SessionTracker) Files() []SessionFile {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]SessionFile, 0, len(st.files))
	for _, f := range st.files {
		out = append(out, *f)
	}
	return out
}

func (st *SessionTracker) Count() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.files)
}

func (st *SessionTracker) TotalChanges() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	total := 0
	for _, f := range st.files {
		total += f.ChangeCount
	}
	return total
}

// NotifyOnChange is a convenience function that runs a watcher and
// feeds changes into a ChangeSummary. It blocks until the watcher stops.
func NotifyOnChange(root string, cs *ChangeSummary, interval time.Duration) error {
	w, err := NewWatcher(root, interval)
	if err != nil {
		return fmt.Errorf("watcher: %w", err)
	}
	defer w.Close()

	for e := range w.Events() {
		cs.Record(e)
	}
	return nil
}
