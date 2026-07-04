package mcplife

import (
	"encoding/json"
	"os"
	"testing"
)

// ── Tests ──────────────────────────────────────────────────────────

// TestMockServer is both a test helper (when MCPLIFE_MOCK_SERVER=1) and a
// sentinel test that we can target with -test.run for re-exec.
func TestMockServer(t *testing.T) {
	if os.Getenv("MCPLIFE_MOCK_SERVER") != "1" {
		t.Skip("sentinel for mock server re-exec")
	}
	RunMockStdioServer()
}

func TestNewMCPClient_Initialize(t *testing.T) {
	c := NewTestMockClient(t)

	err := c.Initialize("test-client", "0.0.1")
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if c.Capabilities.Tools == nil {
		t.Error("expected tools capability, got nil")
	}
	if c.ServerInfo == nil {
		t.Error("expected server info, got nil")
	}
	if name, _ := c.ServerInfo["name"].(string); name != "mock-server" {
		t.Errorf("expected server name 'mock-server', got %q", name)
	}
}

func TestNewMCPClient_ListTools(t *testing.T) {
	c := NewTestMockClient(t)

	if err := c.Initialize("test-client", "0.0.1"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	tools, err := c.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "mock_echo" {
		t.Errorf("expected tool name 'mock_echo', got %q", tools[0].Name)
	}
	if tools[0].Description != "Echoes back the input message" {
		t.Errorf("unexpected description: %q", tools[0].Description)
	}
}

func TestNewMCPClient_CallTool(t *testing.T) {
	c := NewTestMockClient(t)

	if err := c.Initialize("test-client", "0.0.1"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	result, err := c.CallTool("mock_echo", map[string]any{
		"message": "hello world",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected content type 'text', got %q", result.Content[0].Type)
	}
	if result.Content[0].Text != "echo: hello world" {
		t.Errorf("expected 'echo: hello world', got %q", result.Content[0].Text)
	}
}

func TestMCPClientInterface_Connect(t *testing.T) {
	c := NewTestMockClient(t)

	tools, err := c.Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(tools) != 1 || tools[0] != "mock_echo" {
		t.Errorf("expected ['mock_echo'], got %v", tools)
	}
}

func TestMCPClientInterface_CallTool(t *testing.T) {
	c := NewTestMockClient(t)

	if _, err := c.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	args := json.RawMessage(`{"message":"from interface"}`)
	result, err := c.CallToolRaw("mock_echo", args)
	if err != nil {
		t.Fatalf("CallTool (interface): %v", err)
	}
	if result != "echo: from interface" {
		t.Errorf("expected 'echo: from interface', got %q", result)
	}
}

func TestDisconnect(t *testing.T) {
	c := NewTestMockClient(t)

	if err := c.Initialize("test-client", "0.0.1"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if err := c.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	if err := c.HealthCheck(); err != nil {
		t.Logf("HealthCheck after disconnect (expected): %v", err)
	}
}

func TestClose(t *testing.T) {
	c := NewTestMockClient(t)

	if err := c.Initialize("test-client", "0.0.1"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
