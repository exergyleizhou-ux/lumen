// Package signal provides OS signal handling with graceful shutdown
// coordination, timeout management, and ordered cleanup hooks for the
// Lumen agent lifecycle.
package signal

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Hook is a cleanup function with a priority. Lower numbers run first.
type Hook struct {
	Name     string
	Priority int
	Fn       func() error
}

// Manager coordinates graceful shutdown.
type Manager struct {
	mu       sync.Mutex
	hooks    []Hook
	timeout  time.Duration
	shutdown chan os.Signal
	done     chan struct{}
	latch    sync.Once
}

// NewManager creates a signal manager.
func NewManager(timeout time.Duration) *Manager {
	return &Manager{hooks: []Hook{}, timeout: timeout, shutdown: make(chan os.Signal, 1), done: make(chan struct{})}
}

// OnSignal registers a cleanup hook.
func (m *Manager) OnSignal(name string, priority int, fn func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = append(m.hooks, Hook{Name: name, Priority: priority, Fn: fn})
	sort.Slice(m.hooks, func(i, j int) bool { return m.hooks[i].Priority < m.hooks[j].Priority })
}

// Wait blocks until a signal is received, then runs cleanup.
func (m *Manager) Wait(signals ...os.Signal) {
	if len(signals) == 0 {
		signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT}
	}
	signal.Notify(m.shutdown, signals...)
	<-m.shutdown
	fmt.Fprintf(os.Stderr, "\nShutting down...\n")

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.runHooks()
	}()

	select {
	case <-done:
		fmt.Fprintf(os.Stderr, "Graceful shutdown complete.\n")
	case <-time.After(m.timeout):
		fmt.Fprintf(os.Stderr, "Shutdown timed out after %v.\n", m.timeout)
	}
}

// Shutdown triggers immediate graceful shutdown.
func (m *Manager) Shutdown() {
	m.latch.Do(func() {
		close(m.shutdown)
		m.runHooks()
		close(m.done)
	})
}

func (m *Manager) runHooks() {
	m.mu.Lock()
	hooks := make([]Hook, len(m.hooks))
	copy(hooks, m.hooks)
	m.mu.Unlock()

	for _, h := range hooks {
		fmt.Fprintf(os.Stderr, "  Stopping %s...\n", h.Name)
		if err := h.Fn(); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  %s: %v\n", h.Name, err)
		}
	}
}

// ListHooks returns registered hooks in execution order.
func (m *Manager) ListHooks() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, h := range m.hooks {
		out = append(out, fmt.Sprintf("%s (priority=%d)", h.Name, h.Priority))
	}
	return out
}

// FormatHooks formats the hook list.
func (m *Manager) FormatHooks() string {
	hooks := m.ListHooks()
	if len(hooks) == 0 {
		return "No shutdown hooks registered.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Shutdown Hooks (%d):\n%s\n\n", len(hooks), strings.Repeat("─", 40))
	for i, h := range hooks {
		fmt.Fprintf(&sb, "  %d. %s\n", i+1, h)
	}
	return sb.String()
}

// ── Health Pinger ─────────────────────────────────────────

// Pinger is a simple health check that can be used with signal management.
type Pinger struct {
	mu       sync.Mutex
	alive    bool
	lastPing time.Time
}

// NewPinger creates a health pinger.
func NewPinger() *Pinger { return &Pinger{alive: true, lastPing: time.Now()} }

// Ping marks as alive.
func (p *Pinger) Ping() { p.mu.Lock(); defer p.mu.Unlock(); p.alive = true; p.lastPing = time.Now() }

// Alive reports health status.
func (p *Pinger) Alive() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.alive }

// LastPing returns the time of last ping.
func (p *Pinger) LastPing() time.Time { p.mu.Lock(); defer p.mu.Unlock(); return p.lastPing }
