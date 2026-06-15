// Package sessiondb provides persistent session storage with SQLite-backed
// session management, message archival, and queryable session history.
// Supports CRUD operations, pagination, search, and export.
package sessiondb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store persists agent sessions to SQLite.
type Store struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

// NewStore opens or creates a session database.
func NewStore(path string) (*Store, error) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	db, err := sql.Open("sqlite3", path)
	if err != nil { return nil, fmt.Errorf("open db: %w", err) }
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil { return nil, err }
	return s, nil
}

func (s *Store) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY, title TEXT, provider TEXT, model TEXT,
			created_at INTEGER, updated_at INTEGER, message_count INTEGER,
			total_tokens INTEGER, status TEXT, tags TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT, role TEXT,
			content TEXT, tool_calls TEXT, timestamp INTEGER, turn INTEGER,
			FOREIGN KEY(session_id) REFERENCES sessions(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_msg_session ON messages(session_id, turn)`,
		`CREATE INDEX IF NOT EXISTS idx_sess_updated ON sessions(updated_at DESC)`,
	}
	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil { return fmt.Errorf("migrate: %w", err) }
	}
	return nil
}

// SessionRecord is a row in the sessions table.
type SessionRecord struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	TotalTokens  int64     `json:"total_tokens"`
	Status       string    `json:"status"`
	Tags         []string  `json:"tags,omitempty"`
}

// MessageRecord is a row in the messages table.
type MessageRecord struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolCalls string    `json:"tool_calls,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Turn      int       `json:"turn"`
}

// ── CRUD ──────────────────────────────────────────────────

// CreateSession inserts a new session.
func (s *Store) CreateSession(sess *SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tagsJSON, _ := json.Marshal(sess.Tags)
	_, err := s.db.Exec(
		`INSERT INTO sessions (id,title,provider,model,created_at,updated_at,message_count,total_tokens,status,tags)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		sess.ID, sess.Title, sess.Provider, sess.Model,
		sess.CreatedAt.Unix(), sess.UpdatedAt.Unix(),
		sess.MessageCount, sess.TotalTokens, sess.Status, string(tagsJSON),
	)
	return err
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(id string) (*SessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row := s.db.QueryRow(
		`SELECT id,title,provider,model,created_at,updated_at,message_count,total_tokens,status,tags
		 FROM sessions WHERE id=?`, id,
	)
	var sess SessionRecord
	var created, updated int64
	var tagsStr string
	err := row.Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &created, &updated, &sess.MessageCount, &sess.TotalTokens, &sess.Status, &tagsStr)
	if err != nil { return nil, err }
	sess.CreatedAt = time.Unix(created, 0)
	sess.UpdatedAt = time.Unix(updated, 0)
	json.Unmarshal([]byte(tagsStr), &sess.Tags)
	return &sess, nil
}

// UpdateSession modifies an existing session.
func (s *Store) UpdateSession(sess *SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tagsJSON, _ := json.Marshal(sess.Tags)
	_, err := s.db.Exec(
		`UPDATE sessions SET title=?,updated_at=?,message_count=?,total_tokens=?,status=?,tags=? WHERE id=?`,
		sess.Title, time.Now().Unix(), sess.MessageCount, sess.TotalTokens, sess.Status, string(tagsJSON), sess.ID,
	)
	return err
}

// DeleteSession removes a session and its messages.
func (s *Store) DeleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`DELETE FROM messages WHERE session_id=?`, id)
	s.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return nil
}

// ListSessions returns sessions with pagination.
func (s *Store) ListSessions(offset, limit int, status string) ([]*SessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	query := `SELECT id,title,provider,model,created_at,updated_at,message_count,total_tokens,status,tags FROM sessions`
	args := []any{}
	if status != "" {
		query += ` WHERE status=?`
		args = append(args, status)
	}
	query += ` ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	var sessions []*SessionRecord
	for rows.Next() {
		var sess SessionRecord
		var created, updated int64
		var tagsStr string
		rows.Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &created, &updated, &sess.MessageCount, &sess.TotalTokens, &sess.Status, &tagsStr)
		sess.CreatedAt = time.Unix(created, 0)
		sess.UpdatedAt = time.Unix(updated, 0)
		json.Unmarshal([]byte(tagsStr), &sess.Tags)
		sessions = append(sessions, &sess)
	}
	return sessions, nil
}

// SearchSessions searches by title or tags.
func (s *Store) SearchSessions(query string, limit int) ([]*SessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT id,title,provider,model,created_at,updated_at,message_count,total_tokens,status,tags
		 FROM sessions WHERE title LIKE ? OR tags LIKE ?
		 ORDER BY updated_at DESC LIMIT ?`,
		"%"+query+"%", "%"+query+"%", limit,
	)
	if err != nil { return nil, err }
	defer rows.Close()
	var sessions []*SessionRecord
	for rows.Next() {
		var sess SessionRecord
		var created, updated int64
		var tagsStr string
		rows.Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &created, &updated, &sess.MessageCount, &sess.TotalTokens, &sess.Status, &tagsStr)
		sess.CreatedAt = time.Unix(created, 0)
		sess.UpdatedAt = time.Unix(updated, 0)
		json.Unmarshal([]byte(tagsStr), &sess.Tags)
		sessions = append(sessions, &sess)
	}
	return sessions, nil
}

// ── Messages ──────────────────────────────────────────────

// AddMessage inserts a message for a session.
func (s *Store) AddMessage(msg *MessageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO messages (session_id,role,content,tool_calls,timestamp,turn)
		 VALUES (?,?,?,?,?,?)`,
		msg.SessionID, msg.Role, msg.Content, msg.ToolCalls, msg.Timestamp.Unix(), msg.Turn,
	)
	return err
}

// GetMessages retrieves all messages for a session, ordered by turn.
func (s *Store) GetMessages(sessionID string) ([]*MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		`SELECT id,session_id,role,content,tool_calls,timestamp,turn
		 FROM messages WHERE session_id=? ORDER BY turn ASC`, sessionID,
	)
	if err != nil { return nil, err }
	defer rows.Close()
	var msgs []*MessageRecord
	for rows.Next() {
		var m MessageRecord
		var ts int64
		rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.ToolCalls, &ts, &m.Turn)
		m.Timestamp = time.Unix(ts, 0)
		msgs = append(msgs, &m)
	}
	return msgs, nil
}

// Count returns the total number of sessions.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count)
	return count
}

// TotalTokens returns the sum of tokens across all sessions.
func (s *Store) TotalTokens() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total int64
	s.db.QueryRow(`SELECT COALESCE(SUM(total_tokens),0) FROM sessions`).Scan(&total)
	return total
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// FormatSessions formats session records for display.
func FormatSessions(sessions []*SessionRecord) string {
	if len(sessions) == 0 { return "No sessions.\n" }
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d session(s):\n\n", len(sessions))
	for _, s := range sessions {
		age := time.Since(s.UpdatedAt).Truncate(time.Minute)
		tags := ""
		if len(s.Tags) > 0 { tags = " [" + strings.Join(s.Tags, ", ") + "]" }
		fmt.Fprintf(&sb, "  %-30s %s/%s %dmsgs %dtok %s ago%s\n",
			s.Title, s.Provider, s.Model, s.MessageCount, s.TotalTokens, age, tags)
	}
	return sb.String()
}

// ── In-memory fallback ────────────────────────────────────

// MemoryStore provides an in-memory session store when SQLite is unavailable.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionRecord
	messages map[string][]*MessageRecord
}

// NewMemoryStore creates an in-memory session store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: map[string]*SessionRecord{}, messages: map[string][]*MessageRecord{}}
}

func (m *MemoryStore) CreateSession(sess *SessionRecord) error {
	m.mu.Lock(); defer m.mu.Unlock()
	sess.CreatedAt = time.Now(); sess.UpdatedAt = sess.CreatedAt
	m.sessions[sess.ID] = sess; return nil
}

func (m *MemoryStore) GetSession(id string) (*SessionRecord, error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	if s, ok := m.sessions[id]; ok { return s, nil }
	return nil, fmt.Errorf("session %q not found", id)
}

func (m *MemoryStore) ListSessions(offset, limit int, status string) ([]*SessionRecord, error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	var all []*SessionRecord
	for _, s := range m.sessions {
		if status == "" || s.Status == status { all = append(all, s) }
	}
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt.After(all[j].UpdatedAt) })
	if offset >= len(all) { return nil, nil }
	end := offset + limit
	if end > len(all) { end = len(all) }
	return all[offset:end], nil
}

func (m *MemoryStore) AddMessage(msg *MessageRecord) error {
	m.mu.Lock(); defer m.mu.Unlock()
	m.messages[msg.SessionID] = append(m.messages[msg.SessionID], msg)
	if s, ok := m.sessions[msg.SessionID]; ok {
		s.MessageCount = len(m.messages[msg.SessionID])
		s.UpdatedAt = time.Now()
	}
	return nil
}

func (m *MemoryStore) GetMessages(sessionID string) ([]*MessageRecord, error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	return m.messages[sessionID], nil
}

func (m *MemoryStore) Count() int {
	m.mu.RLock(); defer m.mu.RUnlock()
	return len(m.sessions)
}

