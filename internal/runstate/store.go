package runstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"lumen/internal/event"
	"lumen/internal/lumenstore"
)

// SQLiteStore adapts lumenstore's storage-only records to runstate domain types.
type SQLiteStore struct {
	db *lumenstore.Store
}

func NewSQLiteStore(db *lumenstore.Store) *SQLiteStore {
	if db == nil {
		return nil
	}
	return &SQLiteStore{db: db}
}

func (s *SQLiteStore) CreateRun(run Run) error {
	if s == nil || s.db == nil {
		return errors.New("runstate: nil sqlite store")
	}
	return s.db.CreateRun(toRecord(run))
}

func (s *SQLiteStore) UpdateRun(run Run, expectedVersion uint64) error {
	if s == nil || s.db == nil {
		return errors.New("runstate: nil sqlite store")
	}
	return s.db.UpdateRun(toRecord(run), expectedVersion)
}

func (s *SQLiteStore) GetRun(id string) (Run, error) {
	if s == nil || s.db == nil {
		return Run{}, errors.New("runstate: nil sqlite store")
	}
	rec, err := s.db.GetRun(id)
	if errors.Is(err, lumenstore.ErrStoredRunNotFound) {
		return Run{}, fmt.Errorf("%w: %s", ErrRunNotFound, id)
	}
	if err != nil {
		return Run{}, err
	}
	return fromRecord(rec)
}

func (s *SQLiteStore) AppendEvent(ev event.Event) error {
	if s == nil || s.db == nil {
		return errors.New("runstate: nil sqlite store")
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	createdAt := ev.Timestamp
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return s.db.AppendRunEvent(lumenstore.RunEventRecord{
		RunID: ev.RunID, Seq: ev.Seq, EventID: ev.EventID, Kind: string(ev.Kind),
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), Payload: string(payload),
	})
}

func (s *SQLiteStore) Events(runID string, afterSeq uint64) ([]event.Event, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("runstate: nil sqlite store")
	}
	records, err := s.db.LoadRunEvents(runID, afterSeq)
	if err != nil {
		return nil, err
	}
	out := make([]event.Event, 0, len(records))
	for _, rec := range records {
		var ev event.Event
		if err := json.Unmarshal([]byte(rec.Payload), &ev); err != nil {
			return nil, fmt.Errorf("decode run event %s/%d: %w", rec.RunID, rec.Seq, err)
		}
		ev.RunID = rec.RunID
		ev.Seq = rec.Seq
		ev.EventID = rec.EventID
		out = append(out, ev)
	}
	return out, nil
}

func toRecord(run Run) lumenstore.RunRecord {
	return lumenstore.RunRecord{
		ID: run.ID, UserID: run.UserID, WorkspaceID: run.WorkspaceID, SessionID: run.SessionID, ParentID: run.ParentID,
		Profile: run.Profile, Title: run.Title, Status: string(run.Status),
		StopReason: run.StopReason, Error: run.Error,
		CreatedAt: formatTime(run.CreatedAt), UpdatedAt: formatTime(run.UpdatedAt),
		StartedAt: formatTimePtr(run.StartedAt), FinishedAt: formatTimePtr(run.FinishedAt),
		Version: run.Version,
	}
}

func fromRecord(rec lumenstore.RunRecord) (Run, error) {
	createdAt, err := parseRequiredTime("created_at", rec.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	updatedAt, err := parseRequiredTime("updated_at", rec.UpdatedAt)
	if err != nil {
		return Run{}, err
	}
	startedAt, err := parseOptionalTime("started_at", rec.StartedAt)
	if err != nil {
		return Run{}, err
	}
	finishedAt, err := parseOptionalTime("finished_at", rec.FinishedAt)
	if err != nil {
		return Run{}, err
	}
	return Run{
		ID: rec.ID, UserID: rec.UserID, WorkspaceID: rec.WorkspaceID, SessionID: rec.SessionID, ParentID: rec.ParentID,
		Profile: rec.Profile, Title: rec.Title, Status: Status(rec.Status),
		StopReason: rec.StopReason, Error: rec.Error,
		CreatedAt: createdAt, UpdatedAt: updatedAt, StartedAt: startedAt,
		FinishedAt: finishedAt, Version: rec.Version,
	}, nil
}

func formatTime(v time.Time) string { return v.UTC().Format(time.RFC3339Nano) }

func formatTimePtr(v *time.Time) string {
	if v == nil {
		return ""
	}
	return formatTime(*v)
}

func parseRequiredTime(field, value string) (time.Time, error) {
	v, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse run %s: %w", field, err)
	}
	return v, nil
}

func parseOptionalTime(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	v, err := parseRequiredTime(field, value)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
