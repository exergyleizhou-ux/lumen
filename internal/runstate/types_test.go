package runstate

import (
	"context"
	"errors"
	"testing"

	"lumen/internal/agent"
)

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from Status
		to   Status
		want bool
	}{
		{StatusQueued, StatusRunning, true},
		{StatusRunning, StatusWaitingApproval, true},
		{StatusWaitingApproval, StatusRunning, true},
		{StatusRunning, StatusVerifying, true},
		{StatusVerifying, StatusRunning, true},
		{StatusRunning, StatusSucceeded, true},
		{StatusRunning, StatusFailed, true},
		{StatusRunning, StatusCanceled, true},
		{StatusRunning, StatusTimedOut, true},
		{StatusRunning, StatusExhausted, true},
		{StatusSucceeded, StatusRunning, false},
		{StatusFailed, StatusSucceeded, false},
		{StatusCanceled, StatusRunning, false},
		{StatusTimedOut, StatusRunning, false},
		{StatusExhausted, StatusRunning, false},
	}
	for _, tc := range cases {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("CanTransition(%s,%s)=%v want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestClassifyTerminal(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus Status
		wantReason string
	}{
		{name: "success", wantStatus: StatusSucceeded, wantReason: "finished"},
		{name: "exhausted", err: &agent.MaxStepsError{Limit: 3}, wantStatus: StatusExhausted, wantReason: "max_steps"},
		{name: "canceled", err: context.Canceled, wantStatus: StatusCanceled, wantReason: "canceled"},
		{name: "timed out", err: context.DeadlineExceeded, wantStatus: StatusTimedOut, wantReason: "timeout"},
		{name: "failed", err: errors.New("provider unavailable"), wantStatus: StatusFailed, wantReason: "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, reason := ClassifyTerminal(tc.err)
			if status != tc.wantStatus || reason != tc.wantReason {
				t.Fatalf("ClassifyTerminal(%v)=(%s,%s), want (%s,%s)", tc.err, status, reason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

func TestTerminalStatusesRejectEveryTransition(t *testing.T) {
	terminals := []Status{StatusSucceeded, StatusFailed, StatusCanceled, StatusTimedOut, StatusExhausted}
	all := []Status{StatusQueued, StatusRunning, StatusWaitingApproval, StatusVerifying, StatusSucceeded, StatusFailed, StatusCanceled, StatusTimedOut, StatusExhausted}
	for _, from := range terminals {
		if !from.Terminal() {
			t.Fatalf("%s must be terminal", from)
		}
		for _, to := range all {
			if CanTransition(from, to) {
				t.Errorf("terminal status %s transitioned to %s", from, to)
			}
		}
	}
}

func TestVerificationTerminalClassification(t *testing.T) {
	cases := []struct {
		err    error
		reason string
	}{
		{&agent.VerificationIncompleteError{Reason: "no tests"}, "verification_incomplete"},
		{&agent.VerificationFailedError{Step: "test"}, "verification_failed"},
	}
	for _, tc := range cases {
		status, reason := ClassifyTerminal(tc.err)
		if status != StatusFailed || reason != tc.reason {
			t.Fatalf("ClassifyTerminal(%v)=(%s,%s)", tc.err, status, reason)
		}
	}
}
