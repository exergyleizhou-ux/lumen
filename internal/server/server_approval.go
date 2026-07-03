package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"lumen/internal/permission"
)

type approvalWaiter struct {
	ch chan bool
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
		ID    string `json:"id"`
		Allow bool   `json:"allow"`
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
	case wt.ch <- req.Allow:
	default:
	}
	jsonOK(w, map[string]any{"id": req.ID, "allowed": req.Allow})
}

func (s *Server) webApprover(emit func(kind string, payload map[string]any)) func(context.Context, string, json.RawMessage) (bool, error) {
	return func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		mode := s.cfg.Ctrl.PermissionMode()
		if mode == permission.ModeBypass {
			return true, nil
		}
		if mode == permission.ModePlan {
			return false, nil
		}

		id := fmt.Sprintf("appr-%d", s.approvalSeq.Add(1))
		wt := &approvalWaiter{ch: make(chan bool, 1)}
		s.approvals.Store(id, wt)
		defer s.approvals.Delete(id)

		emit("approval_request", map[string]any{
			"id":      id,
			"tool":    toolName,
			"summary": permission.SummarizeArgs(toolName, args),
		})

		select {
		case ok := <-wt.ch:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}