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
	id    string
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
	if req.Allow && len(req.Args) > 0 {
		oldHash, _ := approvalstate.HashArgs(wt.args)
		newHash, hashErr := approvalstate.HashArgs(req.Args)
		if hashErr != nil {
			jsonErr(w, "invalid args", 400)
			return
		}
		if newHash != oldHash {
			old, err := s.approvalStore.Get(wt.owner, req.ID)
			if err != nil {
				jsonErr(w, "approval expired or unknown", 404)
				return
			}
			if _, err = s.approvalStore.Decide(wt.owner, req.ID, approvalstate.DecisionInvalidated, wt.owner.UserID, time.Now().UTC()); err != nil {
				jsonErr(w, "approval expired or unknown", 404)
				return
			}
			newID := fmt.Sprintf("appr-%d-%d", time.Now().UnixNano(), s.approvalSeq.Add(1))
			old.ID = newID
			old.ArgsHash = newHash
			old.EditableArgs = req.Args
			old.Decision = nil
			old.DecidedAt = nil
			old.DecidedBy = ""
			old.CreatedAt = time.Now().UTC()
			old.ExpiresAt = old.CreatedAt.Add(5 * time.Minute)
			old.Version = 1
			if err = s.approvalStore.Create(old); err != nil {
				jsonErr(w, "create replacement approval failed", 500)
				return
			}
			wt.args = req.Args
			wt.id = newID
			s.approvals.Store(newID, wt)
			s.approvals.Delete(req.ID)
			jsonOK(w, map[string]any{"id": newID, "allowed": false, "reapproval_required": true})
			return
		}
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
	case wt.ch <- approvalDecision{Allow: req.Allow, Args: func() json.RawMessage {
		if len(req.Args) > 0 {
			return req.Args
		}
		return wt.args
	}()}:
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
		review, _ := permission.ReviewFrom(ctx)
		effects := approvalstate.Effects{Reads: review.Effects.ReadsFiles, Writes: review.Effects.WritesFiles, Commands: review.Effects.RunsCommands, Network: review.Effects.UsesNetwork, Remote: review.Effects.SendsRemoteData, Compute: review.Effects.StartsCompute, Publish: review.Effects.Publishes, Charge: review.Effects.MayCharge}
		command, scope, remote, networks, outputs := approvalDetails(args)
		if err = s.approvalStore.Create(approvalstate.Approval{ID: id, RunID: runID, StepID: review.StepID, ToolCallID: review.ToolCallID, Owner: owner, RiskLevel: "high", Reason: permission.SummarizeArgs(toolName, args), Effects: effects, Command: command, FileScope: scope, RemoteTarget: remote, NetworkTargets: networks, ExpectedOutputs: outputs, ArgsHash: hash, EditableArgs: args, CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), Version: 1}); err != nil {
			return false, nil, err
		}
		wt := &approvalWaiter{ch: make(chan approvalDecision, 1), owner: owner, runID: runID, args: args, id: id}
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
			approvedID := wt.id
			if err := approvalstate.ValidateExecution(s.approvalStore, owner, approvedID, actual, time.Now().UTC()); err != nil {
				return false, nil, fmt.Errorf("approval parameters changed or expired: %w", err)
			}
			executionID := runID + ":" + review.ToolCallID + ":" + hash
			if _, err := s.approvalStore.Consume(owner, approvedID, executionID, time.Now().UTC()); err != nil {
				return false, nil, fmt.Errorf("approval already consumed: %w", err)
			}
			if review.Execution != nil {
				review.Execution.Complete = func(success bool) error {
					return s.approvalStore.Complete(owner, approvedID, executionID, success, time.Now().UTC())
				}
			}
			return true, dec.Args, nil
		case <-ctx.Done():
			return false, nil, ctx.Err()
		}
	}
}

func approvalDetails(args json.RawMessage) (string, []string, string, []string, []string) {
	var v map[string]any
	_ = json.Unmarshal(args, &v)
	str := func(k string) string { x, _ := v[k].(string); return x }
	command := str("command")
	var scope []string
	for _, k := range []string{"path", "file", "output_path"} {
		if x := str(k); x != "" {
			scope = append(scope, x)
		}
	}
	remote := str("remote_target")
	if remote == "" {
		remote = str("host")
	}
	var networks []string
	for _, k := range []string{"url", "endpoint", "host"} {
		if x := str(k); x != "" {
			networks = append(networks, x)
		}
	}
	return command, scope, remote, networks, append([]string(nil), scope...)
}
