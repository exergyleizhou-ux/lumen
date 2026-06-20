package anthro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lumen/internal/provider"
)

// The request must carry tool schemas in Anthropic's native shape
// (name/description/input_schema) so the model knows the tools exist.
func TestBuildRequestSendsToolSchemas(t *testing.T) {
	p := &Provider{name: "c", model: "claude-x"}
	r := p.buildRequest(provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Tools: []provider.ToolSchema{{
			Name:        "edit_file",
			Description: "edit a file",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
	})
	if len(r.Tools) != 1 {
		t.Fatalf("expected 1 tool in request, got %d", len(r.Tools))
	}
	if r.Tools[0].Name != "edit_file" || r.Tools[0].Description != "edit a file" {
		t.Errorf("tool mismatch: %+v", r.Tools[0])
	}
	if string(r.Tools[0].InputSchema) != `{"type":"object"}` {
		t.Errorf("input_schema = %s, want {\"type\":\"object\"}", r.Tools[0].InputSchema)
	}
}

// A streamed tool_use block (content_block_start → input_json_delta(s) →
// content_block_stop) must be parsed and emitted as one ChunkToolCall with the
// accumulated arguments — not dropped.
func TestParseStreamedToolUse(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"edit_file"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"a.go\"}"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(stream))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "c", BaseURL: srv.URL, Model: "claude-x"})
	ch, err := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "fix it"}},
		Tools:    []provider.ToolSchema{{Name: "edit_file"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got *provider.ToolCall
	for c := range ch {
		if c.Type == provider.ChunkToolCall && c.ToolCall != nil {
			got = c.ToolCall
		}
	}
	if got == nil {
		t.Fatal("expected a ChunkToolCall from the streamed tool_use, got none")
	}
	if got.ID != "toolu_1" || got.Name != "edit_file" {
		t.Errorf("tool call id/name = %q/%q, want toolu_1/edit_file", got.ID, got.Name)
	}
	if got.Arguments != `{"path":"a.go"}` {
		t.Errorf("accumulated arguments = %q, want {\"path\":\"a.go\"}", got.Arguments)
	}
}

// A response that mixes a text block (index 0) and a tool_use block (index 1) —
// the common "I'll edit X" → edit_file pattern — must yield BOTH the text and the
// tool call, with the text block's content_block_stop not corrupting the tool.
func TestParseTextThenToolUse(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"I'll fix it."}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_9","name":"edit_file"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(stream))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "c", BaseURL: srv.URL, Model: "claude-x"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "fix it"}},
		Tools:    []provider.ToolSchema{{Name: "edit_file"}},
	})
	var text strings.Builder
	var tool *provider.ToolCall
	for c := range ch {
		switch c.Type {
		case provider.ChunkText:
			text.WriteString(c.Text)
		case provider.ChunkToolCall:
			tool = c.ToolCall
		}
	}
	if text.String() != "I'll fix it." {
		t.Errorf("text = %q, want \"I'll fix it.\"", text.String())
	}
	if tool == nil || tool.Name != "edit_file" || tool.Arguments != "{}" {
		t.Errorf("tool call = %+v, want edit_file with {}", tool)
	}
}
