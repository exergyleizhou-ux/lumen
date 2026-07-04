// Package mcplife — in-process mock MCP stdio server for tests.
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

// RunMockStdioServer serves MCP JSON-RPC on stdin/stdout until stdin closes.
// Used when the test binary re-execs with MCPLIFE_MOCK_SERVER=1.
func RunMockStdioServer() {
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

// NewTestMockClient launches the in-process mock MCP server and returns a client.
func NewTestMockClient(t testing.TB) *Client {
	t.Helper()

	exe := os.Args[0]
	cmd := exec.Command(exe, "-test.run", "^TestMockServer$", "--")
	cmd.Env = append(os.Environ(), "MCPLIFE_MOCK_SERVER=1")
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

	c := newClient(stdin, stdout, cmd, TransportContentLength)
	t.Cleanup(func() {
		c.Close()
		if stderr.Len() > 0 {
			t.Logf("mock server stderr: %s", stderr.String())
		}
	})
	return c
}

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

func writeMockMessage(w io.Writer, v any) {
	body, _ := json.Marshal(v)
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

func mockDispatch(req jsonrpcRequest) *jsonrpcResponse {
	switch req.Method {
	case "initialize":
		return mockInitialize(req.ID)
	case "tools/list":
		return mockListTools(req.ID)
	case "tools/call":
		return mockCallTool(req.ID, req.Params)
	case "resources/list":
		return mockListResources(req.ID)
	case "prompts/list":
		return mockListPrompts(req.ID)
	case "notifications/initialized":
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
	return &jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: json.RawMessage(b)}
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
	return &jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: json.RawMessage(b)}
}

func mockListResources(id int64) *jsonrpcResponse {
	result := map[string]any{"resources": []map[string]any{}}
	b, _ := json.Marshal(result)
	return &jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: json.RawMessage(b)}
}

func mockListPrompts(id int64) *jsonrpcResponse {
	result := map[string]any{"prompts": []map[string]any{}}
	b, _ := json.Marshal(result)
	return &jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: json.RawMessage(b)}
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
		content = []ToolContent{{Type: "text", Text: "echo: " + msg}}
	default:
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &jsonrpcError{Code: -32602, Message: "Unknown tool: " + p.Name},
		}
	}

	result := CallToolResult{Content: content, IsError: false}
	b, _ := json.Marshal(result)
	return &jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: json.RawMessage(b)}
}