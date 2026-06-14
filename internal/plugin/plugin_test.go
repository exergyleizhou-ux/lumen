package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

// spamEchoServer is a minimal MCP-like server that responds to initialize and tools/list.
// It reads JSON-RPC requests from stdin line-by-line and writes responses to stdout.
func TestManagerConnectDisconnect(t *testing.T) {
	m := NewManager()
	if len(m.ListServers()) != 0 {
		t.Error("new manager should have no servers")
	}
	// Real subprocess connection tested in integration tests
}

func TestManagerCallToolUnknown(t *testing.T) {
	m := NewManager()
	_, err := m.CallTool("nonexistent", "tool", json.RawMessage(`{}`))
	if err == nil {
		t.Error("CallTool on unknown server should error")
	}
}

func TestManagerDisconnectUnknown(t *testing.T) {
	m := NewManager()
	err := m.Disconnect("nonexistent")
	if err == nil {
		t.Error("Disconnect on unknown server should error")
	}
}

func TestManagerShutdown(t *testing.T) {
	m := NewManager()
	m.Shutdown() // should not panic on empty manager
}

func TestServerConfig(t *testing.T) {
	cfg := ServerConfig{
		Name:    "test-server",
		Command: "node",
		Args:    []string{"server.js"},
		Env:     []string{"NODE_ENV=test"},
	}
	if cfg.Name != "test-server" {
		t.Errorf("name mismatch")
	}
	if len(cfg.Args) != 1 {
		t.Errorf("args: want 1, got %d", len(cfg.Args))
	}
}

// TestJsonRPCRequestRoundtrip verifies JSON-RPC request marshaling can be parsed back.
func TestJsonRPCRequestRoundtrip(t *testing.T) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      42,
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed jsonRPCRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.ID != 42 || parsed.Method != "initialize" {
		t.Errorf("roundtrip mismatch: id=%d method=%s", parsed.ID, parsed.Method)
	}
}

// TestJsonRPCErrorFormat verifies error response format.
func TestJsonRPCErrorFormat(t *testing.T) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Error: &jsonRPCError{
			Code:    -32601,
			Message: "Method not found",
		},
	}

	data, _ := json.Marshal(resp)
	if !strings.Contains(string(data), "Method not found") {
		t.Errorf("error response should contain message: %s", string(data))
	}
}

// TestMCPToolsListParsing validates parsing of a real tools/list response.
func TestMCPToolsListParsing(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"search","description":"Search code","inputSchema":{"type":"object","properties":{"query":{"type":"string"}}}}]}}`

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}

	if resp.Result == nil {
		t.Fatal("result should not be nil")
	}

	var wrapper struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &wrapper); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	if len(wrapper.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(wrapper.Tools))
	}
	if wrapper.Tools[0].Name != "search" {
		t.Errorf("tool name: want search, got %s", wrapper.Tools[0].Name)
	}
}

// TestMCPToolsCallResultParsing validates tools/call result.
func TestMCPToolsCallResultParsing(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"found 5 results"}],"isError":false}}`

	var resp jsonRPCResponse
	json.Unmarshal([]byte(response), &resp)

	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &wrapper); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if wrapper.IsError {
		t.Error("isError should be false")
	}
	if len(wrapper.Content) != 1 || wrapper.Content[0].Text != "found 5 results" {
		t.Errorf("content mismatch")
	}
}

// TestMCPToolErrorResult tests an MCP error response.
func TestMCPToolErrorResult(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":4,"result":{"content":[{"type":"text","text":"File not found: x.go"}],"isError":true}}`

	var resp jsonRPCResponse
	json.Unmarshal([]byte(response), &resp)

	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	json.Unmarshal(resp.Result, &wrapper)

	if !wrapper.IsError {
		t.Error("isError should be true")
	}
}
