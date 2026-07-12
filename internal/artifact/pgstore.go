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

func (s PostgresStore) Persist(ctx context.Context, r Record, b []byte) error {
	return Persist(ctx, s, s.Objects, r, b)
}

func (s PostgresStore) Create(r Record) error {
	p, _ := json.Marshal(r.Provenance)
	m, _ := json.Marshal(map[string]any{"path": r.Path})
	refs, _ := json.Marshal(r.InputRefs)
	_, err := s.DB.Exec(`INSERT INTO workbench_artifacts(id,run_id,account_id,workspace_id,name,kind,media_type,object_key,sha256,size_bytes,provenance,metadata,created_at,step_id,tool_call_id,model,input_refs)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT(id) DO NOTHING`, r.ID, r.RunID, r.Owner.UserID, r.Owner.WorkspaceID, r.Name, "runtime", r.MIME, r.ObjectKey, r.SHA256, r.Size, p, m, r.CreatedAt, nullArtifact(r.StepID), nullArtifact(r.ToolCallID), nullArtifact(r.Model), refs)
	return err
}
func (s PostgresStore) ListRun(o runstate.Owner, run string) ([]Record, error) {
	rows, err := s.DB.Query(`SELECT id,run_id,account_id::text,workspace_id::text,name,media_type,object_key,sha256,size_bytes,provenance,metadata,created_at,COALESCE(step_id,''),COALESCE(tool_call_id,''),COALESCE(model,''),input_refs FROM workbench_artifacts WHERE run_id=$1 AND account_id=$2 AND workspace_id=$3 ORDER BY created_at`, run, o.UserID, o.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var p, m []byte
		var refs []byte
		if err = rows.Scan(&r.ID, &r.RunID, &r.Owner.UserID, &r.Owner.WorkspaceID, &r.Name, &r.MIME, &r.ObjectKey, &r.SHA256, &r.Size, &p, &m, &r.CreatedAt, &r.StepID, &r.ToolCallID, &r.Model, &refs); err != nil {
			return nil, err
		}
		json.Unmarshal(p, &r.Provenance)
		json.Unmarshal(refs, &r.InputRefs)
		var meta struct {
			Path string `json:"path"`
		}
		json.Unmarshal(m, &meta)
		r.Path = meta.Path
		out = append(out, r)
	}
	return out, rows.Err()
}
func nullArtifact(v string) any {
	if v == "" {
		return nil
	}
	return v
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
