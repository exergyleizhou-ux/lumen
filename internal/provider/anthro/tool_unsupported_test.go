package anthro

import (
	"context"
	"strings"
	"testing"

	"lumen/internal/provider"
)

// The native Anthropic backend sends no tool schemas and never parses tool_use
// (see anthroRequest — no Tools field; parseSSE — no tool_use branch). A coding
// agent always carries tools, so such a request must fail LOUDLY here rather than
// silently degrade to a plain-chat reply that never edits a file.
func TestStream_RejectsToolBearingRequestLoudly(t *testing.T) {
	prov, err := New(provider.Config{Name: "claude", BaseURL: "http://127.0.0.1:1", Model: "claude-x"})
	if err != nil {
		t.Fatal(err)
	}
	_, serr := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "fix the bug"}},
		Tools:    []provider.ToolSchema{{Name: "edit_file", Description: "edit a file"}},
	})
	if serr == nil {
		t.Fatal("expected Stream to reject a tool-bearing request, got nil error")
	}
	msg := serr.Error()
	for _, want := range []string{"tool", "OpenAI-compatible"} {
		if !strings.Contains(msg, want) {
			t.Errorf("rejection message should mention %q, got: %s", want, msg)
		}
	}
}

// A tool-free request (plain chat) must NOT be rejected synchronously — the guard
// only fires when tools are present.
func TestStream_AllowsToolFreeRequest(t *testing.T) {
	prov, _ := New(provider.Config{Name: "claude", BaseURL: "http://127.0.0.1:1", Model: "claude-x"})
	ch, serr := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if serr != nil {
		t.Fatalf("tool-free request must not be rejected synchronously: %v", serr)
	}
	for range ch { // drain (a failed dial surfaces as a ChunkError, which is fine)
	}
}
