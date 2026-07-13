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
	hosted        bool
}

type approvalWaiter struct {
	ch    chan approvalDecision
	owner runstate.Owner
	args  json.RawMessage
	id    string
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
	if h.hosted && (h.store == nil || runstate.RunIDFromContext(ctx) == "") {
		return false, nil, fmt.Errorf("hosted approval persistence unavailable")
	}
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
	reviewCtx, _ := permission.ReviewFrom(ctx)
	if h.store != nil && runID != "" {
		review, _ := permission.ReviewFrom(ctx)
		effects := approvalstate.Effects{Reads: review.Effects.ReadsFiles, Writes: review.Effects.WritesFiles, Commands: review.Effects.RunsCommands, Network: review.Effects.UsesNetwork, Remote: review.Effects.SendsRemoteData, Compute: review.Effects.StartsCompute, Publish: review.Effects.Publishes, Charge: review.Effects.MayCharge}
		command, scope, remote, networks, outputs := labApprovalDetails(args)
		if err = h.store.Create(approvalstate.Approval{ID: id, RunID: runID, StepID: review.StepID, ToolCallID: review.ToolCallID, Owner: owner, RiskLevel: "high", Reason: permission.SummarizeArgs(toolName, args), Effects: effects, Command: command, FileScope: scope, RemoteTarget: remote, NetworkTargets: networks, ExpectedOutputs: outputs, ArgsHash: hash, EditableArgs: args, CreatedAt: now, ExpiresAt: now.Add(ApprovalTimeout), Version: 1}); err != nil {
			return false, nil, err
		}
	}
	wt := &approvalWaiter{ch: make(chan approvalDecision, 1), owner: owner, args: args, id: id}
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
			approvedID := wt.id
			if err := approvalstate.ValidateExecution(h.store, owner, approvedID, actual, time.Now().UTC()); err != nil {
				return false, nil, fmt.Errorf("approval parameters changed or expired: %w", err)
			}
			executionID := runID + ":" + reviewCtx.ToolCallID + ":" + hash
			if _, err := h.store.Consume(owner, approvedID, executionID, time.Now().UTC()); err != nil {
				return false, nil, fmt.Errorf("approval already consumed: %w", err)
			}
			if reviewCtx.Execution != nil {
				reviewCtx.Execution.Complete = func(success bool) error {
					return h.store.Complete(owner, approvedID, executionID, success, time.Now().UTC())
				}
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

func labApprovalDetails(args json.RawMessage) (string, []string, string, []string, []string) {
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
	case wt.ch <- approvalDecision{Allow: allow, Args: func() json.RawMessage {
		if len(args) > 0 {
			return args
		}
		return wt.args
	}()}:
		h.mu.Lock()
		delete(h.waiters, id)
		h.mu.Unlock()
	default:
	}
	return true
}
func (h *approvalHub) replaceEdited(owner runstate.Owner, id, newID string, args json.RawMessage) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	wt, ok := h.waiters[id]
	if !ok || wt.owner != owner {
		return false
	}
	delete(h.waiters, id)
	wt.args = args
	wt.id = newID
	h.waiters[newID] = wt
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
	if req.Allow && len(req.Args) > 0 {
		a.approvals.mu.Lock()
		wt, ok := a.approvals.waiters[req.ID]
		a.approvals.mu.Unlock()
		if !ok || wt.owner != owner {
			writeErr(w, 404, fmt.Errorf("approval expired or unknown"))
			return
		}
		oldHash, _ := approvalstate.HashArgs(wt.args)
		newHash, e := approvalstate.HashArgs(req.Args)
		if e != nil {
			writeErr(w, 400, e)
			return
		}
		if newHash != oldHash {
			old, e := a.approvalStore.Get(owner, req.ID)
			if e != nil {
				writeErr(w, 404, fmt.Errorf("approval expired or unknown"))
				return
			}
			if _, e = a.approvalStore.Decide(owner, req.ID, approvalstate.DecisionInvalidated, owner.UserID, time.Now().UTC()); e != nil {
				writeErr(w, 404, fmt.Errorf("approval expired or unknown"))
				return
			}
			newID := fmt.Sprintf("appr-%d-%d", time.Now().UnixNano(), a.approvals.seq.Add(1))
			old.ID = newID
			old.ArgsHash = newHash
			old.EditableArgs = req.Args
			old.Decision = nil
			old.DecidedAt = nil
			old.DecidedBy = ""
			old.CreatedAt = time.Now().UTC()
			old.ExpiresAt = old.CreatedAt.Add(ApprovalTimeout)
			old.Version = 1
			if e = a.approvalStore.Create(old); e != nil {
				writeErr(w, 500, e)
				return
			}
			if !a.approvals.replaceEdited(owner, req.ID, newID, req.Args) {
				writeErr(w, 404, fmt.Errorf("approval expired or unknown"))
				return
			}
			writeJSON(w, 200, map[string]any{"id": newID, "allowed": false, "reapproval_required": true})
			return
		}
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
