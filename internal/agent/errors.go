package agent

import (
	"errors"
	"fmt"
)

// ErrMaxStepsExhausted identifies a run that stopped before producing a final
// answer because it consumed its configured tool/model step budget.
var ErrMaxStepsExhausted = errors.New("agent max steps exhausted")

// MaxStepsError carries the exhausted step limit while remaining compatible
// with errors.Is(err, ErrMaxStepsExhausted).
type MaxStepsError struct {
	Limit int
}

func (e *MaxStepsError) Error() string {
	return fmt.Sprintf("%v: limit=%d", ErrMaxStepsExhausted, e.Limit)
}

func (e *MaxStepsError) Unwrap() error { return ErrMaxStepsExhausted }
