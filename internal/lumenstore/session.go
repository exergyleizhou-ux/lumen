package lumenstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppendSessionMessage stores one message for a session (dual-write companion to JSONL).
func (s *Store) AppendSessionMessage(sessionID string, seq int, role string, payload []byte) error {
	if s == nil || s.db == nil || sessionID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO session_messages (session_id, seq, role, payload, created_at)
		 VALUES (?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(session_id, seq) DO UPDATE SET role=excluded.role, payload=excluded.payload, created_at=excluded.created_at`,
		sessionID, seq, role, string(payload),
	)
	return err
}

// LoadSessionMessages returns all messages for a session ordered by seq.
func (s *Store) LoadSessionMessages(sessionID string) ([][]byte, error) {
	if s == nil || s.db == nil || sessionID == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT payload FROM session_messages WHERE session_id=? ORDER BY seq ASC`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, []byte(p))
	}
	return out, rows.Err()
}

// CountSessionMessages returns message count for a session.
func (s *Store) CountSessionMessages(sessionID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM session_messages WHERE session_id=?`, sessionID).Scan(&n)
	return n, err
}

// MigrateJSONLSessions imports ~/.lumen/history/*.jsonl into SQLite (idempotent per line).
// When db is nil, uses Default() if LUMEN_SQLITE_STORE is enabled.
func MigrateJSONLSessions(db *Store, histDir string) (int, error) {
	if db == nil {
		db = Default()
	}
	if db == nil {
		return 0, nil
	}
	entries, err := os.ReadDir(histDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	imported := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".jsonl")
		data, err := os.ReadFile(filepath.Join(histDir, e.Name()))
		if err != nil {
			return imported, err
		}
		seq := 0
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var probe struct {
				Role string `json:"role"`
			}
			if json.Unmarshal([]byte(line), &probe) != nil {
				continue
			}
			if err := db.AppendSessionMessage(sid, seq, probe.Role, []byte(line)); err != nil {
				return imported, fmt.Errorf("%s seq %d: %w", sid, seq, err)
			}
			seq++
			imported++
		}
	}
	return imported, nil
}

// SessionIDFromPath derives a stable session id from a JSONL history path.
func SessionIDFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".jsonl")
}
