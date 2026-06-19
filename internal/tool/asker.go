package tool

import (
	"context"

	"lumen/internal/event"
)

// AskFunc puts structured multiple-choice questions to the user and returns
// their answers. It lives in the tool package (not agent) so built-in tools can
// reach the asker via context without importing the agent package — keeping the
// dependency graph acyclic.
type AskFunc func(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error)

type askerKey struct{}

// WithAsker stamps an asker onto ctx. The agent does this per tool call so the
// `ask` tool can prompt the real user when one is attached (interactive chat),
// and fall back to headless behavior when none is (one-shot / piped runs).
func WithAsker(ctx context.Context, fn AskFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, askerKey{}, fn)
}

// AskerFrom returns the asker attached to ctx, if any.
func AskerFrom(ctx context.Context) (AskFunc, bool) {
	fn, ok := ctx.Value(askerKey{}).(AskFunc)
	return fn, ok && fn != nil
}
