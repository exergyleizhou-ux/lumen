package usage

import (
	"lumen/internal/event"
	"lumen/internal/runstate"
	"testing"
	"time"
)

func TestCapturingSinkReplayIsIdempotent(t *testing.T) {
	store := NewMemoryStore()
	sink := CapturingSink{Store: store, Owner: runstate.Owner{UserID: "u", WorkspaceID: "w"}, Provider: "openai", Model: "gpt", Pricing: Pricing{Input: 2, Output: 10, CacheHit: .5}}
	ev := event.Event{Kind: event.UsageKind, RunID: "run", EventID: "run:2", Timestamp: time.Now(), Usage: &event.Usage{PromptTokens: 100, CompletionTokens: 20, CacheHitTokens: 40, CacheMissTokens: 60}}
	sink.Emit(ev)
	sink.Emit(ev)
	recs := store.Records()
	if len(recs) != 1 {
		t.Fatalf("records=%d", len(recs))
	}
	r := recs[0]
	if r.UserID != "u" || r.WorkspaceID != "w" || r.Provider != "openai" || r.Model != "gpt" {
		t.Fatalf("identity: %+v", r)
	}
	if r.EstimatedCostMicros != 340 {
		t.Fatalf("cost=%d", r.EstimatedCostMicros)
	}
}
