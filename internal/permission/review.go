package permission

import (
	"context"
	"lumen/internal/tool"
)

type Execution struct{ Complete func(bool) error }
type Review struct {
	StepID, ToolCallID string
	Effects            tool.Effects
	Execution          *Execution
}
type reviewKey struct{}

func WithReview(ctx context.Context, r Review) context.Context {
	return context.WithValue(ctx, reviewKey{}, r)
}
func ReviewFrom(ctx context.Context) (Review, bool) {
	r, ok := ctx.Value(reviewKey{}).(Review)
	return r, ok
}
