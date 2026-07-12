package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"lumen/internal/runstate"
)

type PostgresStore struct {
	DB      *sql.DB
	Objects ObjectBackend
}

func (s PostgresStore) Create(r Record) error {
	p, _ := json.Marshal(r.Provenance)
	m, _ := json.Marshal(map[string]any{"step_id": r.StepID, "tool_call_id": r.ToolCallID, "path": r.Path, "model": r.Model, "input_refs": r.InputRefs})
	_, err := s.DB.Exec(`INSERT INTO workbench_artifacts(id,run_id,account_id,workspace_id,name,kind,media_type,object_key,sha256,size_bytes,provenance,metadata,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, r.ID, r.RunID, r.Owner.UserID, r.Owner.WorkspaceID, r.Name, "runtime", r.MIME, r.ObjectKey, r.SHA256, r.Size, p, m, r.CreatedAt)
	return err
}
func (s PostgresStore) ListRun(o runstate.Owner, run string) ([]Record, error) {
	rows, err := s.DB.Query(`SELECT id,run_id,account_id::text,workspace_id::text,name,media_type,object_key,sha256,size_bytes,provenance,metadata,created_at FROM workbench_artifacts WHERE run_id=$1 AND account_id=$2 AND workspace_id=$3 ORDER BY created_at`, run, o.UserID, o.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var p, m []byte
		if err = rows.Scan(&r.ID, &r.RunID, &r.Owner.UserID, &r.Owner.WorkspaceID, &r.Name, &r.MIME, &r.ObjectKey, &r.SHA256, &r.Size, &p, &m, &r.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(p, &r.Provenance)
		var meta struct {
			StepID     string   `json:"step_id"`
			ToolCallID string   `json:"tool_call_id"`
			Path       string   `json:"path"`
			Model      string   `json:"model"`
			InputRefs  []string `json:"input_refs"`
		}
		json.Unmarshal(m, &meta)
		r.StepID, r.ToolCallID, r.Path, r.Model, r.InputRefs = meta.StepID, meta.ToolCallID, meta.Path, meta.Model, meta.InputRefs
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s PostgresStore) Open(ctx context.Context, o runstate.Owner, r Record) (io.ReadCloser, error) {
	if r.Owner != o {
		return nil, ErrNotFound
	}
	if s.Objects == nil {
		return nil, errors.New("artifact object backend unavailable")
	}
	return s.Objects.Get(ctx, r.ObjectKey)
}
