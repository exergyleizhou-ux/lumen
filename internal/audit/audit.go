// Package audit provides an audit trail for all agent actions. It records
// tool calls, permission checks, configuration changes, and user interactions
// with tamper-evident hashing for compliance and forensics.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one audit record.
type Entry struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Actor     string            `json:"actor"`
	Action    string            `json:"action"`
	Resource  string            `json:"resource"`
	Result    string            `json:"result"`
	Details   map[string]any    `json:"details,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	TraceID   string            `json:"trace_id,omitempty"`
	Hash      string            `json:"hash"`
	PrevHash  string            `json:"prev_hash"`
}

// Trail is an append-only audit trail.
type Trail struct {
	mu       sync.Mutex
	entries  []*Entry
	maxSize  int
	lastHash string
}

// NewTrail creates an audit trail.
func NewTrail(maxSize int) *Trail {
	if maxSize <= 0 { maxSize = 10000 }
	return &Trail{maxSize: maxSize}
}

// Record adds an audit entry.
func (t *Trail) Record(actor, action, resource, result string, details map[string]any) *Entry {
	t.mu.Lock(); defer t.mu.Unlock()

	entry := &Entry{
		ID:        fmt.Sprintf("audit-%d", len(t.entries)+1),
		Timestamp: time.Now(),
		Actor:     actor,
		Action:    action,
		Resource:  resource,
		Result:    result,
		Details:   details,
		PrevHash:  t.lastHash,
	}

	// Compute tamper-evident hash
	h := sha256.New()
	data, _ := json.Marshal(map[string]any{
		"id": entry.ID, "ts": entry.Timestamp.UnixNano(), "actor": entry.Actor,
		"action": entry.Action, "resource": entry.Resource, "result": entry.Result,
		"prev": entry.PrevHash,
	})
	h.Write(data)
	entry.Hash = hex.EncodeToString(h.Sum(nil))
	t.lastHash = entry.Hash

	t.entries = append(t.entries, entry)
	if len(t.entries) > t.maxSize { t.entries = t.entries[1:] }
	return entry
}

// Query filters audit entries.
func (t *Trail) Query(actor, action, resource string, since, until time.Time) []*Entry {
	t.mu.Lock(); defer t.mu.Unlock()
	var out []*Entry
	for _, e := range t.entries {
		if actor != "" && e.Actor != actor { continue }
		if action != "" && e.Action != action { continue }
		if resource != "" && e.Resource != resource { continue }
		if !since.IsZero() && e.Timestamp.Before(since) { continue }
		if !until.IsZero() && e.Timestamp.After(until) { continue }
		out = append(out, e)
	}
	return out
}

// Verify checks the hash chain integrity.
func (t *Trail) Verify() (bool, []string) {
	t.mu.Lock(); defer t.mu.Unlock()
	var issues []string
	prevHash := ""
	for i, e := range t.entries {
		if i > 0 && e.PrevHash != prevHash {
			issues = append(issues, fmt.Sprintf("chain break at entry %d: expected prev=%s, got %s", i, prevHash, e.PrevHash))
		}
		h := sha256.New()
		data, _ := json.Marshal(map[string]any{
			"id": e.ID, "ts": e.Timestamp.UnixNano(), "actor": e.Actor,
			"action": e.Action, "resource": e.Resource, "result": e.Result,
			"prev": e.PrevHash,
		})
		h.Write(data)
		expected := hex.EncodeToString(h.Sum(nil))
		if e.Hash != expected {
			issues = append(issues, fmt.Sprintf("hash mismatch at entry %d", i))
		}
		prevHash = e.Hash
	}
	return len(issues) == 0, issues
}

// Stats returns audit statistics.
func (t *Trail) Stats() map[string]int64 {
	t.mu.Lock(); defer t.mu.Unlock()
	stats := map[string]int64{"total": int64(len(t.entries))}
	actorCounts := map[string]int64{}
	actionCounts := map[string]int64{}
	for _, e := range t.entries {
		actorCounts[e.Actor]++
		actionCounts[e.Action]++
	}
	for k, v := range actorCounts { stats["actor:"+k] = v }
	for k, v := range actionCounts { stats["action:"+k] = v }
	return stats
}

// FormatTrail formats audit trail entries.
func FormatTrail(entries []*Entry) string {
	if len(entries) == 0 { return "No audit entries.\n" }
	var sb strings.Builder
	fmt.Fprintf(&sb, "Audit Trail (%d entries):\n%s\n\n", len(entries), strings.Repeat("─", 70))
	for _, e := range entries {
		fmt.Fprintf(&sb, "  %s %s %s %s → %s\n", e.Timestamp.Format("15:04:05"), e.Actor, e.Action, e.Resource, e.Result)
		if e.Details != nil { fmt.Fprintf(&sb, "    details: %v\n", e.Details) }
	}
	return sb.String()
}

// ── Auditor ───────────────────────────────────────────────

// Auditor is a multi-trail audit manager.
type Auditor struct {
	mu     sync.Mutex
	trails map[string]*Trail
}

// NewAuditor creates an auditor.
func NewAuditor() *Auditor {
	return &Auditor{trails: map[string]*Trail{}}
}

// Trail returns or creates a named trail.
func (a *Auditor) Trail(name string) *Trail {
	a.mu.Lock(); defer a.mu.Unlock()
	if t, ok := a.trails[name]; ok { return t }
	t := NewTrail(5000)
	a.trails[name] = t
	return t
}

// Names returns all trail names.
func (a *Auditor) Names() []string {
	a.mu.Lock(); defer a.mu.Unlock()
	var out []string
	for n := range a.trails { out = append(out, n) }
	sort.Strings(out)
	return out
}

// ── Compliance Report ────────────────────────────────────

// ComplianceReport holds compliance summary data.
type ComplianceReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	Period      string    `json:"period"`
	TotalEvents int64     `json:"total_events"`
	ByAction    map[string]int64 `json:"by_action"`
	ByActor     map[string]int64 `json:"by_actor"`
	Failures    int64     `json:"failures"`
	IntegrityOK bool      `json:"integrity_ok"`
}

// GenerateComplianceReport creates a summary report from a trail.
func GenerateComplianceReport(trail *Trail, period string) *ComplianceReport {
	now := time.Now()
	var since time.Time
	switch period {
	case "day": since = now.Add(-24 * time.Hour)
	case "week": since = now.Add(-7 * 24 * time.Hour)
	case "month": since = now.Add(-30 * 24 * time.Hour)
	}

	entries := trail.Query("", "", "", since, now)
	ok, _ := trail.Verify()

	r := &ComplianceReport{GeneratedAt: now, Period: period, TotalEvents: int64(len(entries)), IntegrityOK: ok,
		ByAction: map[string]int64{}, ByActor: map[string]int64{}}

	for _, e := range entries {
		r.ByAction[e.Action]++
		r.ByActor[e.Actor]++
		if e.Result == "failure" || e.Result == "denied" { r.Failures++ }
	}
	return r
}

// FormatCompliance formats a compliance report.
func FormatCompliance(r *ComplianceReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Compliance Report (%s period)\n", r.Period)
	fmt.Fprintf(&sb, "Generated: %s\n%s\n\n", r.GeneratedAt.Format(time.RFC3339), strings.Repeat("─", 50))
	fmt.Fprintf(&sb, "  Total Events: %d\n", r.TotalEvents)
	fmt.Fprintf(&sb, "  Failures:     %d\n", r.Failures)
	if r.IntegrityOK { sb.WriteString("  Integrity:    ✅ OK\n") } else { sb.WriteString("  Integrity:    🔴 BROKEN\n") }
	if len(r.ByAction) > 0 {
		sb.WriteString("\n  By Action:\n")
		for act, c := range r.ByAction { fmt.Fprintf(&sb, "    %-20s %d\n", act, c) }
	}
	return sb.String()
}
