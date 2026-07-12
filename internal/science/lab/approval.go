package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/approvalstate"
	"lumen/internal/permission"
	"lumen/internal/runstate"
)

// approvalDecision is the browser's answer (allow + optional edited args).
type approvalDecision struct {
	Allow bool
	Args  json.RawMessage
}

// approvalHub waits for browser approve/deny decisions (mirrors internal/server).
type approvalHub struct {
	seq           atomic.Uint64
	mu            sync.Mutex
	waiters       map[string]*approvalWaiter
	modeFunc      func() permission.Mode
	ownerModeFunc func(runstate.Owner) permission.Mode
	store         approvalstate.Store
}

type approvalWaiter struct {
	ch    chan approvalDecision
	owner runstate.Owner
}

func newApprovalHub(modeFunc func() permission.Mode) *approvalHub {
	return &approvalHub{
		waiters:  make(map[string]*approvalWaiter),
		modeFunc: modeFunc,
	}
}

func (h *approvalHub) decide(ctx context.Context, toolName string, args json.RawMessage, emit func(kind string, payload map[string]any)) (bool, json.RawMessage, error) {
	return h.decideOwned(ctx, runstate.LocalOwner, toolName, args, emit)
}

func (h *approvalHub) decideOwned(ctx context.Context, owner runstate.Owner, toolName string, args json.RawMessage, emit func(kind string, payload map[string]any)) (bool, json.RawMessage, error) {
	mode := permission.ModeDefault
	if h.ownerModeFunc != nil {
		mode = h.ownerModeFunc(owner)
	} else if h.modeFunc != nil {
		mode = h.modeFunc()
	}
	switch mode {
	case permission.ModeBypass:
		return true, nil, nil
	case permission.ModePlan:
		return false, nil, nil
	}

	id := fmt.Sprintf("appr-%d-%d", time.Now().UnixNano(), h.seq.Add(1))
	hash, err := approvalstate.HashArgs(args)
	if err != nil {
		return false, nil, err
	}
	now := time.Now().UTC()
	runID := runstate.RunIDFromContext(ctx)
	if h.store != nil && runID != "" {
		if err = h.store.Create(approvalstate.Approval{ID: id, RunID: runID, ToolCallID: id, Owner: owner, RiskLevel: "high", Reason: permission.SummarizeArgs(toolName, args), ArgsHash: hash, EditableArgs: args, CreatedAt: now, ExpiresAt: now.Add(ApprovalTimeout), Version: 1}); err != nil {
			return false, nil, err
		}
	}
	wt := &approvalWaiter{ch: make(chan approvalDecision, 1), owner: owner}
	h.mu.Lock()
	h.waiters[id] = wt
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.waiters, id)
		h.mu.Unlock()
	}()

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

	// Bound wait so abandoned browser tabs cannot pin agent goroutines forever.
	timer := time.NewTimer(ApprovalTimeout)
	defer timer.Stop()
	select {
	case dec := <-wt.ch:
		if !dec.Allow {
			return false, nil, nil
		}
		actual := args
		if len(dec.Args) > 0 {
			actual = dec.Args
		}
		if h.store != nil && runID != "" {
			if err := approvalstate.ValidateExecution(h.store, owner, id, actual, time.Now().UTC()); err != nil {
				return false, nil, fmt.Errorf("approval parameters changed or expired: %w", err)
			}
		}
		// Optional user-edited args (must be valid JSON object/array)
		if len(dec.Args) > 0 {
			if !json.Valid(dec.Args) {
				return false, nil, fmt.Errorf("edited args are not valid JSON")
			}
			return true, dec.Args, nil
		}
		return true, nil, nil
	case <-timer.C:
		emit("error", map[string]any{
			"text": fmt.Sprintf("approval timed out after %s", ApprovalTimeout),
		})
		return false, nil, fmt.Errorf("approval timed out after %s", ApprovalTimeout)
	case <-ctx.Done():
		return false, nil, ctx.Err()
	}
}

func (h *approvalHub) resolve(id string, allow bool, args json.RawMessage) bool {
	return h.resolveOwned(runstate.LocalOwner, id, allow, args)
}

func (h *approvalHub) resolveOwned(owner runstate.Owner, id string, allow bool, args json.RawMessage) bool {
	h.mu.Lock()
	wt, ok := h.waiters[id]
	h.mu.Unlock()
	if !ok || wt.owner != owner {
		return false
	}
	select {
	case wt.ch <- approvalDecision{Allow: allow, Args: args}:
	default:
	}
	return true
}

// handleApprove is POST /api/lab/approve {id, allow, args?}.
func (a *API) handleApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID    string          `json:"id"`
		Allow bool            `json:"allow"`
		Args  json.RawMessage `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("id required"))
		return
	}
	owner := labOwner(r)
	if a.approvals == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("approval expired or unknown"))
		return
	}
	if _, err := a.approvalStore.Get(owner, req.ID); err == nil {
		d := approvalstate.DecisionRejected
		if req.Allow {
			d = approvalstate.DecisionApproved
		}
		if _, err = a.approvalStore.Decide(owner, req.ID, d, owner.UserID, time.Now().UTC()); err != nil {
			writeErr(w, http.StatusNotFound, fmt.Errorf("approval expired or unknown"))
			return
		}
	}
	if !a.approvals.resolveOwned(owner, req.ID, req.Allow, req.Args) {
		writeErr(w, http.StatusNotFound, fmt.Errorf("approval expired or unknown"))
		return
	}
	a.approvalsTot.Add(1)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": req.ID, "allowed": req.Allow, "args_edited": len(req.Args) > 0,
	})
}
