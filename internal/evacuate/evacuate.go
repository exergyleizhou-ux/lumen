// Package evacuate provides graceful connection draining, request
// shedding, and traffic redirection for agent node shutdown and
// load management.
package evacuate

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type State int

const (
	StateActive State = iota
	StateDraining
	StateDrained
	StateOffline
)

func (s State) String() string {
	switch s {
	case StateActive:
		return "active"
	case StateDraining:
		return "draining"
	case StateDrained:
		return "drained"
	case StateOffline:
		return "offline"
	default:
		return "unknown"
	}
}

type Conn struct {
	ID         string
	CreatedAt  time.Time
	LastActive time.Time
	BytesIn    int64
	BytesOut   int64
}
type Drainer struct {
	mu           sync.Mutex
	state        State
	conns        map[string]*Conn
	drainTimeout time.Duration
	maxDrain     time.Duration
	drainStarted time.Time
	onDrained    func()
}

func NewDrainer(timeout, maxDrain time.Duration) *Drainer {
	return &Drainer{state: StateActive, conns: map[string]*Conn{}, drainTimeout: timeout, maxDrain: maxDrain}
}
func (d *Drainer) OnDrained(fn func()) { d.mu.Lock(); defer d.mu.Unlock(); d.onDrained = fn }
func (d *Drainer) AddConn(id string) *Conn {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := &Conn{ID: id, CreatedAt: time.Now(), LastActive: time.Now()}
	d.conns[id] = c
	return c
}
func (d *Drainer) RemoveConn(id string) { d.mu.Lock(); defer d.mu.Unlock(); delete(d.conns, id) }
func (d *Drainer) TouchConn(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.conns[id]; ok {
		c.LastActive = time.Now()
	}
}
func (d *Drainer) Drain() {
	d.mu.Lock()
	d.state = StateDraining
	d.drainStarted = time.Now()
	d.mu.Unlock()
	go func() {
		deadline := time.Now().Add(d.maxDrain)
		for {
			time.Sleep(100 * time.Millisecond)
			d.mu.Lock()
			if len(d.conns) == 0 {
				d.state = StateDrained
				d.mu.Unlock()
				if d.onDrained != nil {
					d.onDrained()
				}
				return
			}
			if time.Now().After(deadline) {
				d.state = StateDrained
				d.mu.Unlock()
				if d.onDrained != nil {
					d.onDrained()
				}
				return
			}
			now := time.Now()
			for id, c := range d.conns {
				if now.Sub(c.LastActive) > d.drainTimeout {
					delete(d.conns, id)
				}
			}
			d.mu.Unlock()
		}
	}()
}
func (d *Drainer) State() State   { d.mu.Lock(); defer d.mu.Unlock(); return d.state }
func (d *Drainer) ConnCount() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.conns) }
func (d *Drainer) FormatStatus() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Evacuation Status: %s\n%s\n\n", d.state, strings.Repeat("─", 40))
	fmt.Fprintf(&sb, "  Connections: %d\n", len(d.conns))
	if d.state == StateDraining {
		fmt.Fprintf(&sb, "  Draining for: %v\n", time.Since(d.drainStarted).Round(time.Millisecond))
	}
	return sb.String()
}

type Redirector struct {
	mu      sync.Mutex
	targets []string
	idx     int
	healthy map[string]bool
}

func NewRedirector(targets []string) *Redirector {
	r := &Redirector{targets: targets, healthy: map[string]bool{}}
	for _, t := range targets {
		r.healthy[t] = true
	}
	return r
}
func (r *Redirector) Next() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := 0; i < len(r.targets); i++ {
		r.idx = (r.idx + 1) % len(r.targets)
		if r.healthy[r.targets[r.idx]] {
			return r.targets[r.idx]
		}
	}
	return ""
}
func (r *Redirector) MarkUnhealthy(target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healthy[target] = false
}
func (r *Redirector) MarkHealthy(target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healthy[target] = true
}
func (r *Redirector) Targets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for t := range r.healthy {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
