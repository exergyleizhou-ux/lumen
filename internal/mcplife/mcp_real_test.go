package mcplife

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// ── In-process mock MCP server ─────────────────────────────────────
//
// The test binary re-executes itself with MCPLIFE_MOCK_SERVER=1 to act as
// a minimal MCP server over stdin/stdout. This avoids requiring any
// external binary.

// runMockServer reads JSON-RPC requests from stdin, dispatches them, and
// writes responses to stdout. It runs until stdin is closed.
func runMockServer() {
	scanner := bufio.NewReader(os.Stdin)
	for {
		respBytes, err := readMockMessage(scanner)
		if err != nil {
			if err == io.EOF {
				return
			}
			fmt.Fprintf(os.Stderr, "mock server read error: %v\n", err)
			return
		}
		var req jsonrpcRequest
		if err := json.Unmarshal(respBytes, &req); err != nil {
			fmt.Fprintf(os.Stderr, "mock server unmarshal error: %v\n", err)
			return
		}
		response := mockDispatch(req)
		writeMockMessage(os.Stdout, response)
	}
}

// readMockMessage reads one Content-Length framed message.
func readMockMessage(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "Content-Length:") {
		return nil, fmt.Errorf("expected Content-Length, got %q", line)
	}
	lengthStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return nil, err
	}
	// read blank line
	blank, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	blank = strings.TrimSpace(blank)
	if blank != "" {
		return nil, fmt.Errorf("expected blank, got %q", blank)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// writeMockMessage writes one Content-Length framed message.
func writeMockMessage(w io.Writer, v any) {
	body, _ := json.Marshal(v)
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

// mockDispatch handles a single JSON-RPC request and returns the response.
// It handles: initialize, tools/list, tools/call, notifications/initialized.
func mockDispatch(req jsonrpcRequest) *jsonrpcResponse {
	switch req.Method {
	case "initialize":
		return mockInitialize(req.ID)
	case "tools/list":
		return mockListTools(req.ID)
	case "tools/call":
		return mockCallTool(req.ID, req.Params)
	case "notifications/initialized":
		// Notification – no response expected.
		return nil
	default:
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &jsonrpcError{
				Code:    -32601,
				Message: "Method not found: " + req.Method,
			},
		}
	}
}

func mockInitialize(id int64) *jsonrpcResponse {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]any{
			"name":    "mock-server",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}
	b, _ := json.Marshal(result)
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  json.RawMessage(b),
	}
}

func mockListTools(id int64) *jsonrpcResponse {
	result := map[string]any{
		"tools": []map[string]any{
			{
				"name":        "mock_echo",
				"description": "Echoes back the input message",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"message": map[string]any{
							"type":        "string",
							"description": "The message to echo",
						},
					},
					"required": []string{"message"},
				},
			},
		},
	}
	b, _ := json.Marshal(result)
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  json.RawMessage(b),
	}
}

func mockCallTool(id int64, params json.RawMessage) *jsonrpcResponse {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &jsonrpcError{Code: -32602, Message: "Invalid params: " + err.Error()},
		}
	}

	var content []ToolContent
	switch p.Name {
	case "mock_echo":
		msg, _ := p.Arguments["message"].(string)
		content = []ToolContent{
			{Type: "text", Text: "echo: " + msg},
		}
	default:
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &jsonrpcError{Code: -32602, Message: "Unknown tool: " + p.Name},
		}
	}

	result := CallToolResult{
		Content: content,
		IsError: false,
	}
	b, _ := json.Marshal(result)
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  json.RawMessage(b),
	}
}

// ── Tests ──────────────────────────────────────────────────────────

// TestMockServer is both a test helper (when MCPLIFE_MOCK_SERVER=1) and a
// sentinel test that we can target with -test.run for re-exec.
func TestMockServer(t *testing.T) {
	if os.Getenv("MCPLIFE_MOCK_SERVER") != "1" {
		t.Skip("sentinel for mock server re-exec")
	}
	runMockServer()
}

// startMockServer launches the test binary itself as a mock MCP server.
// Caller must close the returned client when done.
func startMockServer(t *testing.T) *Client {
	t.Helper()

	// We need to re-exec the test binary. os.Args[0] points to the
	// compiled test binary when running under 'go test'.
	exe := os.Args[0]
	cmd := exec.Command(exe, "-test.run", "^TestMockServer$", "--")
	cmd.Env = append(os.Environ(), "MCPLIFE_MOCK_SERVER=1")
	// Prevent the subprocess from inheriting our stdin/stdout; the client
	// will use pipes.
	cmd.Stdin = nil
	cmd.Stdout = nil

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start mock server: %v", err)
	}

	c := newClient(stdin, stdout, cmd)
	t.Cleanup(func() {
		c.Close()
		if stderr.Len() > 0 {
			t.Logf("mock server stderr: %s", stderr.String())
		}
	})
	return c
}

func TestNewMCPClient_Initialize(t *testing.T) {
	c := startMockServer(t)

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
	c := startMockServer(t)

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
	c := startMockServer(t)

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
	c := startMockServer(t)

	tools, err := c.Connect()
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(tools) != 1 || tools[0] != "mock_echo" {
		t.Errorf("expected ['mock_echo'], got %v", tools)
	}
}

func TestMCPClientInterface_CallTool(t *testing.T) {
	c := startMockServer(t)

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
	c := startMockServer(t)

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
	c := startMockServer(t)

	if err := c.Initialize("test-client", "0.0.1"); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
