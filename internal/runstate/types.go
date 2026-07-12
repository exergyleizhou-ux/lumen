// Package runstate defines durable task lifecycle state shared by every Lumen
// surface. It deliberately contains no UI or transport behavior.
package runstate

import (
	"context"
	"errors"
	"time"

	"lumen/internal/agent"
)

// Status is the authoritative lifecycle state of one task run.
type Status string

const (
	StatusQueued          Status = "queued"
	StatusRunning         Status = "running"
	StatusWaitingApproval Status = "waiting_approval"
	StatusVerifying       Status = "verifying"
	StatusSucceeded       Status = "succeeded"
	StatusFailed          Status = "failed"
	StatusCanceled        Status = "canceled"
	StatusTimedOut        Status = "timed_out"
	StatusExhausted       Status = "exhausted"
)

// Run is the durable summary of one task execution.
type Run struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id,omitempty"`
	WorkspaceID string     `json:"workspace_id,omitempty"`
	SessionID   string     `json:"session_id,omitempty"`
	ParentID    string     `json:"parent_run_id,omitempty"`
	Profile     string     `json:"profile"`
	Title       string     `json:"title"`
	Status      Status     `json:"status"`
	StopReason  string     `json:"stop_reason,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Version     uint64     `json:"version"`
}

// Owner is the immutable tenant boundary for a run.
type Owner struct{ UserID, WorkspaceID string }

var LocalOwner = Owner{UserID: "local", WorkspaceID: "local"}

func (o Owner) Valid() bool { return o.UserID != "" && o.WorkspaceID != "" }

// Terminal reports whether no further state transitions are legal.
func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCanceled, StatusTimedOut, StatusExhausted:
		return true
	default:
		return false
	}
}

// CanTransition validates the run state machine. Terminal states are immutable.
func CanTransition(from, to Status) bool {
	if from.Terminal() {
		return false
	}
	switch from {
	case StatusQueued:
		return to == StatusRunning || to == StatusCanceled
	case StatusRunning:
		return to == StatusWaitingApproval || to == StatusVerifying || to.Terminal()
	case StatusWaitingApproval:
		return to == StatusRunning || to == StatusCanceled || to == StatusTimedOut || to == StatusFailed
	case StatusVerifying:
		return to == StatusRunning || to == StatusFailed || to == StatusCanceled || to == StatusTimedOut
	default:
		return false
	}
}

// ClassifyTerminal converts a returned run error into an authoritative terminal
// state and stable machine-readable stop reason.
func ClassifyTerminal(err error) (Status, string) {
	switch {
	case err == nil:
		return StatusSucceeded, "finished"
	case errors.Is(err, agent.ErrMaxStepsExhausted):
		return StatusExhausted, "max_steps"
	case errors.Is(err, agent.ErrVerificationIncomplete):
		return StatusFailed, "verification_incomplete"
	case errors.Is(err, agent.ErrVerificationFailed):
		return StatusFailed, "verification_failed"
	case errors.Is(err, context.Canceled):
		return StatusCanceled, "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return StatusTimedOut, "timeout"
	default:
		return StatusFailed, "error"
	}
}
