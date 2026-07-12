// Package pgstore implements the hosted Postgres persistence boundary.
package pgstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"lumen/internal/event"
	"lumen/internal/runstate"
)

var ErrVersionConflict = runstate.ErrVersionConflict

type Store struct{ db *sql.DB }

func Open(databaseURL string) (*Store, error) {
	if databaseURL == "" {
		return nil, errors.New("WORKBENCH_DATABASE_URL is empty")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect workbench postgres: %w", err)
	}
	return &Store{db: db}, nil
}
func New(db *sql.DB) *Store   { return &Store{db: db} }
func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateRun(r runstate.Run) error {
	_, err := s.db.Exec(`INSERT INTO workbench_runs
 (id,account_id,workspace_id,profile,status,version,title,request,error_message,created_at,started_at,finished_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, r.ID, r.UserID, r.WorkspaceID, r.Profile, r.Status, r.Version, r.Title,
		jsonValue(map[string]any{"session_id": r.SessionID, "parent_run_id": r.ParentID, "stop_reason": r.StopReason}), nullString(r.Error), r.CreatedAt, r.StartedAt, r.FinishedAt, r.UpdatedAt)
	return err
}
func (s *Store) UpdateRun(r runstate.Run, expected uint64) error {
	res, err := s.db.Exec(`UPDATE workbench_runs SET status=$1,version=$2,title=$3,
 request=request || $4::jsonb,error_message=$5,started_at=$6,finished_at=$7,updated_at=$8
 WHERE id=$9 AND account_id=$10 AND workspace_id=$11 AND version=$12`, r.Status, r.Version, r.Title,
		jsonValue(map[string]any{"session_id": r.SessionID, "parent_run_id": r.ParentID, "stop_reason": r.StopReason}), nullString(r.Error), r.StartedAt, r.FinishedAt, r.UpdatedAt, r.ID, r.UserID, r.WorkspaceID, expected)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrVersionConflict
	}
	return nil
}
func (s *Store) GetRun(id string) (runstate.Run, error) {
	var r runstate.Run
	var request []byte
	var errMsg sql.NullString
	err := s.db.QueryRow(`SELECT id,account_id::text,workspace_id::text,profile,title,status,version,request,error_message,created_at,updated_at,started_at,finished_at FROM workbench_runs WHERE id=$1`, id).
		Scan(&r.ID, &r.UserID, &r.WorkspaceID, &r.Profile, &r.Title, &r.Status, &r.Version, &request, &errMsg, &r.CreatedAt, &r.UpdatedAt, &r.StartedAt, &r.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("%w: %s", runstate.ErrRunNotFound, id)
	}
	if err != nil {
		return r, err
	}
	var meta struct {
		SessionID  string `json:"session_id"`
		ParentID   string `json:"parent_run_id"`
		StopReason string `json:"stop_reason"`
	}
	_ = json.Unmarshal(request, &meta)
	r.SessionID, r.ParentID, r.StopReason, r.Error = meta.SessionID, meta.ParentID, meta.StopReason, errMsg.String
	return r, nil
}
func (s *Store) AppendEvent(e event.Event) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	at := e.Timestamp
	if at.IsZero() {
		at = time.Now().UTC()
	}
	res, err := s.db.Exec(`INSERT INTO workbench_events(id,run_id,account_id,workspace_id,seq,type,payload,created_at)
 SELECT $1,$2,account_id,workspace_id,$3,$4,$5,$6 FROM workbench_runs WHERE id=$2
 ON CONFLICT(run_id,seq) DO NOTHING`, e.EventID, e.RunID, e.Seq, e.Kind, payload, at)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists bool
		if qerr := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM workbench_runs WHERE id=$1)`, e.RunID).Scan(&exists); qerr != nil {
			return qerr
		}
		if !exists {
			return fmt.Errorf("%w: %s", runstate.ErrRunNotFound, e.RunID)
		}
	}
	return nil
}
func (s *Store) Events(runID string, after uint64) ([]event.Event, error) {
	rows, err := s.db.Query(`SELECT seq,id,payload FROM workbench_events WHERE run_id=$1 AND seq>$2 ORDER BY seq`, runID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []event.Event
	for rows.Next() {
		var e event.Event
		var raw []byte
		if err = rows.Scan(&e.Seq, &e.EventID, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		e.RunID = runID
		out = append(out, e)
	}
	return out, rows.Err()
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func jsonValue(v any) []byte { b, _ := json.Marshal(v); return b }
