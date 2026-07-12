package artifact

import (
	"bytes"
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
	if s.Objects == nil {
		return errors.New("artifact object backend unavailable")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, _ := json.Marshal(r.Provenance)
	m, _ := json.Marshal(map[string]any{"path": r.Path})
	refs, _ := json.Marshal(r.InputRefs)
	res, err := tx.ExecContext(ctx, `INSERT INTO workbench_artifacts(id,run_id,account_id,workspace_id,name,kind,media_type,object_key,sha256,size_bytes,provenance,metadata,created_at,step_id,tool_call_id,model,input_refs)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT DO NOTHING`, r.ID, r.RunID, r.Owner.UserID, r.Owner.WorkspaceID, r.Name, "runtime", r.MIME, r.ObjectKey, r.SHA256, r.Size, p, m, r.CreatedAt, nullArtifact(r.StepID), nullArtifact(r.ToolCallID), nullArtifact(r.Model), refs)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var id, run, tc, key, sha string
		var size int64
		err = tx.QueryRowContext(ctx, `SELECT id,run_id,COALESCE(tool_call_id,''),object_key,sha256,size_bytes FROM workbench_artifacts WHERE id=$1 OR (run_id=$2 AND tool_call_id=$3) LIMIT 1`, r.ID, r.RunID, r.ToolCallID).Scan(&id, &run, &tc, &key, &sha, &size)
		if err != nil {
			return err
		}
		if id == r.ID && run == r.RunID && tc == r.ToolCallID && key == r.ObjectKey && sha == r.SHA256 && size == r.Size {
			return nil
		}
		return ErrConflict
	}
	if err = s.Objects.Put(ctx, r.ObjectKey, bytes.NewReader(b), r.Size, r.MIME); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		_ = s.Objects.Delete(ctx, r.ObjectKey)
		return err
	}
	return nil
}

func (s PostgresStore) Create(r Record) error {
	p, _ := json.Marshal(r.Provenance)
	m, _ := json.Marshal(map[string]any{"path": r.Path})
	refs, _ := json.Marshal(r.InputRefs)
	res, err := s.DB.Exec(`INSERT INTO workbench_artifacts(id,run_id,account_id,workspace_id,name,kind,media_type,object_key,sha256,size_bytes,provenance,metadata,created_at,step_id,tool_call_id,model,input_refs)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT DO NOTHING`, r.ID, r.RunID, r.Owner.UserID, r.Owner.WorkspaceID, r.Name, "runtime", r.MIME, r.ObjectKey, r.SHA256, r.Size, p, m, r.CreatedAt, nullArtifact(r.StepID), nullArtifact(r.ToolCallID), nullArtifact(r.Model), refs)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}
	var id, run, tc, key, sha string
	var size int64
	err = s.DB.QueryRow(`SELECT id,run_id,COALESCE(tool_call_id,''),object_key,sha256,size_bytes FROM workbench_artifacts WHERE id=$1 OR (run_id=$2 AND tool_call_id=$3) LIMIT 1`, r.ID, r.RunID, r.ToolCallID).Scan(&id, &run, &tc, &key, &sha, &size)
	if err != nil {
		return err
	}
	if id == r.ID && run == r.RunID && tc == r.ToolCallID && key == r.ObjectKey && sha == r.SHA256 && size == r.Size {
		return ErrDuplicate
	}
	return ErrConflict
}
func (s PostgresStore) Delete(ctx context.Context, o runstate.Owner, r Record) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM workbench_artifacts WHERE id=$1 AND run_id=$2 AND account_id=$3 AND workspace_id=$4 AND object_key=$5`, r.ID, r.RunID, o.UserID, o.WorkspaceID, r.ObjectKey)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if s.Objects != nil {
		return s.Objects.Delete(ctx, r.ObjectKey)
	}
	return nil
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
