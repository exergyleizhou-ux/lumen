package agent

import (
	"errors"
	"fmt"
)

// ErrMaxStepsExhausted identifies a run that stopped before producing a final
// answer because it consumed its configured tool/model step budget.
var ErrMaxStepsExhausted = errors.New("agent max steps exhausted")

var (
	ErrVerificationIncomplete = errors.New("engineering verification incomplete")
	ErrVerificationFailed     = errors.New("engineering verification failed")
)

// MaxStepsError carries the exhausted step limit while remaining compatible
// with errors.Is(err, ErrMaxStepsExhausted).
type MaxStepsError struct {
	Limit int
}

func (e *MaxStepsError) Error() string {
	return fmt.Sprintf("%v: limit=%d", ErrMaxStepsExhausted, e.Limit)
}

func (e *MaxStepsError) Unwrap() error { return ErrMaxStepsExhausted }

type VerificationIncompleteError struct{ Reason string }

func (e *VerificationIncompleteError) Error() string {
	if e.Reason == "" {
		return ErrVerificationIncomplete.Error()
	}
	return fmt.Sprintf("%v: %s", ErrVerificationIncomplete, e.Reason)
}

func (e *VerificationIncompleteError) Unwrap() error { return ErrVerificationIncomplete }

type VerificationFailedError struct{ Step string }

func (e *VerificationFailedError) Error() string {
	if e.Step == "" {
		return ErrVerificationFailed.Error()
	}
	return fmt.Sprintf("%v: %s", ErrVerificationFailed, e.Step)
}

func (e *VerificationFailedError) Unwrap() error { return ErrVerificationFailed }

func verificationStopReason(err error) string {
	switch {
	case errors.Is(err, ErrVerificationFailed):
		return "verification_failed"
	case errors.Is(err, ErrVerificationIncomplete):
		return "verification_incomplete"
	default:
		return "failed"
	}
}
