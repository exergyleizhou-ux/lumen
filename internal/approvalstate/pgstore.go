package approvalstate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"lumen/internal/runstate"
	"time"
)

type PostgresStore struct{ DB *sql.DB }

func (s PostgresStore) Create(a Approval) error {
	effects, _ := json.Marshal(a.Effects)
	scope, _ := json.Marshal(a.FileScope)
	nets, _ := json.Marshal(a.NetworkTargets)
	outs, _ := json.Marshal(a.ExpectedOutputs)
	_, err := s.DB.Exec(`INSERT INTO workbench_approvals(approval_id,run_id,tool_call_id,account_id,workspace_id,owner,risk_level,reason,effects,command,file_scope,remote_target,network_targets,estimated_cost,expected_outputs,args_hash,editable_args,version,created_at,expires_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, a.ID, a.RunID, a.ToolCallID, a.Owner.UserID, a.Owner.WorkspaceID, a.Owner.UserID+":"+a.Owner.WorkspaceID, a.RiskLevel, a.Reason, effects, null(a.Command), scope, null(a.RemoteTarget), nets, a.EstimatedCostMicros, outs, a.ArgsHash, a.EditableArgs, a.Version, a.CreatedAt, a.ExpiresAt)
	return err
}
func (s PostgresStore) Get(o runstate.Owner, id string) (Approval, error) {
	var a Approval
	var effects, scope, nets, outs, editable []byte
	var command, remote, by sql.NullString
	var decision sql.NullString
	err := s.DB.QueryRow(`SELECT approval_id,run_id,tool_call_id,account_id::text,workspace_id::text,risk_level,reason,effects,command,file_scope,remote_target,network_targets,estimated_cost,expected_outputs,args_hash,editable_args,version,created_at,expires_at,decided_at,decided_by::text,decision FROM workbench_approvals WHERE approval_id=$1 AND account_id=$2 AND workspace_id=$3`, id, o.UserID, o.WorkspaceID).Scan(&a.ID, &a.RunID, &a.ToolCallID, &a.Owner.UserID, &a.Owner.WorkspaceID, &a.RiskLevel, &a.Reason, &effects, &command, &scope, &remote, &nets, &a.EstimatedCostMicros, &outs, &a.ArgsHash, &editable, &a.Version, &a.CreatedAt, &a.ExpiresAt, &a.DecidedAt, &by, &decision)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	json.Unmarshal(effects, &a.Effects)
	json.Unmarshal(scope, &a.FileScope)
	json.Unmarshal(nets, &a.NetworkTargets)
	json.Unmarshal(outs, &a.ExpectedOutputs)
	a.Command = command.String
	a.RemoteTarget = remote.String
	a.EditableArgs = editable
	a.DecidedBy = by.String
	if decision.Valid {
		d := Decision(decision.String)
		a.Decision = &d
	}
	return a, nil
}
func (s PostgresStore) Decide(o runstate.Owner, id string, d Decision, by string, now time.Time) (Approval, error) {
	res, err := s.DB.Exec(`UPDATE workbench_approvals SET decision=$1,decided_at=$2,decided_by=$3,version=version+1 WHERE approval_id=$4 AND account_id=$5 AND workspace_id=$6 AND decision IS NULL AND expires_at>$2`, d, now, by, id, o.UserID, o.WorkspaceID)
	if err != nil {
		return Approval{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		if _, e := s.Get(o, id); errors.Is(e, ErrNotFound) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, ErrNotExecutable
	}
	return s.Get(o, id)
}
func (s PostgresStore) ListRun(o runstate.Owner, run string) ([]Approval, error) {
	rows, err := s.DB.Query(`SELECT approval_id FROM workbench_approvals WHERE run_id=$1 AND account_id=$2 AND workspace_id=$3 ORDER BY created_at`, run, o.UserID, o.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		a, e := s.Get(o, id)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func null(v string) any {
	if v == "" {
		return nil
	}
	return v
}
