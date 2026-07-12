package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"lumen/internal/approvalstate"
	"lumen/internal/permission"
	"lumen/internal/runstate"
)

type approvalDecision struct {
	Allow bool
	Args  json.RawMessage
}

type approvalWaiter struct {
	ch    chan approvalDecision
	owner runstate.Owner
	runID string
	args  json.RawMessage
}

func (s *Server) routesApproval() {
	s.handleBusiness("/v1/approve", s.handleApprove)
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID    string          `json:"id"`
		Allow bool            `json:"allow"`
		Args  json.RawMessage `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		jsonErr(w, "id required", http.StatusBadRequest)
		return
	}
	raw, ok := s.approvals.Load(req.ID)
	if !ok {
		jsonErr(w, "approval expired or unknown", http.StatusNotFound)
		return
	}
	wt := raw.(*approvalWaiter)
	if wt.owner != ownerFromRequest(r) {
		jsonErr(w, "approval expired or unknown", http.StatusNotFound)
		return
	}
	decision := approvalstate.DecisionRejected
	if req.Allow {
		decision = approvalstate.DecisionApproved
	}
	if _, getErr := s.approvalStore.Get(wt.owner, req.ID); getErr == nil {
		if _, err := s.approvalStore.Decide(wt.owner, req.ID, decision, wt.owner.UserID, time.Now().UTC()); err != nil {
			jsonErr(w, "approval expired or unknown", http.StatusNotFound)
			return
		}
	}
	select {
	case wt.ch <- approvalDecision{Allow: req.Allow, Args: req.Args}:
	default:
	}
	jsonOK(w, map[string]any{"id": req.ID, "allowed": req.Allow, "args_edited": len(req.Args) > 0})
}

func (s *Server) webApprover(owner runstate.Owner, runID string, emit func(kind string, payload map[string]any)) permission.Asker {
	return func(ctx context.Context, toolName string, args json.RawMessage) (bool, json.RawMessage, error) {
		mode := s.cfg.Ctrl.PermissionMode()
		if mode == permission.ModeBypass {
			return true, nil, nil
		}
		if mode == permission.ModePlan {
			return false, nil, nil
		}

		id := fmt.Sprintf("appr-%d-%d", time.Now().UnixNano(), s.approvalSeq.Add(1))
		hash, err := approvalstate.HashArgs(args)
		if err != nil {
			return false, nil, err
		}
		now := time.Now().UTC()
		if err = s.approvalStore.Create(approvalstate.Approval{ID: id, RunID: runID, ToolCallID: id, Owner: owner, RiskLevel: "high", Reason: permission.SummarizeArgs(toolName, args), ArgsHash: hash, EditableArgs: args, CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), Version: 1}); err != nil {
			return false, nil, err
		}
		wt := &approvalWaiter{ch: make(chan approvalDecision, 1), owner: owner, runID: runID, args: args}
		s.approvals.Store(id, wt)
		defer s.approvals.Delete(id)

		argsStr := string(args)
		if len(argsStr) > 8000 {
			argsStr = argsStr[:8000] + "…[truncated]"
		}
		emit("approval_request", map[string]any{
			"id":      id,
			"tool":    toolName,
			"summary": permission.SummarizeArgs(toolName, args),
			"args":    argsStr,
		})

		select {
		case dec := <-wt.ch:
			if !dec.Allow {
				return false, nil, nil
			}
			actual := args
			if len(dec.Args) > 0 {
				actual = dec.Args
			}
			if err := approvalstate.ValidateExecution(s.approvalStore, owner, id, actual, time.Now().UTC()); err != nil {
				return false, nil, fmt.Errorf("approval parameters changed or expired: %w", err)
			}
			return true, dec.Args, nil
		case <-ctx.Done():
			return false, nil, ctx.Err()
		}
	}
}
