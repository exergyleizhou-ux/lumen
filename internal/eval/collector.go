package eval

import (
	"strings"

	"lumen/internal/event"
)

// SignalCollector accumulates the event-stream-derived fields of RunSignals as a
// task runs. The harness fills the rest afterward (Passed, RunErr, FilesChanged,
// TestTampered, ServerContextWindow, and the timeout StopReason override, which
// comes from Run's returned error rather than a TurnDone event).
//
// Tool calls are counted off event.ToolResult, NOT event.ToolDispatch: the
// OpenAI provider can deliver a finalized tool call with no preceding
// ChunkToolCallStart, so a Dispatch-based count undercounts and would
// false-positive the no-tool-call failure bucket.
type SignalCollector struct {
	toolResultCount   int
	firstPromptTokens int
	firstSeen         bool
	stopReason        string
	finalText         strings.Builder
}

// Observe folds one event into the running signals.
func (c *SignalCollector) Observe(e event.Event) {
	switch e.Kind {
	case event.ToolResult:
		c.toolResultCount++
		// Text streamed after the LAST tool result is the model's final answer;
		// reset so finalText holds only that (the greeting tell, for F1).
		c.finalText.Reset()
	case event.UsageKind:
		if !c.firstSeen && e.Usage != nil && e.Usage.PromptTokens > 0 {
			c.firstPromptTokens = e.Usage.PromptTokens // first turn = overflow ground truth
			c.firstSeen = true
		}
	case event.Text:
		c.finalText.WriteString(e.Text)
	case event.TurnDone:
		if e.StopReason != "" {
			c.stopReason = e.StopReason
		}
	}
}

// Partial returns the event-derived signals collected so far.
func (c *SignalCollector) Partial() RunSignals {
	return RunSignals{
		ToolResultCount:   c.toolResultCount,
		FirstPromptTokens: c.firstPromptTokens,
		StopReason:        c.stopReason,
		FinalText:         strings.TrimSpace(c.finalText.String()),
	}
}
