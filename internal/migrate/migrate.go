// Package migrate provides schema and data migration infrastructure for
// evolving agent storage backends. It supports forward and rollback
// migrations with version tracking and transaction safety.
package migrate

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Migration is a single versioned migration step.
type Migration struct {
	Version     int       `json:"version"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Up          func() error `json:"-"`
	Down        func() error `json:"-"`
	AppliedAt   *time.Time  `json:"applied_at,omitempty"`
}

// Status is the migration state.
type Status struct {
	Version   int    `json:"version"`
	Name      string `json:"name"`
	Applied   bool   `json:"applied"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
}

// Runner manages migration execution.
type Runner struct {
	mu          sync.Mutex
	migrations  map[int]*Migration
	applied     map[int]bool
	history     []Migration
	lockFn      func() error
	unlockFn    func() error
}

// NewRunner creates a migration runner.
func NewRunner() *Runner {
	return &Runner{migrations: map[int]*Migration{}, applied: map[int]bool{}}
}

// Register adds a migration.
func (r *Runner) Register(m *Migration) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if _, ok := r.migrations[m.Version]; ok {
		return fmt.Errorf("duplicate migration version %d", m.Version)
	}
	r.migrations[m.Version] = m
	return nil
}

// SetLock sets distributed lock functions.
func (r *Runner) SetLock(lock, unlock func() error) {
	r.mu.Lock(); defer r.mu.Unlock()
	r.lockFn = lock; r.unlockFn = unlock
}

// Migrate runs all pending up migrations.
func (r *Runner) Migrate() ([]Status, error) {
	r.mu.Lock()
	if r.lockFn != nil { if err := r.lockFn(); err != nil { r.mu.Unlock(); return nil, fmt.Errorf("lock: %w", err) } }
	defer func() { if r.unlockFn != nil { r.unlockFn() } }()
	r.mu.Unlock()

	var statuses []Status

	versions := r.sortedVersions()
	for _, v := range versions {
		m := r.migrations[v]
		r.mu.Lock()
		applied := r.applied[v]
		r.mu.Unlock()

		if applied {
			statuses = append(statuses, Status{Version: v, Name: m.Name, Applied: true, AppliedAt: m.AppliedAt})
			continue
		}

		if err := m.Up(); err != nil {
			r.mu.Lock()
			r.applied[v] = false
			r.mu.Unlock()
			statuses = append(statuses, Status{Version: v, Name: m.Name, Applied: false})
			return statuses, fmt.Errorf("migration %d %q: %w", v, m.Name, err)
		}

		now := time.Now()
		m.AppliedAt = &now
		r.mu.Lock()
		r.applied[v] = true
		r.history = append(r.history, *m)
		r.mu.Unlock()

		statuses = append(statuses, Status{Version: v, Name: m.Name, Applied: true})
	}
	return statuses, nil
}

// Rollback reverts the last N migrations.
func (r *Runner) Rollback(steps int) ([]Status, error) {
	r.mu.Lock()
	if r.lockFn != nil { if err := r.lockFn(); err != nil { r.mu.Unlock(); return nil, fmt.Errorf("lock: %w", err) } }
	defer func() { if r.unlockFn != nil { r.unlockFn() } }()
	r.mu.Unlock()

	versions := r.sortedVersions()
	var rev []int
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		r.mu.Lock()
		if r.applied[v] { rev = append(rev, v) }
		r.mu.Unlock()
		if len(rev) >= steps { break }
	}

	var statuses []Status
	for _, v := range rev {
		m := r.migrations[v]
		if err := m.Down(); err != nil {
			return statuses, fmt.Errorf("rollback %d %q: %w", v, m.Name, err)
		}
		r.mu.Lock()
		delete(r.applied, v)
		m.AppliedAt = nil
		r.mu.Unlock()
		statuses = append(statuses, Status{Version: v, Name: m.Name, Applied: false})
	}
	return statuses, nil
}

// Status returns the status of all migrations.
func (r *Runner) Status() []Status {
	r.mu.Lock(); defer r.mu.Unlock()
	versions := r.sortedVersions()
	var out []Status
	for _, v := range versions {
		m := r.migrations[v]
		out = append(out, Status{Version: v, Name: m.Name, Applied: r.applied[v]})
	}
	return out
}

// Pending returns count of unapplied migrations.
func (r *Runner) Pending() int {
	r.mu.Lock(); defer r.mu.Unlock()
	count := 0
	for _, v := range r.sortedVersions() {
		if !r.applied[v] { count++ }
	}
	return count
}

func (r *Runner) sortedVersions() []int {
	versions := make([]int, 0, len(r.migrations))
	for v := range r.migrations { versions = append(versions, v) }
	sort.Ints(versions)
	return versions
}

// FormatStatus renders migration status.
func FormatStatus(statuses []Status) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Migration Status (%d entries):\n%s\n\n", len(statuses), strings.Repeat("─", 50))
	for _, s := range statuses {
		icon := "⬜"
		if s.Applied { icon = "✅" }
		fmt.Fprintf(&sb, "  %s v%d %s", icon, s.Version, s.Name)
		if s.AppliedAt != nil { fmt.Fprintf(&sb, " (%v)", s.AppliedAt.Format(time.RFC3339)) }
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ── Migrable ───────────────────────────────────────────────

// Migrable is a storage backend that supports migrations.
type Migrable interface {
	Migrate(r *Runner) error
	Rollback(r *Runner, steps int) error
	Status(r *Runner) []Status
}

// Apply runs registered migrations against a Migrable target.
func Apply(target Migrable, migrations ...*Migration) error {
	r := NewRunner()
	for _, m := range migrations { r.Register(m) }
	return target.Migrate(r)
}
