package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"lumen/internal/event"
	"lumen/internal/tool"
)

func TestAskToolHeadlessWhenNoAsker(t *testing.T) {
	out, err := (&AskTool{}).Execute(context.Background(), json.RawMessage(`{"questions":[]}`))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "headless") {
		t.Fatalf("expected headless fallback, got %q", out)
	}
}

func TestAskToolCallsAskerAndFormats(t *testing.T) {
	var gotQ []event.AskQuestion
	ctx := tool.WithAsker(context.Background(), func(_ context.Context, qs []event.AskQuestion) ([]event.AskAnswer, error) {
		gotQ = qs
		return []event.AskAnswer{{Header: "topic", Answers: []string{"文化遗产保护"}}}, nil
	})
	args := json.RawMessage(`{"questions":[{"header":"topic","question":"哪个主题?","options":[{"label":"新质生产力"},{"label":"文化遗产保护"}],"multiSelect":false}]}`)
	out, err := (&AskTool{}).Execute(ctx, args)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// The asker received the parsed question (incl. options).
	if len(gotQ) != 1 || gotQ[0].Header != "topic" || len(gotQ[0].Options) != 2 {
		t.Fatalf("asker got wrong questions: %+v", gotQ)
	}
	// The model sees the chosen label.
	if !strings.Contains(out, "文化遗产保护") || !strings.Contains(out, "topic") {
		t.Fatalf("answer not formatted for the model: %q", out)
	}
}
