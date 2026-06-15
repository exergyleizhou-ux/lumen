// Package watchpoint provides data watchpoints: monitor a value/path
// for changes, trigger callbacks on condition matches, snapshot
// before/after values, and maintain a watch log.
package watchpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ChangeType classifies a watched change.
type ChangeType int

const (
	ChangeAdded ChangeType = iota
	ChangeRemoved
	ChangeModified
	ChangeNoop
)

func (c ChangeType) String() string {
	switch c {
	case ChangeAdded:
		return "added"
	case ChangeRemoved:
		return "removed"
	case ChangeModified:
		return "modified"
	default:
		return "noop"
	}
}

// Snapshot captures a value at a point in time.
type Snapshot struct {
	Value     any       `json:"value"`
	Timestamp time.Time `json:"timestamp"`
	Hash      string    `json:"hash"`
}

// Change represents a detected change.
type Change struct {
	Path      string     `json:"path"`
	Type      ChangeType `json:"type"`
	Before    *Snapshot  `json:"before,omitempty"`
	After     *Snapshot  `json:"after,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}

// Watcher watches a set of paths for changes.
type Watcher struct {
	mu        sync.RWMutex
	state     map[string]*Snapshot
	callbacks map[string][]func(Change)
	log       []Change
	maxLog    int
}

// NewWatcher creates a watcher.
func NewWatcher() *Watcher {
	return &Watcher{state: map[string]*Snapshot{}, callbacks: map[string][]func(Change){}, maxLog: 500}
}

// Set initializes or updates a path's value.
func (w *Watcher) Set(path string, value any) *Change {
	w.mu.Lock()
	defer w.mu.Unlock()

	newSnap := w.snapshot(value)
	oldSnap, existed := w.state[path]

	if !existed {
		w.state[path] = newSnap
		change := &Change{Path: path, Type: ChangeAdded, After: newSnap, Timestamp: time.Now()}
		w.record(change)
		w.fireCallbacks(path, *change)
		return change
	}

	if oldSnap.Hash == newSnap.Hash {
		// Update timestamp but no real change
		w.state[path] = newSnap
		return &Change{Path: path, Type: ChangeNoop, Timestamp: time.Now()}
	}

	w.state[path] = newSnap
	change := &Change{Path: path, Type: ChangeModified, Before: oldSnap, After: newSnap, Timestamp: time.Now()}
	w.record(change)
	w.fireCallbacks(path, *change)
	return change
}

// Delete removes a path.
func (w *Watcher) Delete(path string) *Change {
	w.mu.Lock()
	defer w.mu.Unlock()
	oldSnap, existed := w.state[path]
	if !existed {
		return nil
	}
	delete(w.state, path)
	change := &Change{Path: path, Type: ChangeRemoved, Before: oldSnap, Timestamp: time.Now()}
	w.record(change)
	w.fireCallbacks(path, *change)
	return change
}

// Get returns the current snapshot for a path.
func (w *Watcher) Get(path string) *Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.state[path]
}

// GetValue returns the current value for a path.
func (w *Watcher) GetValue(path string) (any, bool) {
	s := w.Get(path)
	if s == nil {
		return nil, false
	}
	return s.Value, true
}

// OnChange registers a callback for a path prefix.
func (w *Watcher) OnChange(prefix string, fn func(Change)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacks[prefix] = append(w.callbacks[prefix], fn)
}

// Log returns recent changes.
func (w *Watcher) Log() []Change {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Change, len(w.log))
	copy(out, w.log)
	return out
}

// ChangedSince returns changes after a timestamp.
func (w *Watcher) ChangedSince(since time.Time) []Change {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []Change
	for _, c := range w.log {
		if c.Timestamp.After(since) {
			out = append(out, c)
		}
	}
	return out
}

// Paths returns all watched paths.
func (w *Watcher) Paths() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	var out []string
	for p := range w.state {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Diff returns changes between two snapshots of the same path.
func (w *Watcher) Diff(path string) string {
	if oldS, newS := w.Get(path), w.state[path]; oldS != nil && newS != nil {
		if oldS.Hash != newS.Hash {
			return fmt.Sprintf("changed: %s → %s", truncate(fmt.Sprint(oldS.Value), 50), truncate(fmt.Sprint(newS.Value), 50))
		}
	}
	return "no change"
}

// FormatLog formats the change log.
func (w *Watcher) FormatLog() string {
	log := w.Log()
	if len(log) == 0 {
		return "No changes recorded.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Watchpoint Log (%d changes):\n%s\n\n", len(log), strings.Repeat("─", 60))
	for _, c := range log {
		icon := iconForChange(c.Type)
		fmt.Fprintf(&sb, "  %s %s %s", icon, c.Timestamp.Format("15:04:05.000"), c.Path)
		switch c.Type {
		case ChangeAdded:
			fmt.Fprintf(&sb, " → %s\n", truncate(fmt.Sprint(c.After.Value), 60))
		case ChangeRemoved:
			fmt.Fprintf(&sb, " (was %s)\n", truncate(fmt.Sprint(c.Before.Value), 60))
		case ChangeModified:
			fmt.Fprintf(&sb, "\n    from: %s\n    to:   %s\n",
				truncate(fmt.Sprint(c.Before.Value), 60), truncate(fmt.Sprint(c.After.Value), 60))
		default:
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func (w *Watcher) snapshot(value any) *Snapshot {
	data := fmt.Sprint(value)
	h := sha256.Sum256([]byte(data))
	return &Snapshot{Value: value, Timestamp: time.Now(), Hash: hex.EncodeToString(h[:])}
}

func (w *Watcher) record(change *Change) {
	w.log = append(w.log, *change)
	if len(w.log) > w.maxLog {
		w.log = w.log[1:]
	}
}

func (w *Watcher) fireCallbacks(path string, change Change) {
	for prefix, fns := range w.callbacks {
		if strings.HasPrefix(path, prefix) {
			for _, fn := range fns {
				fn(change)
			}
		}
	}
}

func iconForChange(c ChangeType) string {
	switch c {
	case ChangeAdded:
		return "🟢"
	case ChangeRemoved:
		return "🔴"
	case ChangeModified:
		return "🟡"
	default:
		return "⚪"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// ── Deep Watcher ──────────────────────────────────────────

// DeepWatcher watches nested map/slice structures for changes at any depth.
type DeepWatcher struct {
	mu     sync.Mutex
	watch  *Watcher
	prefix string
}

// NewDeepWatcher creates a deep watcher.
func NewDeepWatcher(w *Watcher, prefix string) *DeepWatcher {
	return &DeepWatcher{watch: w, prefix: prefix}
}

// WatchAll sets nested paths from a map.
func (dw *DeepWatcher) WatchAll(data map[string]any) []*Change {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	return dw.watchAllRec(data, dw.prefix)
}

func (dw *DeepWatcher) watchAllRec(data any, prefix string) []*Change {
	var changes []*Change
	switch v := data.(type) {
	case map[string]any:
		for k, val := range v {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			changes = append(changes, dw.watch.Set(path, val))
			changes = append(changes, dw.watchAllRec(val, path)...)
		}
	case []any:
		for i, val := range v {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			changes = append(changes, dw.watch.Set(path, val))
			changes = append(changes, dw.watchAllRec(val, path)...)
		}
	default:
		changes = append(changes, dw.watch.Set(prefix, v))
	}
	return changes
}

// ── Conditional Watcher ───────────────────────────────────

// ConditionWatcher fires only when a condition is met.
type ConditionWatcher struct {
	mu        sync.Mutex
	condition func(old, new any) bool
	watcher   *Watcher
}

// NewConditionWatcher creates a conditional watcher.
func NewConditionWatcher(w *Watcher, cond func(old, new any) bool) *ConditionWatcher {
	return &ConditionWatcher{watcher: w, condition: cond}
}

// CheckAndSet sets the value only if the condition passes.
func (cw *ConditionWatcher) CheckAndSet(path string, value any) (*Change, bool) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	old, _ := cw.watcher.GetValue(path)
	if cw.condition != nil && !cw.condition(old, value) {
		return nil, false
	}
	return cw.watcher.Set(path, value), true
}
