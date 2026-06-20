package eval

import (
	"testing"

	"lumen/internal/event"
)

func TestSignalCollector_FinishedWithTools(t *testing.T) {
	var c SignalCollector
	for _, e := range []event.Event{
		{Kind: event.TurnStarted},
		{Kind: event.UsageKind, Usage: &event.Usage{PromptTokens: 4000}},
		{Kind: event.ToolResult, Tool: event.Tool{Name: "bash"}},
		{Kind: event.ToolResult, Tool: event.Tool{Name: "edit_file"}},
		{Kind: event.Text, Text: "Done — added the nil guard."},
		{Kind: event.TurnDone, StopReason: "finished"},
	} {
		c.Observe(e)
	}
	s := c.Partial()
	if s.ToolResultCount != 2 {
		t.Errorf("ToolResultCount = %d, want 2", s.ToolResultCount)
	}
	if s.FirstPromptTokens != 4000 {
		t.Errorf("FirstPromptTokens = %d, want 4000", s.FirstPromptTokens)
	}
	if s.StopReason != "finished" {
		t.Errorf("StopReason = %q, want finished", s.StopReason)
	}
}

// The overflow case: one UsageKind with a huge prompt, no tools, a greeting.
func TestSignalCollector_OverflowGreeting(t *testing.T) {
	var c SignalCollector
	for _, e := range []event.Event{
		{Kind: event.UsageKind, Usage: &event.Usage{PromptTokens: 12000}},
		{Kind: event.Text, Text: "您好！请问您需要什么帮助？"},
		{Kind: event.TurnDone, StopReason: "finished"},
	} {
		c.Observe(e)
	}
	s := c.Partial()
	if s.ToolResultCount != 0 {
		t.Errorf("ToolResultCount = %d, want 0", s.ToolResultCount)
	}
	if s.FirstPromptTokens != 12000 {
		t.Errorf("FirstPromptTokens = %d, want 12000", s.FirstPromptTokens)
	}
	if s.FinalText == "" {
		t.Error("FinalText should capture the greeting")
	}
}

// FirstPromptTokens must capture the FIRST turn's prompt, not the last/sum —
// later turns re-send a grown context and would mask the overflow.
func TestSignalCollector_FirstPromptTokensIsFirst(t *testing.T) {
	var c SignalCollector
	c.Observe(event.Event{Kind: event.UsageKind, Usage: &event.Usage{PromptTokens: 9000}})
	c.Observe(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "read_file"}})
	c.Observe(event.Event{Kind: event.UsageKind, Usage: &event.Usage{PromptTokens: 15000}})
	if got := c.Partial().FirstPromptTokens; got != 9000 {
		t.Errorf("FirstPromptTokens = %d, want 9000 (the first turn)", got)
	}
}
