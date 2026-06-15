// Package lsp (lsp_real.go) — production-grade LSP client that connects to
// gopls via stdin/stdout with proper Content-Length header framing per the
// Language Server Protocol specification.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Public types ──────────────────────────────────────────────

// LSPClient is a JSON-RPC 2.0 client communicating with a language server
// over stdin/stdout using Content-Length header framing.
type LSPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu      sync.Mutex
	reqID   int64
	pending map[int64]chan jsonRPCResponse

	diagMu      sync.RWMutex
	diagnostics map[string][]Diagnostic

	closed bool
}

// CompletionItem represents a single completion suggestion returned by the
// language server.
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"`
	Detail        string `json:"detail"`
	Documentation string `json:"documentation"`
	InsertText    string `json:"insertText"`
}

// Hover holds the hover information for a symbol.
type Hover struct {
	Contents string `json:"contents"`
	Range    Range  `json:"range"`
}

// ── Lifecycle ─────────────────────────────────────────────────

// StartGopls launches gopls as a subprocess, sends the initialize request
// with the given workspace root, and returns a ready LSPClient. The caller
// should defer Shutdown() to clean up.
func StartGopls(ctx context.Context, workspaceRoot string) (*LSPClient, error) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return nil, fmt.Errorf("gopls not found in PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, goplsPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// gopls logs to stderr; discard so it does not block.
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start gopls: %w", err)
	}

	c := &LSPClient{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      bufio.NewReader(stdout),
		pending:     make(map[int64]chan jsonRPCResponse),
		diagnostics: make(map[string][]Diagnostic),
	}

	go c.readLoop()

	rootURI := "file://" + workspaceRoot
	if err := c.initialize(ctx, rootURI); err != nil {
		c.Shutdown()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	return c, nil
}

// Shutdown sends the shutdown request followed by an exit notification and
// waits for the gopls process to exit.
func (c *LSPClient) Shutdown() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Best-effort shutdown + exit.
	c.call(context.Background(), "shutdown", nil)
	c.notify("exit", nil)

	// Drain pending callers.
	c.mu.Lock()
	for id, ch := range c.pending {
		ch <- jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &jsonRPCError{Code: -32000, Message: "server closed"},
		}
	}
	c.pending = nil
	c.mu.Unlock()

	c.stdin.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

// ── Document lifecycle ────────────────────────────────────────

// OpenDocument sends a textDocument/didOpen notification to the server so it
// can start tracking the file for live diagnostics.
func (c *LSPClient) OpenDocument(uri, text string) error {
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "go",
			"version":    1,
			"text":       text,
		},
	})
	return nil
}

// CloseDocument sends a textDocument/didClose notification.
func (c *LSPClient) CloseDocument(uri string) error {
	c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	return nil
}

// ── Diagnostics ───────────────────────────────────────────────

// GetDiagnostics requests diagnostics for the given URI via
// textDocument/diagnostic and returns them. Results are also cached and
// updated by push notifications from the server.
func (c *LSPClient) GetDiagnostics(ctx context.Context, uri string) ([]Diagnostic, error) {
	result, err := c.call(ctx, "textDocument/diagnostic", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Items []Diagnostic `json:"items"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		return nil, fmt.Errorf("parse diagnostics: %w", err)
	}

	c.diagMu.Lock()
	c.diagnostics[uri] = wrapper.Items
	c.diagMu.Unlock()

	return wrapper.Items, nil
}

// ── Completion ────────────────────────────────────────────────

// GetCompletion requests code completion at the given position.
func (c *LSPClient) GetCompletion(ctx context.Context, uri string, line, col int) ([]CompletionItem, error) {
	result, err := c.call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": col},
	})
	if err != nil {
		return nil, err
	}

	// The LSP spec allows two shapes: a flat array or {items: [...], isIncomplete: bool}.
	var items []CompletionItem

	// Try array first.
	var flat []CompletionItem
	if json.Unmarshal(result, &flat) == nil {
		return flat, nil
	}

	// Try wrapped.
	var wrapper struct {
		Items        []CompletionItem `json:"items"`
		IsIncomplete bool             `json:"isIncomplete"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		return nil, fmt.Errorf("parse completion: %w", err)
	}
	items = wrapper.Items

	// Flatten documentation — it can be a string or {kind, value}.
	for i := range items {
		items[i].Documentation = flattenDoc(items[i].Documentation)
	}

	return items, nil
}

// ── Hover ─────────────────────────────────────────────────────

// GetHover requests hover information at the given position.
func (c *LSPClient) GetHover(ctx context.Context, uri string, line, col int) (*Hover, error) {
	result, err := c.call(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": col},
	})
	if err != nil {
		return nil, err
	}

	var raw struct {
		Contents any    `json:"contents"`
		Range    *Range `json:"range,omitempty"`
	}
	if err := json.Unmarshal(result, &raw); err != nil {
		return nil, fmt.Errorf("parse hover: %w", err)
	}

	contents := flattenHoverContents(raw.Contents)
	r := Range{}
	if raw.Range != nil {
		r = *raw.Range
	}
	return &Hover{Contents: contents, Range: r}, nil
}

// ── Navigation ────────────────────────────────────────────────

// GetDefinition returns the definition location(s) for a symbol at the given
// position.
func (c *LSPClient) GetDefinition(ctx context.Context, uri string, line, col int) ([]Location, error) {
	result, err := c.call(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": col},
	})
	if err != nil {
		return nil, err
	}

	var locs []Location
	if err := json.Unmarshal(result, &locs); err != nil {
		var loc Location
		if err2 := json.Unmarshal(result, &loc); err2 != nil {
			return nil, fmt.Errorf("parse definition: %w", err)
		}
		locs = []Location{loc}
	}
	return locs, nil
}

// GetReferences returns all references to a symbol at the given position.
func (c *LSPClient) GetReferences(ctx context.Context, uri string, line, col int, includeDecl bool) ([]Location, error) {
	result, err := c.call(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": col},
		"context":      map[string]any{"includeDeclaration": includeDecl},
	})
	if err != nil {
		return nil, err
	}

	var locs []Location
	if err := json.Unmarshal(result, &locs); err != nil {
		return nil, fmt.Errorf("parse references: %w", err)
	}
	return locs, nil
}

// ── Initialization ────────────────────────────────────────────

func (c *LSPClient) initialize(ctx context.Context, rootURI string) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"diagnostic": map[string]any{"dynamicRegistration": true},
				"completion": map[string]any{
					"completionItem": map[string]any{
						"documentationFormat": []string{"markdown", "plaintext"},
					},
				},
				"hover": map[string]any{
					"contentFormat": []string{"markdown", "plaintext"},
				},
			},
		},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	c.notify("initialized", map[string]any{})
	return nil
}

// ── JSON-RPC transport ────────────────────────────────────────

func (c *LSPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client closed")
	}
	c.reqID++
	id := c.reqID
	ch := make(chan jsonRPCResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	paramsJSON, _ := json.Marshal(params)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}
	body, _ := json.Marshal(req)

	if err := c.write(body); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}

	timeout := 30 * time.Second
	if ctx == context.Background() || ctx == context.TODO() {
		// Use our own deadline.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("lsp call %s: %w", method, ctx.Err())
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("lsp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *LSPClient) notify(method string, params any) {
	paramsJSON, _ := json.Marshal(params)
	req := jsonRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
	}
	body, _ := json.Marshal(req)
	c.write(body) // best-effort, ignore error
}

// write sends a JSON message framed with a Content-Length header.
func (c *LSPClient) write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err := c.stdin.Write(data)
	return err
}

// ── Read loop ─────────────────────────────────────────────────

func (c *LSPClient) readLoop() {
	for {
		contentLength, err := c.readHeader()
		if err != nil {
			return
		}
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			return
		}
		c.dispatch(body)
	}
}

// readHeader reads HTTP-style headers until the empty \r\n line and returns
// the Content-Length value.
func (c *LSPClient) readHeader() (int, error) {
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return 0, fmt.Errorf("missing Content-Length header")
		}
		if strings.HasPrefix(line, "Content-Length:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			n, err := strconv.Atoi(v)
			if err != nil {
				return 0, fmt.Errorf("bad Content-Length: %s", v)
			}
			// Consume the blank \r\n separator line.
			sep, err := c.stdout.ReadString('\n')
			if err != nil {
				return 0, err
			}
			_ = sep
			return n, nil
		}
		// Ignore other headers (e.g. Content-Type).
	}
}

// dispatch routes an incoming message to either a pending response channel
// (if it has an id) or a notification handler.
func (c *LSPClient) dispatch(body []byte) {
	// Peek at the id field.
	var peek struct {
		ID     *int64 `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &peek); err != nil {
		return
	}

	if peek.Method != "" && peek.ID == nil {
		// Notification.
		var notif jsonRPCNotification
		if err := json.Unmarshal(body, &notif); err != nil {
			return
		}
		c.handleNotification(notif)
		return
	}

	// Response.
	var resp jsonRPCResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[resp.ID]
	if ok {
		delete(c.pending, resp.ID)
	}
	c.mu.Unlock()
	if ok {
		ch <- resp
	}
}

func (c *LSPClient) handleNotification(notif jsonRPCNotification) {
	switch notif.Method {
	case "textDocument/publishDiagnostics":
		var params struct {
			URI   string       `json:"uri"`
			Diags []Diagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(notif.Params, &params); err == nil {
			c.diagMu.Lock()
			c.diagnostics[params.URI] = params.Diags
			c.diagMu.Unlock()
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────

func flattenDoc(raw string) string {
	// Try to parse as MarkupContent.
	var mc struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if json.Unmarshal([]byte(raw), &mc) == nil && mc.Value != "" {
		return mc.Value
	}
	return raw
}

func flattenHoverContents(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		// MarkupContent: {"kind": "markdown", "value": "..."}
		if val, ok := x["value"].(string); ok {
			if lang, ok := x["language"].(string); ok && lang != "" {
				return fmt.Sprintf("```%s\n%s\n```", lang, val)
			}
			return val
		}
	}
	return ""
}
