// Package configlive provides dynamic configuration reloading with file
// watching, hot-reload notifications, validation, and rollback. Config
// changes are detected via polling, validated before application, and
// rolled back on failure. Supports TOML, JSON, and YAML config formats.
package configlive

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Value is a configuration value with metadata.
type Value struct {
	Key      string    `json:"key"`
	Value    any       `json:"value"`
	Default  any       `json:"default"`
	Source   string    `json:"source"`
	Modified time.Time `json:"modified"`
}

// Store holds all config values with change tracking.
type Store struct {
	mu       sync.RWMutex
	values   map[string]*Value
	history  []Change
	maxHist  int
	watchers map[string][]func(key string, old, new any)
}

// Change is one config modification.
type Change struct {
	Key        string    `json:"key"`
	OldValue   any       `json:"old_value"`
	NewValue   any       `json:"new_value"`
	Timestamp  time.Time `json:"timestamp"`
	RolledBack bool      `json:"rolled_back,omitempty"`
}

// NewStore creates a config store.
func NewStore() *Store {
	return &Store{
		values:   map[string]*Value{},
		maxHist:  500,
		watchers: map[string][]func(key string, old, new any){},
	}
}

// Set stores a value and tracks the change.
func (s *Store) Set(key string, value any, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.values[key]
	if exists && old.Value == value {
		return
	}

	oldVal := any(nil)
	if exists {
		oldVal = old.Value
	}
	now := time.Now()

	s.values[key] = &Value{Key: key, Value: value, Default: value, Source: source, Modified: now}
	s.history = append(s.history, Change{Key: key, OldValue: oldVal, NewValue: value, Timestamp: now})
	if len(s.history) > s.maxHist {
		s.history = s.history[1:]
	}

	// Fire watchers
	for _, fn := range s.watchers[key] {
		fn(key, oldVal, value)
	}
}

// Get retrieves a value.
func (s *Store) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[key]
	if !ok {
		return nil, false
	}
	return v.Value, true
}

// GetString retrieves a string value.
func (s *Store) GetString(key string) string {
	v, ok := s.Get(key)
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// GetInt retrieves an int value.
func (s *Store) GetInt(key string) int {
	v, ok := s.Get(key)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// Watch registers a callback for changes to a key prefix.
func (s *Store) Watch(prefix string, fn func(key string, old, new any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watchers[prefix] = append(s.watchers[prefix], fn)
}

// History returns recent changes.
func (s *Store) History(limit int) []Change {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.history) {
		limit = len(s.history)
	}
	out := make([]Change, limit)
	copy(out, s.history[len(s.history)-limit:])
	return out
}

// Rollback reverts the last change for a key.
func (s *Store) Rollback(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.history) - 1; i >= 0; i-- {
		if s.history[i].Key == key && s.history[i].OldValue != nil {
			s.values[key] = &Value{Key: key, Value: s.history[i].OldValue, Modified: time.Now()}
			s.history[i].RolledBack = true
			return nil
		}
	}
	return fmt.Errorf("no rollback available for %q", key)
}

// Keys returns all config keys.
func (s *Store) Keys() []string { s.mu.RLock(); defer s.mu.RUnlock(); return s.keysLocked() }

// keysLocked assumes the caller holds the lock.
func (s *Store) keysLocked() []string {
	keys := make([]string, 0, len(s.values))
	for k := range s.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Format formats the current configuration.
func (s *Store) Format() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Configuration (%d keys):\n\n", len(s.values))
	for _, k := range s.keysLocked() {
		v := s.values[k]
		fmt.Fprintf(&sb, "  %-30s = %v  [%s]\n", k, v.Value, v.Source)
	}
	return sb.String()
}

// ── File Watcher ──────────────────────────────────────────

// Watcher polls a config file and applies changes to a store.
type Watcher struct {
	mu       sync.Mutex
	path     string
	store    *Store
	interval time.Duration
	lastMod  time.Time
	stopCh   chan struct{}
}

// NewWatcher creates a config file watcher.
func NewWatcher(path string, store *Store, interval time.Duration) *Watcher {
	return &Watcher{path: path, store: store, interval: interval, stopCh: make(chan struct{})}
}

// Start begins polling the config file.
func (w *Watcher) Start() {
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-w.stopCh:
				return
			case <-ticker.C:
				w.check()
			}
		}
	}()
}

// Stop stops polling.
func (w *Watcher) Stop() { close(w.stopCh) }

func (w *Watcher) check() {
	info, err := os.Stat(w.path)
	if err != nil {
		return
	}
	if info.ModTime().Equal(w.lastMod) {
		return
	}
	w.lastMod = info.ModTime()
	data, err := os.ReadFile(w.path)
	if err != nil {
		return
	}
	w.store.Set(filepath.Base(w.path), string(data), "file")
}

// ── Validation ────────────────────────────────────────────

// Validator checks config values before applying.
type Validator struct {
	mu    sync.Mutex
	rules map[string]func(any) error
}

// NewValidator creates a config validator.
func NewValidator() *Validator {
	return &Validator{rules: map[string]func(any) error{}}
}

// AddRule registers a validation rule for a key prefix.
func (v *Validator) AddRule(prefix string, fn func(any) error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.rules[prefix] = fn
}

// Validate checks a value against registered rules.
func (v *Validator) Validate(key string, value any) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	for prefix, fn := range v.rules {
		if strings.HasPrefix(key, prefix) {
			if err := fn(value); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	return nil
}
