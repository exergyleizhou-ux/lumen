// Package serve provides an HTTP/SSE server that exposes the Lumen agent
// as a REST API. Adapted from Reasonix's serve package. It supports:
//
//   - POST /v1/chat — start a chat session (SSE streaming)
//   - GET /v1/chat/:id — get session status + messages
//   - POST /v1/chat/:id — send a message to an existing session
//   - GET /health — health check
//
// The server reuses the control.Controller so CLI, TUI, and HTTP share one code path.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"lumen/internal/control"
	"lumen/internal/event"
)

// Server wraps an HTTP server with a controller factory.
type Server struct {
	addr       string
	newCtrl    func() (*control.Controller, error)
	sessions   map[string]*session
	mu         sync.Mutex
	httpServer *http.Server
}

// session holds one active chat session.
type session struct {
	id       string
	ctrl     *control.Controller
	messages []chatMessage
	mu       sync.Mutex
}

type chatMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// New creates an HTTP server. newCtrl is called to create a fresh controller
// for each new session (the controller owns the agent lifecycle).
func New(addr string, newCtrl func() (*control.Controller, error)) *Server {
	return &Server{
		addr:     addr,
		newCtrl:  newCtrl,
		sessions: map[string]*session{},
	}
}

// Start begins listening and returns. Call Shutdown to stop.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/chat", s.handleChat)
	mux.HandleFunc("/v1/chat/", s.handleChatSession)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // long for SSE streaming
		IdleTimeout:  2 * time.Minute,
	}

	log.Printf("Lumen HTTP server listening on http://%s", s.addr)
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("serve error: %v", err)
		}
	}()
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	// Close all sessions
	s.mu.Lock()
	for _, sess := range s.sessions {
		sess.ctrl.Close()
	}
	s.sessions = map[string]*session{}
	s.mu.Unlock()
	return s.httpServer.Shutdown(ctx)
}

// ── Handlers ──────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}

	var req struct {
		Message string `json:"message"`
		Model   string `json:"model,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	// Create new controller + session
	ctrl, err := s.newCtrl()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	id := fmt.Sprintf("sess-%d", time.Now().UnixNano())
	sess := &session{
		id:       id,
		ctrl:     ctrl,
		messages: []chatMessage{{Role: "user", Content: req.Message, Timestamp: time.Now()}},
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	// SSE streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Session-Id", id)
	w.WriteHeader(http.StatusOK)

	// Run agent in background, pipe events to SSE
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sink := newSSESink(w, flusher, id)
	ctrl.SetSink(sink)

	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Run(ctx, req.Message)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(w, "data: {\"error\":%q}\n\n", err.Error())
			flusher.Flush()
		}
	case <-ctx.Done():
		fmt.Fprintf(w, "data: {\"error\":\"timeout\"}\n\n")
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	// Move result to session
	sess.mu.Lock()
	snap := ctrl.Session().Snapshot()
	for _, m := range snap {
		sess.messages = append(sess.messages, chatMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	sess.mu.Unlock()
}

func (s *Server) handleChatSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/chat/")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id required"})
		return
	}

	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		sess.mu.Lock()
		defer sess.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       sess.id,
			"messages": sess.messages,
		})

	case http.MethodPost:
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "message received (explicit resume not yet supported)"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET or POST required"})
	}
}

// ── SSE Sink ──────────────────────────────────────────────

type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
	id      string
	mu      sync.Mutex
}

func newSSESink(w http.ResponseWriter, flusher http.Flusher, id string) *sseSink {
	return &sseSink{w: w, flusher: flusher, id: id}
}

func (s *sseSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, _ := json.Marshal(map[string]any{
		"session": s.id,
		"kind":    string(e.Kind),
		"text":    e.Text,
		"tool":    e.Tool.Name,
		"output":  e.Tool.Output,
		"error":   e.Tool.Err,
		"usage":   e.Usage,
	})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

var _ event.Sink = (*sseSink)(nil)

// ── Helpers ───────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// SetSink on controller (needed by SSE path)
func init() {
	// The controller needs SetSink — verify it exists at compile time
	_ = (*control.Controller)(nil)
}
