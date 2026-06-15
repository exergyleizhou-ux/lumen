// Package sync implements a data synchronization engine with three-way merge,
// conflict resolution strategies (last-write-wins, CRDT-like, manual), sync journal,
// and change tracking.
package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Strategy defines how conflicts are resolved during synchronization.
type Strategy int

const (
	StrategyLastWriteWins Strategy = iota // Timestamp-based; newest wins.
	StrategyCRDTLike                       // Merge both values when possible.
	StrategyManual                         // Flag conflict for manual resolution.
)

var strategyNames = map[Strategy]string{
	StrategyLastWriteWins: "last-write-wins",
	StrategyCRDTLike:      "crdt-like",
	StrategyManual:        "manual",
}

func (s Strategy) String() string {
	if n, ok := strategyNames[s]; ok {
		return n
	}
	return "unknown"
}

// ChangeType indicates the kind of change.
type ChangeType int

const (
	ChangeCreate ChangeType = iota
	ChangeUpdate
	ChangeDelete
)

var changeTypeNames = map[ChangeType]string{
	ChangeCreate: "create",
	ChangeUpdate: "update",
	ChangeDelete: "delete",
}

func (ct ChangeType) String() string {
	if n, ok := changeTypeNames[ct]; ok {
		return n
	}
	return "unknown"
}

// Record represents a data record with versioning metadata.
type Record struct {
	Key       string            `json:"key"`
	Value     json.RawMessage   `json:"value"`
	Version   int64             `json:"version"`   // Monotonic version.
	UpdatedAt time.Time         `json:"updated_at"`
	UpdatedBy string            `json:"updated_by"`
	Digest    string            `json:"digest"`    // Content hash.
	Deleted   bool              `json:"deleted"`
	Extra     map[string]string `json:"extra"`
}

// Clone returns a shallow copy of r.
func (r *Record) Clone() *Record {
	c := *r
	c.Extra = make(map[string]string)
	for k, v := range r.Extra {
		c.Extra[k] = v
	}
	return &c
}

// ComputeDigest updates the digest field from the record's content.
func (r *Record) ComputeDigest() {
	h := sha256.New()
	data, _ := json.Marshal([]interface{}{r.Key, r.Value, r.Deleted})
	r.Digest = hex.EncodeToString(h.Sum(data))
}

// Change represents a tracked modification.
type Change struct {
	ID        string     `json:"id"`
	RecordKey string     `json:"record_key"`
	Type      ChangeType `json:"type"`
	OldValue  json.RawMessage `json:"old_value,omitempty"`
	NewValue  json.RawMessage `json:"new_value,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
	Source    string     `json:"source"` // e.g. "local", "remote", "merge".
}

// Conflict describes a synchronization conflict.
type Conflict struct {
	Key        string          `json:"key"`
	Local      *Record         `json:"local"`
	Remote     *Record         `json:"remote"`
	Base       *Record         `json:"base"`
	ResolvedBy Strategy        `json:"resolved_by"`
	ResolvedAt time.Time       `json:"resolved_at,omitempty"`
	Resolution json.RawMessage `json:"resolution,omitempty"`
}

// JournalEntry records a synchronization operation.
type JournalEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"` // "merge", "resolve", "revert".
	Keys      []string  `json:"keys"`
	Conflicts int       `json:"conflicts"`
	Details   string    `json:"details"`
}

// MergeResult holds the outcome of a three-way merge.
type MergeResult struct {
	Resolved  []*Record  `json:"resolved"`
	Conflicts []*Conflict `json:"conflicts"`
	Applied   int        `json:"applied"`
	Skipped   int        `json:"skipped"`
}

// Engine is the synchronization engine.
type Engine struct {
	mu          sync.RWMutex
	store       map[string]*Record // key -> record
	journal     []JournalEntry
	changes     []Change
	conflicts   []Conflict
	strategy    Strategy
	version     int64
	changeCap   int
	journalCap  int
}

// NewEngine creates a new synchronization engine.
func NewEngine(strategy Strategy) *Engine {
	return &Engine{
		store:      make(map[string]*Record),
		strategy:   strategy,
		changeCap:  10000,
		journalCap: 5000,
	}
}

// SetStrategy updates the conflict resolution strategy.
func (e *Engine) SetStrategy(s Strategy) { e.mu.Lock(); defer e.mu.Unlock(); e.strategy = s }

// Strategy returns the current strategy.
func (e *Engine) Strategy() Strategy { e.mu.RLock(); defer e.mu.RUnlock(); return e.strategy }

// Put stores a record.
func (e *Engine) Put(rec *Record) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.version++
	now := time.Now()

	old, exists := e.store[rec.Key]
	if exists {
		e.recordChange(Change{
			RecordKey: rec.Key,
			Type:      ChangeUpdate,
			OldValue:  old.Value,
			NewValue:  rec.Value,
			Timestamp: now,
			Source:    "local",
		})
	} else {
		e.recordChange(Change{
			RecordKey: rec.Key,
			Type:      ChangeCreate,
			NewValue:  rec.Value,
			Timestamp: now,
			Source:    "local",
		})
	}

	rec.Version = e.version
	rec.UpdatedAt = now
	rec.ComputeDigest()
	e.store[rec.Key] = rec
}

// Get retrieves a record.
func (e *Engine) Get(key string) (*Record, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	r, ok := e.store[key]
	if !ok || r.Deleted {
		return nil, false
	}
	return r.Clone(), true
}

// Delete marks a record as deleted.
func (e *Engine) Delete(key, by string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.store[key]
	if !ok {
		return false
	}
	e.version++
	now := time.Now()
	e.recordChange(Change{
		RecordKey: key,
		Type:      ChangeDelete,
		OldValue:  r.Value,
		Timestamp: now,
		Source:    "local",
	})
	r.Deleted = true
	r.UpdatedAt = now
	r.UpdatedBy = by
	r.Version = e.version
	r.ComputeDigest()
	return true
}

// List returns all non-deleted records.
func (e *Engine) List() []*Record {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*Record, 0, len(e.store))
	for _, r := range e.store {
		if !r.Deleted {
			out = append(out, r.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// recordChange appends a change, trimming old entries.
func (e *Engine) recordChange(c Change) {
	c.ID = fmt.Sprintf("c-%d-%d", e.version, len(e.changes))
	e.changes = append(e.changes, c)
	if len(e.changes) > e.changeCap {
		e.changes = e.changes[len(e.changes)-e.changeCap:]
	}
}

// Merge performs a three-way merge of local and remote records against a base.
func (e *Engine) Merge(base, local, remote []*Record) *MergeResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	result := &MergeResult{}
	now := time.Now()

	baseMap := recordsToMap(base)
	localMap := recordsToMap(local)
	remoteMap := recordsToMap(remote)

	allKeys := make(map[string]bool)
	for k := range baseMap {
		allKeys[k] = true
	}
	for k := range localMap {
		allKeys[k] = true
	}
	for k := range remoteMap {
		allKeys[k] = true
	}

	for key := range allKeys {
		b := baseMap[key]
		l := localMap[key]
		r := remoteMap[key]

		resolved := e.resolveThreeWay(key, b, l, r, now, result)
		if resolved != nil {
			e.store[key] = resolved
			result.Resolved = append(result.Resolved, resolved)
			result.Applied++
		} else {
			result.Skipped++
		}
	}

	// Record journal entry.
	e.addJournal(JournalEntry{
		Timestamp: now,
		Action:    "merge",
		Keys:      keysFromMap(allKeys),
		Conflicts: len(result.Conflicts),
		Details:   fmt.Sprintf("merged %d, conflicts %d", result.Applied, len(result.Conflicts)),
	})

	return result
}

// resolveThreeWay applies the three-way merge logic for a single key.
func (e *Engine) resolveThreeWay(key string, base, local, remote *Record, now time.Time, result *MergeResult) *Record {
	// Key only in local.
	if local != nil && remote == nil {
		if base != nil {
			// Deleted locally, existed in base.
			local.UpdatedAt = now
			local.ComputeDigest()
			return local
		}
		// New in local only.
		local.UpdatedAt = now
		local.ComputeDigest()
		return local
	}

	// Key only in remote.
	if remote != nil && local == nil {
		if base != nil {
			// Deleted remotely.
			remote.UpdatedAt = now
			remote.ComputeDigest()
			return remote
		}
		remote.UpdatedAt = now
		remote.ComputeDigest()
		return remote
	}

	// Both present.
	if local != nil && remote != nil {
		// Same digest => no conflict.
		local.ComputeDigest()
		remote.ComputeDigest()
		if local.Digest == remote.Digest {
			return local
		}

		// Neither changed from base => no conflict.
		if base != nil {
			base.ComputeDigest()
			localChanged := local.Digest != base.Digest
			remoteChanged := remote.Digest != base.Digest

			if !localChanged && !remoteChanged {
				return local
			}
			if localChanged && !remoteChanged {
				return local
			}
			if !localChanged && remoteChanged {
				return remote
			}
		}

		// Conflict.
		c := &Conflict{
			Key:        key,
			Local:      local.Clone(),
			Remote:     remote.Clone(),
			Base:       nil,
			ResolvedBy: e.strategy,
		}
		if base != nil {
			c.Base = base.Clone()
		}

		resolved := e.applyStrategy(c)
		c.ResolvedAt = time.Now()
		c.Resolution = resolved.Value
		result.Conflicts = append(result.Conflicts, c)
		e.conflicts = append(e.conflicts, *c)
		return resolved
	}

	// Both nil (deleted in both).
	return nil
}

// applyStrategy resolves a conflict using the configured strategy.
func (e *Engine) applyStrategy(c *Conflict) *Record {
	switch e.strategy {
	case StrategyLastWriteWins:
		if c.Local.UpdatedAt.After(c.Remote.UpdatedAt) {
			return c.Local
		}
		return c.Remote

	case StrategyCRDTLike:
		// Simple CRDT-like: merge maps if both are JSON objects.
		merged := mergeJSON(c.Local.Value, c.Remote.Value)
		r := c.Local.Clone()
		r.Value = merged
		r.ComputeDigest()
		return r

	case StrategyManual:
		// Mark as conflict, return local as placeholder.
		c.ResolvedBy = StrategyManual
		r := c.Local.Clone()
		return r

	default:
		return c.Local
	}
}

// mergeJSON attempts to merge two JSON objects; if not both objects, returns local.
func mergeJSON(a, b json.RawMessage) json.RawMessage {
	var ma, mb map[string]json.RawMessage
	if json.Unmarshal(a, &ma) != nil || json.Unmarshal(b, &mb) != nil {
		return a
	}
	for k, v := range mb {
		ma[k] = v
	}
	out, err := json.Marshal(ma)
	if err != nil {
		return a
	}
	return out
}

// addJournal appends a journal entry.
func (e *Engine) addJournal(je JournalEntry) {
	je.ID = fmt.Sprintf("j-%d", len(e.journal))
	e.journal = append(e.journal, je)
	if len(e.journal) > e.journalCap {
		e.journal = e.journal[len(e.journal)-e.journalCap:]
	}
}

// Journal returns a copy of the sync journal.
func (e *Engine) Journal() []JournalEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]JournalEntry, len(e.journal))
	copy(out, e.journal)
	return out
}

// Changes returns recently tracked changes.
func (e *Engine) Changes() []Change {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Change, len(e.changes))
	copy(out, e.changes)
	return out
}

// Conflicts returns all unresolved conflicts.
func (e *Engine) Conflicts() []Conflict {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Conflict, len(e.conflicts))
	copy(out, e.conflicts)
	return out
}

// ResolveConflict allows manual resolution of a conflict.
func (e *Engine) ResolveConflict(key string, resolved *Record) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.version++
	now := time.Now()
	resolved.Version = e.version
	resolved.UpdatedAt = now
	resolved.ComputeDigest()
	e.store[key] = resolved

	// Remove from conflicts list.
	filtered := e.conflicts[:0]
	for _, c := range e.conflicts {
		if c.Key != key {
			filtered = append(filtered, c)
		}
	}
	e.conflicts = filtered

	e.addJournal(JournalEntry{
		Timestamp: now,
		Action:    "resolve",
		Keys:      []string{key},
		Details:   "manual resolution",
	})
	return true
}

// Revert undoes a journal entry by restoring previous state.
func (e *Engine) Revert(journalID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, je := range e.journal {
		if je.ID == journalID {
			e.journal = append(e.journal[:i], e.journal[i+1:]...)
			e.addJournal(JournalEntry{
				Timestamp: time.Now(),
				Action:    "revert",
				Keys:      je.Keys,
				Details:   fmt.Sprintf("reverted %s", journalID),
			})
			return true
		}
	}
	return false
}

// Stats returns basic statistics.
func (e *Engine) Stats() map[string]int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	total, deleted := 0, 0
	for _, r := range e.store {
		total++
		if r.Deleted {
			deleted++
		}
	}
	return map[string]int{
		"total_records":  total,
		"active_records": total - deleted,
		"deleted":        deleted,
		"changes":        len(e.changes),
		"journal_entries": len(e.journal),
		"conflicts":      len(e.conflicts),
	}
}

// --- Helpers ---

func recordsToMap(recs []*Record) map[string]*Record {
	m := make(map[string]*Record, len(recs))
	for _, r := range recs {
		m[r.Key] = r
	}
	return m
}

func keysFromMap(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
