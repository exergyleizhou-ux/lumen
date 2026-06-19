package anthro

import (
	"encoding/json"
	"strings"
	"testing"

	"lumen/internal/provider"
)

// A tool call must serialize as a structured tool_use block (id/name/input),
// not as JSON string-formatted into a "text" field — Anthropic rejects the
// latter, so tool calls were broken for this provider.
func TestBuildRequest_ToolUseWireFormat(t *testing.T) {
	p := &Provider{}
	req := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleAssistant, Content: "calling", ToolCalls: []provider.ToolCall{
			{ID: "t1", Name: "read_file", Arguments: `{"path":"x.go"}`},
		}},
	}}
	data, _ := json.Marshal(p.buildRequest(req))
	s := string(data)
	if !strings.Contains(s, `"type":"tool_use"`) || !strings.Contains(s, `"name":"read_file"`) || !strings.Contains(s, `"input":{"path":"x.go"}`) {
		t.Errorf("tool_use must serialize with structured id/name/input, got: %s", s)
	}
	if strings.Contains(s, `\"name\":\"read_file\"`) {
		t.Errorf("tool_use must NOT be JSON-stringified into a text field, got: %s", s)
	}
}

// A tool result must serialize as a structured tool_result block.
func TestBuildRequest_ToolResultWireFormat(t *testing.T) {
	p := &Provider{}
	req := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleTool, ToolCallID: "t1", Content: `quote " and newline`},
	}}
	data, _ := json.Marshal(p.buildRequest(req))
	s := string(data)
	if !strings.Contains(s, `"type":"tool_result"`) || !strings.Contains(s, `"tool_use_id":"t1"`) {
		t.Errorf("tool_result must serialize with structured tool_use_id, got: %s", s)
	}
	// content with a quote must be valid JSON (not broken by unescaped %s)
	var probe map[string]any
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Errorf("request with a quoted tool-result content must be valid JSON: %v", err)
	}
}
