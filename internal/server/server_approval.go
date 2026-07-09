package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"lumen/internal/permission"
)

type approvalDecision struct {
	Allow bool
	Args  json.RawMessage
}

type approvalWaiter struct {
	ch chan approvalDecision
}

func (s *Server) routesApproval() {
	s.mux.HandleFunc("/v1/approve", s.handleApprove)
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
	select {
	case wt.ch <- approvalDecision{Allow: req.Allow, Args: req.Args}:
	default:
	}
	jsonOK(w, map[string]any{"id": req.ID, "allowed": req.Allow, "args_edited": len(req.Args) > 0})
}

func (s *Server) webApprover(emit func(kind string, payload map[string]any)) permission.Asker {
	return func(ctx context.Context, toolName string, args json.RawMessage) (bool, json.RawMessage, error) {
		mode := s.cfg.Ctrl.PermissionMode()
		if mode == permission.ModeBypass {
			return true, nil, nil
		}
		if mode == permission.ModePlan {
			return false, nil, nil
		}

		id := fmt.Sprintf("appr-%d", s.approvalSeq.Add(1))
		wt := &approvalWaiter{ch: make(chan approvalDecision, 1)}
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
			return true, dec.Args, nil
		case <-ctx.Done():
			return false, nil, ctx.Err()
		}
	}
}