// Package contextmgr manages hierarchical contexts with cancellation and
// deadline propagation for sub-agent orchestration. Each context carries
// metadata (turn number, tool call ID, agent name) for tracing.
package contextmgr

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Metadata carried in agent contexts.
type Metadata struct {
	Turn        int       `json:"turn"`
	ToolCallID  string    `json:"tool_call_id,omitempty"`
	AgentName   string    `json:"agent_name,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	DeadlineAt  time.Time `json:"deadline_at,omitempty"`
}

type ctxKey struct{}

// WithMetadata attaches agent metadata to a context.
func WithMetadata(ctx context.Context, m *Metadata) context.Context {
	return context.WithValue(ctx, ctxKey{}, m)
}

// GetMetadata extracts agent metadata from a context.
func GetMetadata(ctx context.Context) *Metadata {
	if m, ok := ctx.Value(ctxKey{}).(*Metadata); ok { return m }
	return nil
}

// Manager creates and tracks agent contexts.
type Manager struct {
	mu      sync.Mutex
	active  map[string]context.CancelFunc
	seq     int64
}

// NewManager creates a context manager.
func NewManager() *Manager { return &Manager{active: map[string]context.CancelFunc{}} }

// NewTurn creates a context for a new agent turn with a default timeout.
func (m *Manager) NewTurn(parent context.Context, turn int, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	ctx = WithMetadata(ctx, &Metadata{Turn: turn, StartedAt: time.Now(), DeadlineAt: time.Now().Add(timeout)})
	m.mu.Lock()
	m.active[fmt.Sprintf("turn-%d", turn)] = cancel
	m.mu.Unlock()
	return ctx, cancel
}

// NewSubAgent creates a context for a sub-agent with a tighter timeout.
func (m *Manager) NewSubAgent(parent context.Context, toolCallID, agentName string, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	meta := GetMetadata(parent)
	if meta == nil { meta = &Metadata{StartedAt: time.Now()} }
	newMeta := *meta
	newMeta.ToolCallID = toolCallID
	newMeta.AgentName = agentName
	newMeta.DeadlineAt = time.Now().Add(timeout)
	ctx = WithMetadata(ctx, &newMeta)
	m.mu.Lock()
	m.active[toolCallID] = cancel
	m.mu.Unlock()
	return ctx, cancel
}

// Cancel stops a tracked context by key.
func (m *Manager) Cancel(key string) {
	m.mu.Lock()
	if cancel, ok := m.active[key]; ok { cancel(); delete(m.active, key) }
	m.mu.Unlock()
}

// CancelAll stops all tracked contexts.
func (m *Manager) CancelAll() {
	m.mu.Lock(); defer m.mu.Unlock()
	for _, cancel := range m.active { cancel() }
	m.active = map[string]context.CancelFunc{}
}

// ActiveCount returns the number of active contexts.
func (m *Manager) ActiveCount() int {
	m.mu.Lock(); defer m.mu.Unlock()
	return len(m.active)
}

// WithCancelOnDone returns a context that is cancelled when the parent's done
// channel is closed. Useful for chaining sub-agent lifecycles.
func WithCancelOnDone(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

