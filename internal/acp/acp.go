// Package acp implements the Agent Communication Protocol — a JSON-RPC
// interface for IDE integration (Zed, Cursor, VS Code). It exposes the
// Lumen agent as an ACP-compliant server that editors can connect to
// for inline completions, diagnostics, and chat.
// Adapted from Reasonix's acp/ package and Zed's ACP spec.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// ── JSON-RPC types ─────────────────────────────────────────

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ── ACP message types ──────────────────────────────────────

type ChatRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message"`
	Model     string `json:"model,omitempty"`
}

type ChatResponse struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

type DiagnosticRequest struct {
	FilePath string `json:"file_path"`
}

type DiagnosticResponse struct {
	FilePath string       `json:"file_path"`
	Items    []DiagItem   `json:"items"`
}

type DiagItem struct {
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
	Severity int    `json:"severity"` // 1=error, 2=warning
}

type CompletionRequest struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Prefix   string `json:"prefix"`
}

type CompletionResponse struct {
	Items []CompletionItem `json:"items"`
}

type CompletionItem struct {
	Text     string `json:"text"`
	Label    string `json:"label"`
	Detail   string `json:"detail,omitempty"`
}

type StatusResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Model   string `json:"model"`
	Ready   bool   `json:"ready"`
}

// ── Handler interface ──────────────────────────────────────

// Handler processes ACP requests. Implementations can delegate to the
// agent controller or a mock for testing.
type Handler interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Diagnostics(ctx context.Context, req DiagnosticRequest) (DiagnosticResponse, error)
	Completion(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	Status(ctx context.Context) StatusResponse
}

// ── Server ─────────────────────────────────────────────────

// Server listens on a Unix socket (or TCP port) and handles ACP JSON-RPC.
type Server struct {
	handler Handler
	ln      net.Listener
	seq     atomic.Int64
	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	done    chan struct{}
}

// NewServer creates an ACP server.
func NewServer(handler Handler) *Server {
	return &Server{
		handler: handler,
		conns:   map[net.Conn]struct{}{},
		done:    make(chan struct{}),
	}
}

// ListenUnix starts listening on a Unix domain socket at the given path.
func (s *Server) ListenUnix(socketPath string) error {
	os.Remove(socketPath) // clean stale socket
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", socketPath, err)
	}
	s.ln = ln
	log.Printf("ACP listening on unix:%s", socketPath)
	go s.accept()
	return nil
}

// ListenTCP starts listening on a TCP address.
func (s *Server) ListenTCP(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen tcp %s: %w", addr, err)
	}
	s.ln = ln
	log.Printf("ACP listening on tcp:%s", addr)
	go s.accept()
	return nil
}

func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeResponse(conn, response{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}

		go s.dispatch(conn, req)
	}
}

func (s *Server) dispatch(conn net.Conn, req request) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result json.RawMessage
	var rpcErr *rpcError

	switch req.Method {
	case "initialize":
		result, _ = json.Marshal(map[string]any{
			"protocolVersion": "0.1.0",
			"serverInfo": map[string]string{
				"name":    "Lumen",
				"version": "0.2.0",
			},
			"capabilities": map[string]bool{
				"chat":        true,
				"diagnostics": true,
				"completion":  true,
			},
		})

	case "chat/send":
		var params ChatRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			rpcErr = &rpcError{Code: -32602, Message: "invalid params"}
		} else {
			resp, err := s.handler.Chat(ctx, params)
			if err != nil {
				rpcErr = &rpcError{Code: -32000, Message: err.Error()}
			} else {
				result, _ = json.Marshal(resp)
			}
		}

	case "diagnostics/get":
		var params DiagnosticRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			rpcErr = &rpcError{Code: -32602, Message: "invalid params"}
		} else {
			resp, err := s.handler.Diagnostics(ctx, params)
			if err != nil {
				rpcErr = &rpcError{Code: -32000, Message: err.Error()}
			} else {
				result, _ = json.Marshal(resp)
			}
		}

	case "completion/get":
		var params CompletionRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			rpcErr = &rpcError{Code: -32602, Message: "invalid params"}
		} else {
			resp, err := s.handler.Completion(ctx, params)
			if err != nil {
				rpcErr = &rpcError{Code: -32000, Message: err.Error()}
			} else {
				result, _ = json.Marshal(resp)
			}
		}

	case "status":
		status := s.handler.Status(ctx)
		result, _ = json.Marshal(status)

	case "shutdown":
		result, _ = json.Marshal(map[string]string{"status": "shutting_down"})
		go s.Shutdown()

	default:
		rpcErr = &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}

	if req.ID != 0 {
		s.writeResponse(conn, response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr})
	}
}

func (s *Server) writeResponse(conn net.Conn, resp response) {
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	conn.Write(data)
}

// Shutdown stops the server.
func (s *Server) Shutdown() {
	close(s.done)
	if s.ln != nil {
		s.ln.Close()
	}
	s.mu.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.conns = map[net.Conn]struct{}{}
	s.mu.Unlock()
}

// ── Default handler (delegates to shell) ────────────────────

// DefaultHandler implements Handler by shelling out to the lumen binary.
type DefaultHandler struct {
	model string
}

func NewDefaultHandler(model string) *DefaultHandler {
	return &DefaultHandler{model: model}
}

func (h *DefaultHandler) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if req.SessionID == "" {
		req.SessionID = fmt.Sprintf("acp-%d", time.Now().UnixNano())
	}
	// Shell out to lumen run
	cmd := exec.CommandContext(ctx, "lumen", "run", req.Message)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ChatResponse{}, fmt.Errorf("lumen run: %w\n%s", err, out)
	}
	return ChatResponse{SessionID: req.SessionID, Content: string(out)}, nil
}

func (h *DefaultHandler) Diagnostics(ctx context.Context, req DiagnosticRequest) (DiagnosticResponse, error) {
	return DiagnosticResponse{FilePath: req.FilePath}, nil
}

func (h *DefaultHandler) Completion(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}

func (h *DefaultHandler) Status(ctx context.Context) StatusResponse {
	return StatusResponse{
		Name:    "Lumen",
		Version: "0.2.0",
		Model:   h.model,
		Ready:   true,
	}
}

// ── Serve with stdin/stdout (for IDE subprocess mode) ──────

// ServeStdio reads JSON-RPC requests from stdin and writes responses
// to stdout. This is the mode used when an IDE launches Lumen as a
// subprocess (like gopls).
func ServeStdio(handler Handler) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)
	_ = &Server{handler: handler, conns: map[net.Conn]struct{}{}} // unused, but ensures handler is valid

	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			encoder.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		// Dispatch synchronously for stdio (no goroutine — must be ordered)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var result json.RawMessage
		var rpcErr *rpcError
		switch req.Method {
		case "initialize":
			result, _ = json.Marshal(map[string]any{
				"protocolVersion": "0.1.0",
				"serverInfo":      map[string]string{"name": "Lumen", "version": "0.2.0"},
				"capabilities":    map[string]bool{"chat": true, "diagnostics": true},
			})
		case "status":
			result, _ = json.Marshal(handler.Status(ctx))
		case "shutdown":
			result, _ = json.Marshal(map[string]string{"status": "ok"})
			cancel()
			return nil
		default:
			rpcErr = &rpcError{Code: -32601, Message: "not found"}
		}
		cancel()
		if req.ID != 0 {
			encoder.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr})
		}
	}
	return scanner.Err()
}

// ── Zed-specific integration ────────────────────────────────

// ZedConfig returns the JSON configuration Zed needs to connect to Lumen.
func ZedConfig(socketPath string) []byte {
	cfg := map[string]any{
		"name":        "lumen",
		"description": "Lumen coding agent",
		"transport":   "unix",
		"path":        socketPath,
		"capabilities": map[string]bool{
			"chat":        true,
			"diagnostics": true,
			"completion":  true,
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return data
}
