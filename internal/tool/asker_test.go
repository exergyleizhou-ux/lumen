package tool

import (
	"context"
	"errors"
	"testing"

	"lumen/internal/event"
)

func TestAskerRoundTrip(t *testing.T) {
	called := false
	fn := func(ctx context.Context, qs []event.AskQuestion) ([]event.AskAnswer, error) {
		called = true
		return []event.AskAnswer{{Header: qs[0].Header, Answers: []string{"ok"}}}, nil
	}
	ctx := WithAsker(context.Background(), fn)
	got, ok := AskerFrom(ctx)
	if !ok {
		t.Fatal("AskerFrom should find the stamped asker")
	}
	ans, err := got(ctx, []event.AskQuestion{{Header: "h"}})
	if err != nil || !called || len(ans) != 1 || ans[0].Answers[0] != "ok" {
		t.Fatalf("asker not invoked correctly: called=%v ans=%v err=%v", called, ans, err)
	}
}

func TestAskerAbsentAndNil(t *testing.T) {
	if _, ok := AskerFrom(context.Background()); ok {
		t.Fatal("no asker on a bare context")
	}
	// WithAsker(nil) must not register a non-nil-but-useless asker.
	if _, ok := AskerFrom(WithAsker(context.Background(), nil)); ok {
		t.Fatal("WithAsker(nil) should be a no-op")
	}
}

func TestAskerErrorPropagates(t *testing.T) {
	want := errors.New("boom")
	ctx := WithAsker(context.Background(), func(context.Context, []event.AskQuestion) ([]event.AskAnswer, error) {
		return nil, want
	})
	fn, _ := AskerFrom(ctx)
	if _, err := fn(ctx, nil); !errors.Is(err, want) {
		t.Fatalf("error should propagate, got %v", err)
	}
}
