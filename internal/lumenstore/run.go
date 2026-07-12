package lumenstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrRunConflict       = errors.New("run already exists")
	ErrStoredRunNotFound = errors.New("stored run not found")
	ErrVersionConflict   = errors.New("run version conflict")
	ErrEventConflict     = errors.New("run event conflict")
)

// RunRecord is the storage-only representation of a run. Domain conversion
// belongs to runstate so this package remains independent of runtime policy.
type RunRecord struct {
	ID          string
	UserID      string
	WorkspaceID string
	SessionID   string
	ParentID    string
	Profile     string
	Title       string
	Status      string
	StopReason  string
	Error       string
	CreatedAt   string
	UpdatedAt   string
	StartedAt   string
	FinishedAt  string
	Version     uint64
}

type RunEventRecord struct {
	RunID     string
	Seq       uint64
	EventID   string
	Kind      string
	CreatedAt string
	Payload   string
}

func (s *Store) CreateRun(rec RunRecord) error {
	if s == nil || s.db == nil {
		return errors.New("lumenstore: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO runs (
		id, user_id, workspace_id, session_id, parent_run_id, profile, title, status, stop_reason, error,
		created_at, updated_at, started_at, finished_at, version
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.UserID, rec.WorkspaceID, rec.SessionID, rec.ParentID, rec.Profile, rec.Title, rec.Status,
		rec.StopReason, rec.Error, rec.CreatedAt, rec.UpdatedAt, rec.StartedAt,
		rec.FinishedAt, rec.Version,
	)
	if isUniqueConstraint(err) {
		return fmt.Errorf("%w: %s", ErrRunConflict, rec.ID)
	}
	return err
}

func (s *Store) UpdateRun(rec RunRecord, expectedVersion uint64) error {
	if s == nil || s.db == nil {
		return errors.New("lumenstore: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE runs SET
		user_id=?, workspace_id=?, session_id=?, parent_run_id=?, profile=?, title=?, status=?, stop_reason=?, error=?,
		created_at=?, updated_at=?, started_at=?, finished_at=?, version=?
		WHERE id=? AND version=?`,
		rec.UserID, rec.WorkspaceID, rec.SessionID, rec.ParentID, rec.Profile, rec.Title, rec.Status, rec.StopReason,
		rec.Error, rec.CreatedAt, rec.UpdatedAt, rec.StartedAt, rec.FinishedAt,
		rec.Version, rec.ID, expectedVersion,
	)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: id=%s expected=%d", ErrVersionConflict, rec.ID, expectedVersion)
	}
	return nil
}

func (s *Store) GetRun(id string) (RunRecord, error) {
	if s == nil || s.db == nil {
		return RunRecord{}, errors.New("lumenstore: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var rec RunRecord
	err := s.db.QueryRow(`SELECT id, user_id, workspace_id, session_id, parent_run_id, profile, title, status,
		stop_reason, error, created_at, updated_at, started_at, finished_at, version
		FROM runs WHERE id=?`, id).Scan(
		&rec.ID, &rec.UserID, &rec.WorkspaceID, &rec.SessionID, &rec.ParentID, &rec.Profile, &rec.Title, &rec.Status,
		&rec.StopReason, &rec.Error, &rec.CreatedAt, &rec.UpdatedAt, &rec.StartedAt,
		&rec.FinishedAt, &rec.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RunRecord{}, fmt.Errorf("%w: %s", ErrStoredRunNotFound, id)
	}
	return rec, err
}

func (s *Store) AppendRunEvent(rec RunEventRecord) error {
	if s == nil || s.db == nil {
		return errors.New("lumenstore: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO run_events (run_id, seq, event_id, kind, created_at, payload)
		VALUES (?, ?, ?, ?, ?, ?)`, rec.RunID, rec.Seq, rec.EventID, rec.Kind, rec.CreatedAt, rec.Payload)
	if isUniqueConstraint(err) {
		return fmt.Errorf("%w: run=%s seq=%d", ErrEventConflict, rec.RunID, rec.Seq)
	}
	return err
}

func (s *Store) LoadRunEvents(runID string, afterSeq uint64) ([]RunEventRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("lumenstore: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT run_id, seq, event_id, kind, created_at, payload
		FROM run_events WHERE run_id=? AND seq>? ORDER BY seq ASC`, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunEventRecord
	for rows.Next() {
		var rec RunEventRecord
		if err := rows.Scan(&rec.RunID, &rec.Seq, &rec.EventID, &rec.Kind, &rec.CreatedAt, &rec.Payload); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
