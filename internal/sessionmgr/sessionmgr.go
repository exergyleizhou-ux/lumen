// Package sessionmgr manages agent sessions: listing, resuming, deleting,
// and exporting session data. It provides the foundation for the session
// management slash commands and the TUI session browser.
// Adapted from Reasonix's session control.
package sessionmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Info describes one saved session.
type Info struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Messages  int       `json:"messages"`
	Tokens    int64     `json:"tokens"`
	Model     string    `json:"model"`
	Provider  string    `json:"provider"`
	Active    bool      `json:"active"`
}

// Manager manages saved sessions on disk.
type Manager struct {
	mu  sync.Mutex
	dir string
}

// NewManager creates a session manager storing data under the given directory.
func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("session dir: %w", err)
	}
	return &Manager{dir: dir}, nil
}

// List returns all saved sessions, sorted by most recent first.
func (m *Manager) List() ([]Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, err
	}

	var sessions []Info
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := m.loadInfo(filepath.Join(m.dir, e.Name()))
		if err != nil {
			continue
		}
		sessions = append(sessions, info)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

func (m *Manager) loadInfo(path string) (Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, err
	}
	info.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	return info, nil
}

// Save persists session metadata.
func (m *Manager) Save(info Info) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info.UpdatedAt = time.Now()
	data, _ := json.MarshalIndent(info, "", "  ")
	path := filepath.Join(m.dir, info.ID+".json")
	return os.WriteFile(path, data, 0o644)
}

// Load reads a session by ID.
func (m *Manager) Load(id string) (Info, error) {
	return m.loadInfo(filepath.Join(m.dir, id+".json"))
}

// Delete removes a session by ID.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return os.Remove(filepath.Join(m.dir, id+".json"))
}

// ResumePath returns the JSONL transcript path for a session.
func (m *Manager) ResumePath(id string) string {
	return filepath.Join(m.dir, id+".jsonl")
}

// ExportJSONL reads a session transcript and returns its content.
func (m *Manager) ExportJSONL(id string) (string, error) {
	data, err := os.ReadFile(m.ResumePath(id))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// NewID generates a unique session ID.
func NewID() string {
	return fmt.Sprintf("sess-%d", time.Now().UnixNano())
}

// TitleFromPrompt extracts a short title from the first user message.
func TitleFromPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if len(prompt) <= 60 {
		return prompt
	}
	return prompt[:57] + "..."
}

// FormatList formats session infos as a human-readable table.
func FormatList(sessions []Info) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d session(s)\n", len(sessions)))
	sb.WriteString(strings.Repeat("─", 80) + "\n")
	for _, s := range sessions {
		icon := " "
		if s.Active {
			icon = "●"
		}
		age := time.Since(s.UpdatedAt).Truncate(time.Minute)
		fmt.Fprintf(&sb, "%s %-36s %4d msgs %5s ago  %s/%s\n",
			icon, s.Title, s.Messages, shortDuration(age), s.Provider, s.Model)
	}
	return sb.String()
}

func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ── Session resume/restore ─────────────────────────────────

// SessionStore persists and restores session JSONL transcripts.
type SessionStore struct {
	dir string
	mu  sync.Mutex
}

// NewSessionStore creates a store for session transcripts.
func NewSessionStore(dir string) (*SessionStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &SessionStore{dir: dir}, nil
}

// SaveTranscript writes a session transcript to disk.
func (s *SessionStore) SaveTranscript(id string, messages []map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Create(filepath.Join(s.dir, id+".jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, m := range messages {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	return nil
}

// LoadTranscript reads a session transcript from disk.
func (s *SessionStore) LoadTranscript(id string) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(filepath.Join(s.dir, id+".jsonl"))
	if err != nil {
		return nil, err
	}

	var messages []map[string]any
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		messages = append(messages, m)
	}
	return messages, nil
}

// DeleteTranscript removes a session transcript.
func (s *SessionStore) DeleteTranscript(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(filepath.Join(s.dir, id+".jsonl"))
}

// ListTranscripts returns all saved transcript IDs.
func (s *SessionStore) ListTranscripts() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".jsonl"))
		}
	}
	return ids, nil
}
