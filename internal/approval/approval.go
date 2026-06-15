// Package approval manages hierarchical approval workflows for tool calls,
// file changes, and configuration updates. Supports multi-level approval
// with auto-approve rules, expiration, and audit trail. Used for Plan Mode
// approvals, dangerous command confirmation, and configuration changes.
package approval

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level defines the severity of an approval request.
type Level int

const (
	LevelInfo     Level = 0
	LevelWarning  Level = 1
	LevelCritical Level = 2
)

// Status is the lifecycle state of an approval request.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusDenied   Status = "denied"
	StatusExpired  Status = "expired"
	StatusAuto     Status = "auto_approved"
)

// Request is one thing that needs approval.
type Request struct {
	ID          string            `json:"id"`
	Level       Level             `json:"level"`
	Tool        string            `json:"tool"`
	Description string            `json:"description"`
	Args        string            `json:"args,omitempty"`
	Status      Status            `json:"status"`
	ApprovedBy  string            `json:"approved_by,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
	ResolvedAt  time.Time         `json:"resolved_at,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

// Rule defines an auto-approve or auto-deny condition.
type Rule struct {
	Name      string          `json:"name"`
	Priority  int             `json:"priority"`
	ToolMatch string          `json:"tool_match"` // glob pattern
	Condition func(*Request) bool `json:"-"`
	Action    string          `json:"action"` // "approve", "deny", "ask"
}

// Manager handles approval workflows.
type Manager struct {
	mu       sync.Mutex
	pending  []*Request
	history  []*Request
	maxHist  int
	rules    []Rule
	onChange func(*Request)
}

// NewManager creates an approval manager.
func NewManager() *Manager {
	return &Manager{maxHist: 500}
}

// AddRule registers an auto-approve/deny rule.
func (m *Manager) AddRule(r Rule) {
	m.mu.Lock(); defer m.mu.Unlock()
	m.rules = append(m.rules, r)
	sort.Slice(m.rules, func(i, j int) bool { return m.rules[i].Priority > m.rules[j].Priority })
}

// Request submits an approval request. Returns immediately; the caller
// should check Status after the callback fires.
func (m *Manager) Request(level Level, tool, description, args string, ttl time.Duration) *Request {
	req := &Request{
		ID: fmt.Sprintf("appr-%d", time.Now().UnixNano()),
		Level: level, Tool: tool, Description: description, Args: args,
		Status: StatusPending, CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ttl),
	}

	// Check auto-rules
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rule := range m.rules {
		if matchGlob(rule.ToolMatch, tool) {
			if rule.Condition == nil || rule.Condition(req) {
				switch rule.Action {
				case "approve":
					req.Status = StatusAuto
					req.ApprovedBy = "rule:" + rule.Name
				case "deny":
					req.Status = StatusDenied
					req.Reason = "auto-denied by rule: " + rule.Name
				}
				req.ResolvedAt = time.Now()
				m.archive(req)
				if m.onChange != nil { m.onChange(req) }
				return req
			}
		}
	}

	m.pending = append(m.pending, req)
	return req
}

// Approve marks a request as approved.
func (m *Manager) Approve(id, by, reason string) error {
	m.mu.Lock(); defer m.mu.Unlock()
	for i, r := range m.pending {
		if r.ID == id {
			r.Status = StatusApproved
			r.ApprovedBy = by
			r.Reason = reason
			r.ResolvedAt = time.Now()
			m.pending = append(m.pending[:i], m.pending[i+1:]...)
			m.archive(r)
			if m.onChange != nil { m.onChange(r) }
			return nil
		}
	}
	return fmt.Errorf("request %q not found", id)
}

// Deny marks a request as denied.
func (m *Manager) Deny(id, by, reason string) error {
	m.mu.Lock(); defer m.mu.Unlock()
	for i, r := range m.pending {
		if r.ID == id {
			r.Status = StatusDenied
			r.ApprovedBy = by
			r.Reason = reason
			r.ResolvedAt = time.Now()
			m.pending = append(m.pending[:i], m.pending[i+1:]...)
			m.archive(r)
			if m.onChange != nil { m.onChange(r) }
			return nil
		}
	}
	return fmt.Errorf("request %q not found", id)
}

// ExpireOld rejects requests past their expiration time.
func (m *Manager) ExpireOld() int {
	m.mu.Lock(); defer m.mu.Unlock()
	now := time.Now()
	var kept []*Request
	expired := 0
	for _, r := range m.pending {
		if now.After(r.ExpiresAt) {
			r.Status = StatusExpired
			r.ResolvedAt = now
			m.archive(r)
			expired++
		} else {
			kept = append(kept, r)
		}
	}
	m.pending = kept
	return expired
}

// Pending returns all unresolved requests.
func (m *Manager) Pending() []*Request {
	m.mu.Lock(); defer m.mu.Unlock()
	out := make([]*Request, len(m.pending))
	copy(out, m.pending)
	return out
}

// History returns recent resolved requests.
func (m *Manager) History(limit int) []*Request {
	m.mu.Lock(); defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.history) { limit = len(m.history) }
	out := make([]*Request, limit)
	copy(out, m.history[len(m.history)-limit:])
	return out
}

// OnChange registers a callback for request resolution.
func (m *Manager) OnChange(fn func(*Request)) { m.onChange = fn }

func (m *Manager) archive(r *Request) {
	m.history = append(m.history, r)
	if len(m.history) > m.maxHist { m.history = m.history[len(m.history)-m.maxHist:] }
}

func matchGlob(pattern, name string) bool {
	if pattern == "*" || pattern == "" { return true }
	return strings.EqualFold(pattern, name)
}

// FormatPending formats pending requests for display.
func (m *Manager) FormatPending() string {
	pending := m.Pending()
	if len(pending) == 0 { return "No pending approval requests.\n" }
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d pending approval(s):\n\n", len(pending))
	for _, r := range pending {
		icon := "⚠️"
		if r.Level == LevelCritical { icon = "🔴" }
		fmt.Fprintf(&sb, "%s [%s] %s — %s (expires %s)\n",
			icon, r.ID, r.Tool, r.Description, r.ExpiresAt.Format("15:04:05"))
	}
	return sb.String()
}

// ApproveAll approves all pending requests with the given reason.
func (m *Manager) ApproveAll(by, reason string) int {
	count := 0
	for _, r := range m.Pending() {
		if err := m.Approve(r.ID, by, reason); err == nil { count++ }
	}
	return count
}

// Count returns the number of pending requests.
func (m *Manager) Count() int {
	m.mu.Lock(); defer m.mu.Unlock()
	return len(m.pending)
}
