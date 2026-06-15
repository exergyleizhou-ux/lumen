package plugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// ── JSON-RPC types ─────────────────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// ── Client ──────────────────────────────────────────────────

// Client is a JSON-RPC client communicating with an MCP server over stdio.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	seq    atomic.Int64
	mu     sync.Mutex // guards stdin writes
	resps  map[int64]chan jsonRPCResponse
	respMu sync.Mutex
	closed bool
}

func startClient(cfg ServerConfig) (*Client, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Command, err)
	}

	c := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		resps:  map[int64]chan jsonRPCResponse{},
	}

	// Start the response reader goroutine
	go c.readResponses()

	// Initialize handshake
	if err := c.initialize(); err != nil {
		c.close()
		return nil, err
	}

	return c, nil
}

func (c *Client) initialize() error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "Lumen",
			"version": "0.1.0",
		},
	}
	_, err := c.call("initialize", params)
	return err
}

func (c *Client) listTools() ([]ToolDef, error) {
	result, err := c.call("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return wrapper.Tools, nil
}

func (c *Client) callTool(name string, args json.RawMessage) (string, error) {
	result, err := c.call("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}

	// Extract the text content from the MCP result
	var wrapper struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		// Fallback: return raw result
		return string(result), nil
	}
	if wrapper.IsError && len(wrapper.Content) > 0 {
		return "", fmt.Errorf("mcp tool error: %s", wrapper.Content[0].Text)
	}
	var texts []string
	for _, c := range wrapper.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// call sends a JSON-RPC request and waits for the response.
func (c *Client) call(method string, params any) (json.RawMessage, error) {
	id := c.seq.Add(1)

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}

	respCh := make(chan jsonRPCResponse, 1)
	c.respMu.Lock()
	if c.closed {
		c.respMu.Unlock()
		return nil, fmt.Errorf("client closed")
	}
	c.resps[id] = respCh
	c.respMu.Unlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	body = append(body, '\n')

	c.mu.Lock()
	_, err = c.stdin.Write(body)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	resp := <-respCh

	c.respMu.Lock()
	delete(c.resps, id)
	c.respMu.Unlock()

	if resp.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

func (c *Client) readResponses() {
	scanner := bufio.NewScanner(c.stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		c.respMu.Lock()
		ch, ok := c.resps[resp.ID]
		c.respMu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

func (c *Client) close() error {
	c.respMu.Lock()
	c.closed = true
	for id, ch := range c.resps {
		ch <- jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &jsonRPCError{Code: -32000, Message: "server disconnected"},
		}
	}
	c.resps = map[int64]chan jsonRPCResponse{}
	c.respMu.Unlock()

	c.stdin.Close()
	c.stdout.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}
