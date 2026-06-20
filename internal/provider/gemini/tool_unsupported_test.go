package gemini

import (
	"context"
	"strings"
	"testing"

	"lumen/internal/provider"
)

// The Gemini backend has no typed functionCall part (geminiPart is Text-only) and
// serializes tool calls as plain text, so it cannot drive Lumen's agent loop. A
// tool-bearing request must fail LOUDLY rather than silently degrade.
func TestStream_RejectsToolBearingRequestLoudly(t *testing.T) {
	prov, err := New(provider.Config{Name: "gem", BaseURL: "http://127.0.0.1:1", Model: "gemini-x"})
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

// A tool-free request (plain chat) must NOT be rejected synchronously.
func TestStream_AllowsToolFreeRequest(t *testing.T) {
	prov, _ := New(provider.Config{Name: "gem", BaseURL: "http://127.0.0.1:1", Model: "gemini-x"})
	ch, serr := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if serr != nil {
		t.Fatalf("tool-free request must not be rejected synchronously: %v", serr)
	}
	for range ch {
	}
}
