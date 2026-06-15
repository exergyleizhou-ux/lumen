// Package lsp implements a Language Server Protocol client over stdio
// JSON-RPC. It provides diagnostics, hover, definition, references, and
// symbols — the core capabilities a coding agent needs to navigate source
// code. Adapted from claw-code's lsp_client.rs and Reasonix's lsp/.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

type jsonRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ── LSP types ──────────────────────────────────────────────

// Diagnostic represents a compiler/linter warning or error.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1=error, 2=warning, 3=info, 4=hint
	Code     string `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Range is a half-open interval [start, end) in a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position is a zero-based line and character offset.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// HoverResult holds the hover information for a symbol.
type HoverResult struct {
	Contents string `json:"contents"` // markdown string
	Range    *Range `json:"range,omitempty"`
}

// Location points to a range in a file.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// SymbolInformation describes a symbol in the workspace.
type SymbolInformation struct {
	Name     string   `json:"name"`
	Kind     int      `json:"kind"`
	Location Location `json:"location"`
}

// ── Client ──────────────────────────────────────────────────

// Client is a JSON-RPC client communicating with a language server over stdio.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	seq    atomic.Int64
	mu     sync.Mutex
	resps  map[int64]chan jsonRPCResponse
	respMu sync.Mutex
	closed bool

	// Diagnostics cache (per-URI)
	diagMu sync.RWMutex
	diags  map[string][]Diagnostic
}

// StartClient launches a language server binary and performs the initialize
// handshake. rootURI is the workspace root as a file:// URI.
func StartClient(command string, args []string, rootURI string) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	c := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		resps:  map[int64]chan jsonRPCResponse{},
		diags:  map[string][]Diagnostic{},
	}

	go c.readLoop()

	if err := c.initialize(rootURI); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	return c, nil
}

func (c *Client) initialize(rootURI string) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"diagnostic": map[string]any{"dynamicRegistration": false},
			},
		},
	}
	if _, err := c.call("initialize", params); err != nil {
		return err
	}
	// Send initialized notification
	c.notify("initialized", map[string]any{})
	return nil
}

// ── Public API ────────────────────────────────────────────

// Diagnostics returns cached diagnostics for a file path (not URI).
func (c *Client) Diagnostics(path string) []Diagnostic {
	c.diagMu.RLock()
	defer c.diagMu.RUnlock()
	return c.diags[pathToURI(path)]
}

// RequestDiagnostics triggers a textDocument/diagnostic request for a path.
func (c *Client) RequestDiagnostics(ctx context.Context, path string) ([]Diagnostic, error) {
	uri := pathToURI(path)
	result, err := c.call("textDocument/diagnostic", map[string]any{
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
	c.diags[uri] = wrapper.Items
	c.diagMu.Unlock()

	return wrapper.Items, nil
}

// Hover requests hover information for a position.
func (c *Client) Hover(ctx context.Context, path string, line, character int) (*HoverResult, error) {
	result, err := c.call("textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": character},
	})
	if err != nil {
		return nil, err
	}

	var hover struct {
		Contents any    `json:"contents"`
		Range    *Range `json:"range,omitempty"`
	}
	if err := json.Unmarshal(result, &hover); err != nil {
		return nil, fmt.Errorf("parse hover: %w", err)
	}

	contents := ""
	switch v := hover.Contents.(type) {
	case string:
		contents = v
	case map[string]any:
		if lang, ok := v["language"].(string); ok {
			if val, ok := v["value"].(string); ok {
				contents = fmt.Sprintf("```%s\n%s\n```", lang, val)
			}
		}
	}

	return &HoverResult{Contents: contents, Range: hover.Range}, nil
}

// Definition returns the definition location(s) for a symbol.
func (c *Client) Definition(ctx context.Context, path string, line, character int) ([]Location, error) {
	result, err := c.call("textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": character},
	})
	if err != nil {
		return nil, err
	}

	var locs []Location
	if err := json.Unmarshal(result, &locs); err != nil {
		// Single location
		var loc Location
		if err2 := json.Unmarshal(result, &loc); err2 != nil {
			return nil, fmt.Errorf("parse definition: %w", err)
		}
		locs = []Location{loc}
	}
	return locs, nil
}

// References returns all references to a symbol.
func (c *Client) References(ctx context.Context, path string, line, character int) ([]Location, error) {
	result, err := c.call("textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line, "character": character},
		"context":      map[string]any{"includeDeclaration": false},
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

// Symbols returns workspace symbols matching query.
func (c *Client) Symbols(ctx context.Context, query string) ([]SymbolInformation, error) {
	result, err := c.call("workspace/symbol", map[string]any{
		"query": query,
	})
	if err != nil {
		return nil, err
	}

	var syms []SymbolInformation
	if err := json.Unmarshal(result, &syms); err != nil {
		return nil, fmt.Errorf("parse symbols: %w", err)
	}
	return syms, nil
}

// DidOpen notifies the server that a file was opened (for live diagnostics).
func (c *Client) DidOpen(path, content, languageID string) error {
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        pathToURI(path),
			"languageId": languageID,
			"version":    1,
			"text":       content,
		},
	})
	return nil
}

// Close shuts down the language server.
func (c *Client) Close() error {
	c.call("shutdown", nil)
	c.notify("exit", nil)

	c.respMu.Lock()
	c.closed = true
	for id, ch := range c.resps {
		ch <- jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &jsonRPCError{Code: -32000, Message: "server closed"}}
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

// ── JSON-RPC ─────────────────────────────────────────────

func (c *Client) call(method string, params any) (json.RawMessage, error) {
	id := c.seq.Add(1)
	paramsJSON, _ := json.Marshal(params)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: paramsJSON}

	respCh := make(chan jsonRPCResponse, 1)
	c.respMu.Lock()
	if c.closed {
		c.respMu.Unlock()
		return nil, fmt.Errorf("client closed")
	}
	c.resps[id] = respCh
	c.respMu.Unlock()

	body, _ := json.Marshal(req)
	body = append(body, '\n')
	c.mu.Lock()
	_, err := c.stdin.Write(body)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("lsp call %s timed out", method)
	case resp := <-respCh:
		c.respMu.Lock()
		delete(c.resps, id)
		c.respMu.Unlock()
		if resp.Error != nil {
			return nil, fmt.Errorf("lsp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *Client) notify(method string, params any) {
	paramsJSON, _ := json.Marshal(params)
	req := jsonRPCNotification{JSONRPC: "2.0", Method: method, Params: paramsJSON}
	body, _ := json.Marshal(req)
	body = append(body, '\n')
	c.mu.Lock()
	c.stdin.Write(body)
	c.mu.Unlock()
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Check if it's a notification first (no id field in JSON)
		var notif jsonRPCNotification
		if err := json.Unmarshal(line, &notif); err == nil && notif.Method != "" && !hasKey(line, "id") {
			c.handleNotification(notif)
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

func (c *Client) handleNotification(notif jsonRPCNotification) {
	switch notif.Method {
	case "textDocument/publishDiagnostics":
		var params struct {
			URI   string       `json:"uri"`
			Diags []Diagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(notif.Params, &params); err == nil {
			c.diagMu.Lock()
			c.diags[params.URI] = params.Diags
			c.diagMu.Unlock()
		}
	}
}

// ── Helpers ────────────────────────────────────────────────

func pathToURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	return "file://" + path
}

func uriToPath(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}

func hasKey(data []byte, key string) bool {
	return strings.Contains(string(data), `"`+key+`"`)
}
