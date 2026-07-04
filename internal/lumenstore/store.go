// Package lumenstore provides optional SQLite persistence for audit and session metadata.
package lumenstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const EnvSQLite = "LUMEN_SQLITE_STORE"

// Store is a small SQLite backend (~/.lumen/lumen.db by default).
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// DefaultPath returns the default SQLite database path.
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvSQLite); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".lumen", "lumen.db"), nil
}

// Open opens (and migrates) a SQLite database at path.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL,
			session TEXT,
			tool TEXT NOT NULL,
			ok INTEGER NOT NULL,
			payload TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_events(ts)`,
		`CREATE TABLE IF NOT EXISTS session_meta (
			id TEXT PRIMARY KEY,
			title TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS session_messages (
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (session_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_session_messages_sid ON session_messages(session_id)`,
		`CREATE TABLE IF NOT EXISTS science_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			template_id TEXT,
			base_url TEXT,
			updated_at TEXT NOT NULL,
			payload TEXT NOT NULL
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// InsertAudit appends one audit row.
func (s *Store) InsertAudit(session, tool string, ok bool, payload map[string]any) error {
	if s == nil || s.db == nil {
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(
		`INSERT INTO audit_events (ts, session, tool, ok, payload) VALUES (?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), session, tool, boolToInt(ok), string(b),
	)
	return err
}

// UpsertSessionMeta records session title/metadata.
func (s *Store) UpsertSessionMeta(id, title string) error {
	if s == nil || s.db == nil || id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO session_meta (id, title, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET title=excluded.title, updated_at=excluded.updated_at`,
		id, title, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// CountAudit returns total audit rows (for health checks).
func (s *Store) CountAudit() (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_events`).Scan(&n)
	return n, err
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

var (
	defaultStore *Store
	defaultOnce  sync.Once
	defaultErr   error
	defaultMu    sync.Mutex
)

// ResetDefaultForTest clears the process-wide store so tests can re-init Default()
// after changing LUMEN_SQLITE_STORE.
func ResetDefaultForTest() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultStore != nil {
		_ = defaultStore.Close()
	}
	defaultStore = nil
	defaultErr = nil
	defaultOnce = sync.Once{}
}

// Default opens the process-wide store when LUMEN_SQLITE_STORE is on or a path.
// Unset = disabled (JSONL-only). Set off/0/false to force disable.
func Default() *Store {
	defaultOnce.Do(func() {
		path, ok := resolveSQLitePath()
		if !ok {
			return
		}
		defaultStore, defaultErr = Open(path)
	})
	return defaultStore
}

func resolveSQLitePath() (string, bool) {
	v := strings.TrimSpace(os.Getenv(EnvSQLite))
	if v == "" {
		return "", false
	}
	switch strings.ToLower(v) {
	case "off", "0", "false", "none", "disabled":
		return "", false
	case "on", "1", "true", "enabled":
		p, err := DefaultPath()
		if err != nil {
			defaultErr = err
			return "", false
		}
		return p, true
	default:
		return v, true
	}
}

// DefaultErr reports initialization failure for Default().
func DefaultErr() error { return defaultErr }