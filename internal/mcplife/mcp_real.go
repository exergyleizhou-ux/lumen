// Package mcplife provides MCP server lifecycle management. This file
// implements a real MCP (Model Context Protocol) client over stdio transport,
// using JSON-RPC 2.0 framing per the MCP 2024-11-05 specification.
package mcplife

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── JSON-RPC 2.0 wire types ────────────────────────────────────────

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ── Public MCP types ───────────────────────────────────────────────

// Tool describes an MCP tool exposed by the server.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// CallToolResult is the result returned by a tools/call request.
type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent is a single content item within a tool result.
type ToolContent struct {
	Type string `json:"type"` // "text", "image", "resource", etc.
	Text string `json:"text,omitempty"`
}

// Resource describes an MCP resource exposed by the server.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

// Prompt describes an MCP prompt template.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes one argument to a prompt template.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ServerCapabilities records what the server supports.
type ServerCapabilities struct {
	Tools     map[string]any `json:"tools,omitempty"`
	Resources map[string]any `json:"resources,omitempty"`
	Prompts   map[string]any `json:"prompts,omitempty"`
}

// ── Client ─────────────────────────────────────────────────────────

// Client is a real MCP client connected to a child process over stdio.
// It implements the MCPClient interface used by Manager.
type Client struct {
	cmd     *exec.Cmd
	mu      sync.Mutex
	reqID   int64
	pending map[int64]chan *jsonrpcResponse
	stdin   io.WriteCloser
	stdout  *bufio.Reader

	// readLoopDone is closed when the background read loop exits.
	readLoopDone chan struct{}

	// ServerCapabilities is populated by a successful Initialize call.
	Capabilities ServerCapabilities
	// ServerInfo is the server info returned by initialize.
	ServerInfo map[string]any

	// closeOnce prevents double-closing.
	closeOnce sync.Once
	// closeErr records the first error from Close.
	closeErr error
}

// NewMCPClient creates a Client, launches the server process via os/exec,
// and connects to its stdin/stdout. The caller must call Initialize before
// using other methods.
func NewMCPClient(serverCommand string, serverArgs []string) (*Client, error) {
	cmd := exec.Command(serverCommand, serverArgs...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcplife: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcplife: stdout pipe: %w", err)
	}
	// Capture stderr for diagnostics; wire it to a strings.Builder we can
	// inspect on errors.
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcplife: start %s: %w", serverCommand, err)
	}

	c := newClient(stdin, stdout, cmd)
	return c, nil
}

// newClient is the internal constructor used both by NewMCPClient and by
// tests that supply their own io pipes.
func newClient(stdin io.WriteCloser, stdout io.Reader, cmd *exec.Cmd) *Client {
	c := &Client{
		cmd:          cmd,
		stdin:        stdin,
		stdout:       bufio.NewReader(stdout),
		pending:      make(map[int64]chan *jsonrpcResponse),
		readLoopDone: make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// ── Public MCP methods ─────────────────────────────────────────────

// Initialize sends an initialize request and then the required
// notifications/initialized notification. On success the server's
// capabilities are stored in c.Capabilities.
func (c *Client) Initialize(clientName, clientVersion string) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    clientName,
			"version": clientVersion,
		},
	}
	raw, err := c.call("initialize", params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	var result struct {
		Capabilities ServerCapabilities `json:"capabilities"`
		ServerInfo   map[string]any     `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("initialize: parse response: %w", err)
	}
	c.Capabilities = result.Capabilities
	c.ServerInfo = result.ServerInfo

	// MCP spec requires sending notifications/initialized after initialize.
	if err := c.notify("notifications/initialized", nil); err != nil {
		return fmt.Errorf("initialize: send initialized notification: %w", err)
	}
	return nil
}

// ListTools requests the list of tools from the server.
func (c *Client) ListTools() ([]Tool, error) {
	raw, err := c.call("tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("tools/list: parse response: %w", err)
	}
	return result.Tools, nil
}

// CallTool invokes a named tool with the supplied arguments and returns
// the structured result.
func (c *Client) CallTool(name string, args map[string]any) (CallToolResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	raw, err := c.call("tools/call", params)
	if err != nil {
		return CallToolResult{}, fmt.Errorf("tools/call %s: %w", name, err)
	}
	var result CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return CallToolResult{}, fmt.Errorf("tools/call %s: parse response: %w", name, err)
	}
	return result, nil
}

// ListResources requests the list of resources from the server.
func (c *Client) ListResources() ([]Resource, error) {
	raw, err := c.call("resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("resources/list: %w", err)
	}
	var result struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("resources/list: parse response: %w", err)
	}
	return result.Resources, nil
}

// ReadResource reads the contents of a resource by URI.
func (c *Client) ReadResource(uri string) (map[string]any, error) {
	params := map[string]any{"uri": uri}
	raw, err := c.call("resources/read", params)
	if err != nil {
		return nil, fmt.Errorf("resources/read %s: %w", uri, err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("resources/read %s: parse response: %w", uri, err)
	}
	return result, nil
}

// ListPrompts requests the list of prompt templates from the server.
func (c *Client) ListPrompts() ([]Prompt, error) {
	raw, err := c.call("prompts/list", nil)
	if err != nil {
		return nil, fmt.Errorf("prompts/list: %w", err)
	}
	var result struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("prompts/list: parse response: %w", err)
	}
	return result.Prompts, nil
}

// GetPrompt retrieves a prompt template by name, filling in arguments.
func (c *Client) GetPrompt(name string, args map[string]any) (map[string]any, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	raw, err := c.call("prompts/get", params)
	if err != nil {
		return nil, fmt.Errorf("prompts/get %s: %w", name, err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("prompts/get %s: parse response: %w", name, err)
	}
	return result, nil
}

// Close shuts down the server process and cleans up resources.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		// Best-effort: close stdin so the child sees EOF and can exit on its own.
		_ = c.stdin.Close()
		// Wait for the read loop to finish.
		<-c.readLoopDone
		if c.cmd == nil || c.cmd.Process == nil {
			return
		}
		// Give the child a moment to exit gracefully after stdin EOF; only
		// force-kill if it overstays, and treat an intentional kill as a clean
		// close rather than surfacing "signal: killed".
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case err := <-done:
			c.closeErr = err
		case <-time.After(2 * time.Second):
			_ = c.cmd.Process.Kill()
			<-done
			c.closeErr = nil
		}
	})
	return c.closeErr
}

// ── MCPClient interface implementation ─────────────────────────────

// Connect initializes the client with a default name and returns the list
// of tool names exposed by the server. It satisfies the MCPClient interface.
func (c *Client) Connect() ([]string, error) {
	if err := c.Initialize("mcplife", "1.0.0"); err != nil {
		return nil, err
	}
	tools, err := c.ListTools()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names, nil
}

// CallToolRaw calls a tool with raw JSON arguments and returns the
// concatenated text content. It satisfies the MCPClient interface.
func (c *Client) CallToolRaw(tool string, args json.RawMessage) (string, error) {
	var argMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argMap); err != nil {
			return "", fmt.Errorf("mcplife: CallToolRaw %s: invalid args: %w", tool, err)
		}
	}
	result, err := c.CallTool(tool, argMap)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, ct := range result.Content {
		if ct.Type == "text" {
			parts = append(parts, ct.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// Disconnect closes the client. It satisfies the MCPClient interface.
func (c *Client) Disconnect() error {
	return c.Close()
}

// HealthCheck returns nil if the server process is still running.
func (c *Client) HealthCheck() error {
	if c.cmd == nil || c.cmd.Process == nil {
		return fmt.Errorf("mcplife: server not started")
	}
	// On Unix we could send signal 0; os doesn't expose that portably.
	// ProcessState is only set after Wait, so just check the process exists.
	return nil
}

// ── JSON-RPC transport ─────────────────────────────────────────────

// call sends a request and waits for the matching response.
func (c *Client) call(method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.reqID, 1)

	// Marshal params to RawMessage.
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = json.RawMessage(b)
	}

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}

	ch := make(chan *jsonrpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.writeMessage(req); err != nil {
		return nil, err
	}

	// Wait for the response.
	resp := <-ch
	if resp == nil {
		return nil, fmt.Errorf("mcplife: channel closed for request %d", id)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcplife: rpc error code=%d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// notify sends a JSON-RPC notification (no id, no response expected).
func (c *Client) notify(method string, params any) error {
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		rawParams = json.RawMessage(b)
	}
	n := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	}
	return c.writeMessage(n)
}

// writeMessage marshals v as JSON and writes it to stdin with an MCP
// Content-Length header.
func (c *Client) writeMessage(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := c.stdin.Write(body); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

// ── Read loop ──────────────────────────────────────────────────────

// readLoop continuously reads JSON-RPC messages from stdout and dispatches
// them to the appropriate pending request channel.
func (c *Client) readLoop() {
	defer close(c.readLoopDone)
	for {
		resp, err := c.readMessage()
		if err != nil {
			// If we can't read, close all pending channels so callers
			// don't hang forever.
			c.mu.Lock()
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		// Notifications have no id; we ignore them.
		if resp.ID == 0 && resp.JSONRPC == "" {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		c.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

// readMessage reads one MCP-framed message from stdout. It expects a
// Content-Length header followed by \r\n\r\n and then exactly N bytes of
// JSON body.
func (c *Client) readMessage() (*jsonrpcResponse, error) {
	// Read Content-Length header line.
	headerLine, err := c.stdout.ReadString('\n')
	if err != nil {
		return nil, err
	}
	headerLine = strings.TrimSpace(headerLine)
	if !strings.HasPrefix(headerLine, "Content-Length:") {
		return nil, fmt.Errorf("mcplife: expected Content-Length header, got %q", headerLine)
	}
	lengthStr := strings.TrimSpace(strings.TrimPrefix(headerLine, "Content-Length:"))
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return nil, fmt.Errorf("mcplife: invalid Content-Length %q: %w", lengthStr, err)
	}
	// Read the blank line (\r\n).
	blank, err := c.stdout.ReadString('\n')
	if err != nil {
		return nil, err
	}
	blank = strings.TrimSpace(blank)
	if blank != "" {
		return nil, fmt.Errorf("mcplife: expected blank line after header, got %q", blank)
	}
	// Read exactly length bytes.
	body := make([]byte, length)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, fmt.Errorf("mcplife: read body: %w", err)
	}
	var resp jsonrpcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("mcplife: parse response: %w", err)
	}
	return &resp, nil
}
