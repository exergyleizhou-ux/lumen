package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/permission"
)

// approvalHub waits for browser approve/deny decisions (mirrors internal/server).
type approvalHub struct {
	seq      atomic.Uint64
	mu       sync.Mutex
	waiters  map[string]*approvalWaiter
	modeFunc func() permission.Mode
}

type approvalWaiter struct {
	ch chan bool
}

func newApprovalHub(modeFunc func() permission.Mode) *approvalHub {
	return &approvalHub{
		waiters:  make(map[string]*approvalWaiter),
		modeFunc: modeFunc,
	}
}

func (h *approvalHub) decide(ctx context.Context, toolName string, args json.RawMessage, emit func(kind string, payload map[string]any)) (bool, error) {
	if h.modeFunc != nil {
		switch h.modeFunc() {
		case permission.ModeBypass:
			return true, nil
		case permission.ModePlan:
			return false, nil
		}
	}

	id := fmt.Sprintf("appr-%d", h.seq.Add(1))
	wt := &approvalWaiter{ch: make(chan bool, 1)}
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
		"args":    argsStr, // full args JSON for UI inspection (gate still uses original)
	})

	// Bound wait so abandoned browser tabs cannot pin agent goroutines forever.
	timer := time.NewTimer(ApprovalTimeout)
	defer timer.Stop()
	select {
	case ok := <-wt.ch:
		return ok, nil
	case <-timer.C:
		emit("error", map[string]any{
			"text": fmt.Sprintf("approval timed out after %s", ApprovalTimeout),
		})
		return false, fmt.Errorf("approval timed out after %s", ApprovalTimeout)
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (h *approvalHub) resolve(id string, allow bool) bool {
	h.mu.Lock()
	wt, ok := h.waiters[id]
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case wt.ch <- allow:
	default:
	}
	return true
}

// handleApprove is POST /api/lab/approve {id, allow}.
func (a *API) handleApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID    string `json:"id"`
		Allow bool   `json:"allow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("id required"))
		return
	}
	if a.approvals == nil || !a.approvals.resolve(req.ID, req.Allow) {
		writeErr(w, http.StatusNotFound, fmt.Errorf("approval expired or unknown"))
		return
	}
	a.approvalsTot.Add(1)
	writeJSON(w, http.StatusOK, map[string]any{"id": req.ID, "allowed": req.Allow})
}
