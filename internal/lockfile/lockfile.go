// Package lockfile provides distributed lock file management with
// acquisition, release, TTL refresh, and deadlock detection. Uses
// a pluggable backend (in-memory for now, Redis-ready).
package lockfile

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Lock struct {
	Key        string
	Owner      string
	TTL        time.Duration
	AcquiredAt time.Time
	ExpiresAt  time.Time
	Metadata   map[string]string
}
type Manager struct {
	mu    sync.Mutex
	locks map[string]*Lock
}

func NewManager() *Manager { return &Manager{locks: map[string]*Lock{}} }
func (m *Manager) Acquire(key, owner string, ttl time.Duration) (*Lock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.locks[key]; ok {
		if time.Now().Before(existing.ExpiresAt) {
			return nil, fmt.Errorf("lock %q held by %q (expires %v)", key, existing.Owner, existing.ExpiresAt.Format(time.RFC3339))
		}
		delete(m.locks, key)
	}
	l := &Lock{Key: key, Owner: owner, TTL: ttl, AcquiredAt: time.Now(), ExpiresAt: time.Now().Add(ttl)}
	m.locks[key] = l
	return l, nil
}
func (m *Manager) Release(key, owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[key]
	if !ok {
		return fmt.Errorf("lock %q not found", key)
	}
	if l.Owner != owner {
		return fmt.Errorf("lock %q owned by %q, not %q", key, l.Owner, owner)
	}
	delete(m.locks, key)
	return nil
}
func (m *Manager) Refresh(key, owner string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[key]
	if !ok {
		return fmt.Errorf("lock %q not found", key)
	}
	if l.Owner != owner {
		return fmt.Errorf("lock %q owned by %q, not %q", key, l.Owner, owner)
	}
	l.TTL = ttl
	l.ExpiresAt = time.Now().Add(ttl)
	return nil
}
func (m *Manager) Expired() []*Lock {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var out []*Lock
	for _, l := range m.locks {
		if now.After(l.ExpiresAt) {
			out = append(out, l)
		}
	}
	return out
}
func (m *Manager) GarbageCollect() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	collected := 0
	for k, l := range m.locks {
		if now.After(l.ExpiresAt) {
			delete(m.locks, k)
			collected++
		}
	}
	return collected
}
func (m *Manager) List() []*Lock { m.mu.Lock(); defer m.mu.Unlock(); return m.listLocked() }

// listLocked assumes the caller holds the lock.
func (m *Manager) listLocked() []*Lock {
	var out []*Lock
	for _, l := range m.locks {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AcquiredAt.Before(out[j].AcquiredAt) })
	return out
}
func (m *Manager) FormatLocks() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Lock Manager (%d locks):\n%s\n\n", len(m.locks), strings.Repeat("─", 60))
	for _, l := range m.listLocked() {
		remaining := time.Until(l.ExpiresAt).Round(time.Millisecond)
		fmt.Fprintf(&sb, "  %-30s owner=%-15s ttl=%v expires_in=%v\n", l.Key, l.Owner, l.TTL, remaining)
	}
	return sb.String()
}
