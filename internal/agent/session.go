package agent

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	"lumen/internal/provider"
)

// Session holds the conversation history for one agent run.
// It is prepend-only: messages are only appended, never modified in place,
// so the prefix cache stays warm across turns.
type Session struct {
	mu       sync.Mutex
	Messages []provider.Message
	Path     string // JSONL file path for persistence
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
func (s *Session) Add(m provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, m)
	s.appendToFile(m)
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
}

// ── JSONL persistence ─────────────────────────────────────

func (s *Session) appendToFile(m provider.Message) {
	if s.Path == "" {
		return
	}
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	json.NewEncoder(f).Encode(m)
}

func (s *Session) load() {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m provider.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		s.Messages = append(s.Messages, m)
	}
}
