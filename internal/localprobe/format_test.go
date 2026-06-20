package localprobe

import (
	"strings"
	"testing"
)

func TestFormatMarkdownRendersMatrix(t *testing.T) {
	results := []Result{
		{Name: "lmstudio", Model: "qwen3.6-27b", CanToolCall: true, TokensPerSec: 18.4, ElapsedMs: 4200, CompletionTokens: 20},
		{Name: "lmstudio", Model: "gemma-4-coder", CanToolCall: false, TokensPerSec: 31.2, ElapsedMs: 1500, TextReply: "I would edit the file."},
		{Name: "ollama", Model: "", Err: errString("connection refused")},
	}
	md := FormatMarkdown(results)

	// A model that emits tool_calls drives the agent.
	if !strings.Contains(md, "qwen3.6-27b") {
		t.Error("missing qwen row")
	}
	if !strings.Contains(md, "✅") {
		t.Error("a tool-call-capable model should be marked ✅")
	}
	if !strings.Contains(md, "❌") {
		t.Error("a prose-only model should be marked ❌")
	}
	// The unreachable endpoint should be reported, not dropped.
	if !strings.Contains(md, "connection refused") {
		t.Errorf("unreachable endpoint error not surfaced:\n%s", md)
	}
	// Markdown table header present.
	if !strings.Contains(md, "| Endpoint |") {
		t.Errorf("not a markdown table:\n%s", md)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
