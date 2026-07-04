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

// DropLast removes the most recently appended message from both the
// in-memory slice and the JSONL file. Used to undo a user message when
// the turn failed before an assistant reply could be added.
func (s *Session) DropLast() {
	s.mu.Lock()
	if len(s.Messages) == 0 {
		s.mu.Unlock()
		return
	}
	s.Messages = s.Messages[:len(s.Messages)-1]
	s.mu.Unlock()

	if s.Path == "" {
		return
	}
	// Truncate JSONL: remove the last line
	data, err := os.ReadFile(s.Path)
	if err != nil || len(data) == 0 {
		return
	}
	// Find last complete line boundary (skip trailing newline if any)
	end := len(data)
	if data[end-1] == '\n' {
		end--
	}
	lastNL := -1
	for i := end - 1; i >= 0; i-- {
		if data[i] == '\n' {
			lastNL = i
			break
		}
	}
	if lastNL >= 0 {
		os.WriteFile(s.Path, data[:lastNL+1], 0o644)
	} else {
		os.WriteFile(s.Path, nil, 0o644) // only one line — clear
	}
	s.syncSQLiteFromMemory()
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

	// Keep the persisted JSONL in sync with the compacted memory — otherwise the
	// file keeps the dropped middle (growing unbounded) and a resume replays a
	// transcript that diverges from what the model actually saw.
	if err := s.rewriteFileLocked(); err != nil && s.persistErr == nil {
		s.persistErr = err
	}
}

// rewriteFileLocked overwrites the JSONL with the current in-memory messages.
// The caller must hold s.mu. No-op when persistence is disabled.
func (s *Session) rewriteFileLocked() error {
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

func (s *Session) syncSQLiteFromMemory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.syncSQLiteLocked()
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
	if db := lumenstore.Default(); db != nil {
		sid := lumenstore.SessionIDFromPath(s.Path)
		if rows, err := db.LoadSessionMessages(sid); err == nil && len(rows) > 0 {
			for _, row := range rows {
				var m provider.Message
				if json.Unmarshal(row, &m) == nil {
					s.Messages = append(s.Messages, m)
				}
			}
			return
		}
	}
	s.Messages = s.loadFromJSONL()
	if db := lumenstore.Default(); db != nil && len(s.Messages) > 0 {
		_, _ = lumenstore.MigrateJSONLSessionFile(db, s.Path)
	}
}

func (s *Session) loadFromJSONL() []provider.Message {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil
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
	return out
}
