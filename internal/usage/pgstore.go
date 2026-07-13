package usage

import (
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"lumen/internal/runstate"
)

type PostgresStore struct{ DB *sql.DB }

func (s PostgresStore) CreateUsage(r Record) error {
	_, err := s.DB.Exec(`INSERT INTO workbench_usage(run_id,event_id,account_id,workspace_id,provider,model,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,cost_microunits,occurred_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, r.RunID, r.EventID, r.UserID, r.WorkspaceID, r.Provider, r.Model, r.InputTokens, r.OutputTokens, r.CacheHitTokens, r.CacheMissTokens, r.EstimatedCostMicros, r.CreatedAt)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return ErrDuplicate
	}
	return err
}
func (s PostgresStore) ListRun(o runstate.Owner, run string) ([]Record, error) {
	rows, err := s.DB.Query(`SELECT event_id,run_id,account_id::text,workspace_id::text,provider,model,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,cost_microunits,occurred_at FROM workbench_usage WHERE run_id=$1 AND account_id=$2 AND workspace_id=$3 ORDER BY occurred_at`, run, o.UserID, o.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err = rows.Scan(&r.EventID, &r.RunID, &r.UserID, &r.WorkspaceID, &r.Provider, &r.Model, &r.InputTokens, &r.OutputTokens, &r.CacheHitTokens, &r.CacheMissTokens, &r.EstimatedCostMicros, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
