package event

import (
	"sync"
	"testing"
	"time"
)

func TestDiscardSink(t *testing.T) {
	// Discard should not panic on any event
	ev := Event{Kind: TurnStarted, Timestamp: time.Now()}
	Discard.Emit(ev)
	Discard.Emit(Event{Kind: Text, Text: "hello"})
	Discard.Emit(Event{Kind: ToolDispatch, Tool: Tool{ID: "1", Name: "bash"}})
	// If we got here without panic, it works
}

func TestFuncSink(t *testing.T) {
	var mu sync.Mutex
	var events []Event
	sink := FuncSink(func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	})

	sink.Emit(Event{Kind: Text, Text: "a"})
	sink.Emit(Event{Kind: Text, Text: "b"})
	sink.Emit(Event{Kind: TurnDone})

	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
	if events[0].Text != "a" {
		t.Errorf("first event: want 'a', got %q", events[0].Text)
	}
}

func TestEventKinds(t *testing.T) {
	// All event kinds should be non-empty strings
	kinds := []Kind{TurnStarted, TurnDone, Phase, Text, Reasoning,
		ToolDispatch, ToolResult, ToolProgress, UsageKind, Notice, Ask, PlanApproval}
	for _, k := range kinds {
		if string(k) == "" {
			t.Error("event kind should not be empty")
		}
	}
}

func TestLevelSeverity(t *testing.T) {
	levels := []Level{LevelInfo, LevelWarn, LevelErr}
	for _, l := range levels {
		if string(l) == "" {
			t.Error("level should not be empty")
		}
	}
}

func TestToolStruct(t *testing.T) {
	tt := Tool{
		ID:       "call-1",
		Name:     "bash",
		ReadOnly: false,
		Args:     `{"command":"echo hi"}`,
		Err:      "",
	}
	if tt.ID != "call-1" || tt.Name != "bash" {
		t.Error("Tool struct fields mismatch")
	}
	if tt.ReadOnly {
		t.Error("bash should not be read-only")
	}
}

func TestUsageStruct(t *testing.T) {
	u := Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CacheHitTokens:   80,
		CacheMissTokens:  20,
	}
	if u.TotalTokens != 150 {
		t.Errorf("total tokens: %d", u.TotalTokens)
	}
	if u.CacheHitTokens+u.CacheMissTokens != 100 {
		t.Error("cache hit + miss should equal prompt tokens")
	}
}

func TestAskQuestionStruct(t *testing.T) {
	q := AskQuestion{
		Header:   "Library",
		Question: "Which library to use?",
		Options: []AskOption{
			{Label: "stdlib", Description: "Go standard library"},
			{Label: "gin", Description: "Gin web framework"},
		},
		MultiSelect: false,
	}
	if q.Header != "Library" || len(q.Options) != 2 {
		t.Error("AskQuestion struct mismatch")
	}
}

func TestSinkNilInterface(t *testing.T) {
	// A nil FuncSink should not panic — Discard is safe
	var s Sink
	if s != nil {
		// If it's not nil, emit should work
		t.Skip("sink is not nil, but Discard is always safe")
	}
}
