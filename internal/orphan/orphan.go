// Package orphan detects unused resources (orphans) across the agent
// system: dangling sessions, stale files, unused model connections, and
// expired caches. It provides configurable cleanup policies.
package orphan

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Resource is a tracked resource that may become orphaned.
type Resource struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Owner     string            `json:"owner,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	LastUsed  time.Time         `json:"last_used"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// Policy defines when a resource is considered orphaned.
type Policy struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	MaxIdle   time.Duration `json:"max_idle"`
	MaxAge    time.Duration `json:"max_age"`
	AutoClean bool          `json:"auto_clean"`
}

// DefaultPolicies returns standard orphan detection policies.
func DefaultPolicies() []Policy {
	return []Policy{
		{Name: "stale-sessions", Type: "session", MaxIdle: 1 * time.Hour, MaxAge: 24 * time.Hour, AutoClean: true},
		{Name: "temp-files", Type: "file", MaxIdle: 30 * time.Minute, MaxAge: 6 * time.Hour, AutoClean: true},
		{Name: "idle-connections", Type: "connection", MaxIdle: 5 * time.Minute, MaxAge: 1 * time.Hour, AutoClean: false},
		{Name: "expired-caches", Type: "cache", MaxAge: 12 * time.Hour, AutoClean: true},
	}
}

// Detector finds orphaned resources.
type Detector struct {
	mu        sync.RWMutex
	resources map[string]*Resource
	policies  map[string]Policy
	cleanFn   func(*Resource) error
}

// NewDetector creates an orphan detector.
func NewDetector() *Detector {
	return &Detector{resources: map[string]*Resource{}, policies: map[string]Policy{}}
}

// RegisterResource adds a tracked resource.
func (d *Detector) RegisterResource(r *Resource) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r.LastUsed = time.Now()
	d.resources[r.ID] = r
}

// Touch updates the last-used time of a resource.
func (d *Detector) Touch(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.resources[id]; ok {
		r.LastUsed = time.Now()
	}
}

// Remove stops tracking a resource.
func (d *Detector) Remove(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.resources, id)
}

// AddPolicy registers an orphan detection policy.
func (d *Detector) AddPolicy(p Policy) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.policies[p.Name] = p
}

// SetCleanup registers the cleanup function.
func (d *Detector) SetCleanup(fn func(*Resource) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cleanFn = fn
}

// Scan finds orphaned resources based on policies.
func (d *Detector) Scan() []*Resource { d.mu.RLock(); defer d.mu.RUnlock(); return d.scanLocked() }

// scanLocked assumes the caller holds the lock.
func (d *Detector) scanLocked() []*Resource {
	now := time.Now()
	var orphans []*Resource

	for _, r := range d.resources {
		for _, p := range d.policies {
			if p.Type != "" && p.Type != r.Type {
				continue
			}
			if p.MaxIdle > 0 && now.Sub(r.LastUsed) > p.MaxIdle {
				orphans = append(orphans, r)
				break
			}
			if p.MaxAge > 0 && now.Sub(r.CreatedAt) > p.MaxAge {
				orphans = append(orphans, r)
				break
			}
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].LastUsed.Before(orphans[j].LastUsed) })
	return orphans
}

// Clean removes orphaned resources.
func (d *Detector) Clean(orphans []*Resource) (cleaned int, errors []error) {
	d.mu.Lock()
	cleanFn := d.cleanFn
	d.mu.Unlock()

	for _, o := range orphans {
		var clean bool
		d.mu.RLock()
		for _, p := range d.policies {
			if p.Type == o.Type && p.AutoClean {
				clean = true
				break
			}
		}
		d.mu.RUnlock()
		if !clean {
			continue
		}

		if cleanFn != nil {
			if err := cleanFn(o); err != nil {
				errors = append(errors, err)
				continue
			}
		}
		d.mu.Lock()
		delete(d.resources, o.ID)
		d.mu.Unlock()
		cleaned++
	}
	return
}

// Stats returns orphan statistics by type.
func (d *Detector) Stats() map[string]int { d.mu.RLock(); defer d.mu.RUnlock(); return d.statsLocked() }

// statsLocked assumes the caller holds the lock.
func (d *Detector) statsLocked() map[string]int {
	stats := map[string]int{}
	for _, r := range d.resources {
		stats[r.Type]++
	}
	return stats
}

// Report formats an orphan report.
func (d *Detector) Report() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var sb strings.Builder
	stats := d.statsLocked()
	orphans := d.scanLocked()

	fmt.Fprintf(&sb, "Orphan Report: %d total resources, %d orphans\n%s\n\n", len(d.resources), len(orphans), strings.Repeat("─", 50))

	fmt.Fprintf(&sb, "By Type:\n")
	for typ, count := range stats {
		fmt.Fprintf(&sb, "  %-15s %d\n", typ, count)
	}

	if len(orphans) > 0 {
		fmt.Fprintf(&sb, "\nOrphans:\n")
		for _, o := range orphans {
			idle := time.Since(o.LastUsed)
			fmt.Fprintf(&sb, "  🔴 %s [%s] idle=%v age=%v\n", o.ID, o.Type, idle.Round(time.Second), time.Since(o.CreatedAt).Round(time.Second))
		}
	}
	return sb.String()
}
