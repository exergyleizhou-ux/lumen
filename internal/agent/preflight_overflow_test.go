package agent

import (
	"strings"
	"testing"

	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

func hasOverflowWarn(events []event.Event) bool {
	for _, e := range events {
		if e.Kind == event.Notice && e.Level == event.LevelWarn && strings.Contains(e.Text, "context window") {
			return true
		}
	}
	return false
}

// When the un-compactable stable prefix (system prompt + tool schemas) plus the
// first input already crowds the context window, the agent must WARN before the
// first turn streams — auto-compaction can't shrink the prefix, so a silent
// window slide (the gemma-4-12b "greeting instead of editing" failure) is the
// alternative.
func TestPreflightOverflowWarnsWhenPrefixCrowdsWindow(t *testing.T) {
	var got []event.Event
	a := New(ttBlockingProvider{}, tool.NewRegistry(), NewSession(""),
		Options{ContextWindow: 50, Sink: event.FuncSink(func(e event.Event) { got = append(got, e) })})
	// A stable prefix far larger than the 50-token window * 0.8 compactRatio.
	a.session.Add(provider.Message{Role: provider.RoleSystem, Content: strings.Repeat("token ", 200)})
	a.cachedSchemas = a.tools.Schemas()

	a.preflightOverflowCheck("hello")

	if !hasOverflowWarn(got) {
		t.Fatalf("expected an overflow WARN notice, got %d events: %+v", len(got), got)
	}
}

func TestPreflightNoWarnWhenWithinWindow(t *testing.T) {
	var got []event.Event
	a := New(ttBlockingProvider{}, tool.NewRegistry(), NewSession(""),
		Options{ContextWindow: 128000, Sink: event.FuncSink(func(e event.Event) { got = append(got, e) })})
	a.session.Add(provider.Message{Role: provider.RoleSystem, Content: "short prompt"})
	a.cachedSchemas = a.tools.Schemas()

	a.preflightOverflowCheck("hi")

	if hasOverflowWarn(got) {
		t.Fatal("did not expect an overflow warning when well within the window")
	}
}
