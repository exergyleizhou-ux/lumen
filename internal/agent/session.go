package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"lumen/internal/lumenstore"
	"lumen/internal/provider"
)

// Session holds the conversation history for one agent run.
// It is prepend-only: messages are only appended, never modified in place,
// so the prefix cache stays warm across turns.
type Session struct {
	mu         sync.Mutex
	Messages   []provider.Message
	Path       string // JSONL file path for persistence
	persistErr error  // first persistence failure, if any (guarded by mu)
}

// NewSession creates an empty session, optionally loading from path.
func NewSession(path string) *Session {
	s := &Session{Path: path}
	if path != "" {
		s.load()
	}
	return s
}

// Add appends a message to the session and persists it.
// The mutex is released before disk I/O to avoid blocking other
// session operations (Snapshot, Len) during slow filesystem writes.
func (s *Session) Add(m provider.Message) {
	s.mu.Lock()
	s.Messages = append(s.Messages, m)
	seq := len(s.Messages) - 1
	s.mu.Unlock()
	if err := s.appendToFile(m, seq); err != nil {
		s.mu.Lock()
		if s.persistErr == nil {
			s.persistErr = err
		}
		s.mu.Unlock()
	}
}

// PersistErr returns the first error encountered while persisting the session to
// disk, or nil. Callers surface it so a session that silently stops persisting
// (and thus won't resume) is not mistaken for a healthy one.
func (s *Session) PersistErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistErr
}

// Snapshot returns a copy of the current message list.
func (s *Session) Snapshot() []provider.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]provider.Message, len(s.Messages))
	copy(out, s.Messages)
	return out
}

// Len returns the number of messages.
func (s *Session) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Messages)
}

// SystemPrompt builds the cache-stable prefix: system message + tool schemas +
// project memory. It must return the same bytes every call for the prefix
// cache to stay hot.
func (s *Session) SystemPrompt(basePrompt, memory string) string {
	var sb strings.Builder
	sb.WriteString(basePrompt)
	if memory != "" {
		sb.WriteString("\n\n")
		sb.WriteString(memory)
	}
	return sb.String()
}

// DropLast removes the most recently appended message from memory, JSONL,
// and SQLite via persistLocked (same rewrite path as Compact).
func (s *Session) DropLast() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Messages) == 0 {
		return
	}
	s.Messages = s.Messages[:len(s.Messages)-1]
	if err := s.persistLocked(); err != nil && s.persistErr == nil {
		s.persistErr = err
	}
}

// DropTo truncates the session back to n messages (both memory and file).
func (s *Session) DropTo(n int) {
	for s.Len() > n {
		s.DropLast()
	}
}

// Compact drops the middle of the session, keeping the first keepFirst and
// last keepLast messages verbatim and inserting a short marker (the summary
// arg) in their place. This is a sliding window — the omitted messages are
// NOT model-summarized — chosen to keep the cache-stable prefix end intact.
func (s *Session) Compact(keepFirst, keepLast int, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Messages) <= keepFirst+keepLast {
		return
	}

	// Build compacted list: first N + summary + last N
	compacted := make([]provider.Message, 0, keepFirst+1+keepLast)
	compacted = append(compacted, s.Messages[:keepFirst]...)
	compacted = append(compacted, provider.Message{
		Role:    provider.RoleUser,
		Content: "[SESSION COMPACTED]\n\n" + summary,
	})
	compacted = append(compacted, s.Messages[len(s.Messages)-keepLast:]...)
	s.Messages = compacted

	// Keep JSONL + SQLite in sync with compacted memory (see persistLocked).
	if err := s.persistLocked(); err != nil && s.persistErr == nil {
		s.persistErr = err
	}
}

// persistLocked overwrites JSONL from memory and replaces all SQLite rows.
// The caller must hold s.mu. This is the sole rewrite entrypoint for mutations.
func (s *Session) persistLocked() error {
	if s.Path == "" {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, m := range s.Messages {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	if err := os.WriteFile(s.Path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return s.syncSQLiteLocked()
}

func (s *Session) syncSQLiteLocked() error {
	if s.Path == "" {
		return nil
	}
	db := lumenstore.Default()
	if db == nil {
		return nil
	}
	sid := lumenstore.SessionIDFromPath(s.Path)
	payloads := make([][]byte, len(s.Messages))
	roles := make([]string, len(s.Messages))
	for i, m := range s.Messages {
		b, _ := json.Marshal(m)
		payloads[i] = b
		roles[i] = string(m.Role)
	}
	return db.ReplaceSessionMessages(sid, payloads, roles)
}

// ── JSONL persistence ─────────────────────────────────────

func (s *Session) appendToFile(m provider.Message, seq int) error {
	if s.Path == "" {
		return nil
	}
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(m); err != nil {
		return err
	}
	if db := lumenstore.Default(); db != nil {
		b, _ := json.Marshal(m)
		sid := lumenstore.SessionIDFromPath(s.Path)
		_ = db.AppendSessionMessage(sid, seq, string(m.Role), b)
		_ = db.UpsertSessionMeta(sid, "")
	}
	return nil
}

func (s *Session) load() {
	if s.Path == "" {
		return
	}
	jsonlMsgs, jsonlState := s.loadFromJSONLWithMeta()
	db := lumenstore.Default()
	sid := lumenstore.SessionIDFromPath(s.Path)

	if db == nil {
		s.Messages = jsonlMsgs
		return
	}

	rows, err := db.LoadSessionMessages(sid)
	if err != nil {
		s.Messages = jsonlMsgs
		if len(jsonlMsgs) > 0 {
			_, _ = lumenstore.MigrateJSONLSessionFile(db, s.Path)
		}
		return
	}

	outcome := reconcileLoad(jsonlState, jsonlMsgs, rows)
	s.Messages = outcome.Messages
	if outcome.ClearSQLite {
		_ = replaceSQLiteMessages(db, sid, nil)
	} else if outcome.ReplaceSQLite {
		_ = replaceSQLiteMessages(db, sid, outcome.Messages)
	} else if outcome.MigrateJSONL {
		_, _ = lumenstore.MigrateJSONLSessionFile(db, s.Path)
	}
}

func replaceSQLiteMessages(db *lumenstore.Store, sid string, msgs []provider.Message) error {
	payloads := make([][]byte, len(msgs))
	roles := make([]string, len(msgs))
	for i, m := range msgs {
		b, _ := json.Marshal(m)
		payloads[i] = b
		roles[i] = string(m.Role)
	}
	return db.ReplaceSessionMessages(sid, payloads, roles)
}

func (s *Session) loadFromJSONLWithMeta() ([]provider.Message, jsonlFileState) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, jsonlAbsent
		}
		return nil, jsonlUnreadable
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, jsonlPresentEmpty
	}
	var out []provider.Message
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m provider.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, jsonlPresentEmpty
	}
	return out, jsonlPresentWithMessages
}
