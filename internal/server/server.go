// Package server provides an HTTP+SSE server for Lumen (goal:d6aa846b round9).
// It wraps the control.Controller and exposes:
//
//	GET  /            — web UI (embedded HTML/JS)
//	POST /v1/chat     — SSE-streaming chat completion
//	GET  /v1/models   — list available models
//	GET  /v1/status   — agent status (running/idle)
//	GET  /v1/sessions — list recent sessions
//	POST /v1/memories — list/save/delete memories
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/memory"
	"lumen/internal/permission"
)

// Config holds the server configuration.
type Config struct {
	Addr   string // listen address, e.g. ":8080"
	Ctrl   *control.Controller
	Static string // path to static files (empty = use embedded)
}

// Server wraps the HTTP server.
type Server struct {
	cfg    Config
	mux    *http.ServeMux
	memDir string
	// turnMu serializes chat turns: every request shares one Controller/Agent/
	// Session, which Configure+Run mutate without internal locking. Without this,
	// two concurrent POST /v1/chat requests race those fields and interleave
	// messages into one session.
	turnMu sync.Mutex
	planMu sync.Mutex
	plan   planState

	approvals   sync.Map
	approvalSeq atomic.Uint64
}

// New creates a new Server.
func New(cfg Config) (*Server, error) {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.memDir = filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "memories")
	s.routes()
	return s, nil
}

// ListenAndServe starts the HTTP server. Blocks until error.
func (s *Server) ListenAndServe() error {
	log.Printf("lumen serve: listening on %s", s.cfg.Addr)
	log.Printf("  web UI:  http://localhost%s/", s.cfg.Addr)
	log.Printf("  API:     http://localhost%s/v1/chat", s.cfg.Addr)
	return http.ListenAndServe(s.cfg.Addr, s.mux)
}

// ── Routes ──────────────────────────────────────────────────

func (s *Server) routes() {
	s.mountStatic()
	s.routesAPI()
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/v1/chat", s.handleChat)
	s.mux.HandleFunc("/v1/models", s.handleModels)
	s.mux.HandleFunc("/v1/status", s.handleStatus)
	s.mux.HandleFunc("/v1/sessions", s.handleSessions)
	s.mux.HandleFunc("/v1/memories", s.handleMemories)
	s.mux.HandleFunc("/v1/workflow", s.handleWorkflow)
}

// ── Web UI (embedded static — Claude Code–grade panel) ───────

func (s *Server) mountStatic() {
	assets, err := fs.Sub(staticFS, "static/assets")
	if err != nil {
		return
	}
	s.mux.Handle("/assets/", http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
		}
		http.FileServer(http.FS(assets)).ServeHTTP(w, r)
	})))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "ui missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self' https://demo.oasisdata2026.xyz https://*.oasisdata2026.xyz")
	w.Write(data)
}

// ── SSE Chat ────────────────────────────────────────────────

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt   string   `json:"prompt"`
		Images   []string `json:"images"` // base64-encoded images
		APIKey   string   `json:"api_key,omitempty"`
		Provider string   `json:"provider,omitempty"`
		Model    string   `json:"model,omitempty"`
		Mode     string   `json:"mode,omitempty"` // agent · plan · bypass · default · accept-edits
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		http.Error(w, `{"error":"prompt required"}`, http.StatusBadRequest)
		return
	}

	// Allow runtime key like Lumen Science GUI: set env so Configure picks it up
	if req.APIKey != "" {
		envVar := "DEEPSEEK_API_KEY"
		if req.Provider == "qwen" {
			envVar = "DASHSCOPE_API_KEY"
		} else if req.Provider == "moonshot" {
			envVar = "MOONSHOT_API_KEY"
		} else if req.Provider == "zhipu" {
			envVar = "ZHIPU_API_KEY"
		}
		os.Setenv(envVar, req.APIKey)
		if req.Model != "" {
			// Note: full model override would require more ctrl changes; UI shows it
		}
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// SSE sink — emits each event as an SSE data frame
	sink := sseSink{w: w, flusher: flusher}

	// Serialize turns: the shared Controller/Agent/Session is not safe for
	// concurrent Configure+Run. One chat at a time (acceptable for a single-
	// session agent); concurrent requests queue here rather than corrupt state.
	s.turnMu.Lock()
	defer s.turnMu.Unlock()

	if os.Getenv("LUMEN_DEMO") == "1" && req.APIKey == "" { // goal:d6aa846b round9
		// Demo echo only when no runtime API key — browser-local keys still hit the real provider.
		sink.emit("turn_started", "")
		sink.emit("text", "[Demo mode] You said: "+req.Prompt)
		if len(req.Images) > 0 {
			sink.emit("text", " (with "+strconv.Itoa(len(req.Images))+" image(s))")
		}
		sink.emit("turn_done", "")
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	if err := s.cfg.Ctrl.Configure(sink, nil, ""); err != nil {
		sink.emit("turn_started", "")
		sink.emit("error", err.Error())
		sink.emit("turn_done", "")
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	s.cfg.Ctrl.SetApprover(s.webApprover(func(kind string, payload map[string]any) {
		sink.emitPayload(kind, payload)
	}))

	if req.Mode != "" {
		s.cfg.Ctrl.SetPermissionMode(parseUIMode(req.Mode))
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sink.emit("turn_started", "")
	if s.cfg.Ctrl.PermissionMode() == permission.ModePlan || req.Mode == "plan" {
		if err := s.cfg.Ctrl.Plan(ctx, req.Prompt); err != nil {
			sink.emit("error", err.Error())
		}
	} else if err := s.cfg.Ctrl.Run(ctx, req.Prompt); err != nil {
		sink.emit("error", err.Error())
	}
	sink.emit("turn_done", "")

	// Send terminal event
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s sseSink) Emit(e event.Event) {
	data, _ := json.Marshal(map[string]any{
		"kind":      e.Kind,
		"text":      e.Text,
		"tool":      e.Tool,
		"usage":     e.Usage,
		"timestamp": e.Timestamp,
	})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

func (s sseSink) emit(kind, text string) {
	data, _ := json.Marshal(map[string]any{"kind": kind, "text": text})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

func (s sseSink) emitPayload(kind string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["kind"] = kind
	data, _ := json.Marshal(payload)
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

// ── REST ────────────────────────────────────────────────────

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	ctrl := s.cfg.Ctrl
	jsonOK(w, map[string]any{
		"provider": ctrl.ProviderName(),
		"model":    ctrl.ModelName(),
		"mode":     string(ctrl.PermissionMode()),
		"ui_mode":  uiModeFromPermission(ctrl.PermissionMode()),
		"presets":  config.ModelPresets(),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, map[string]any{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
		"agent":  s.statusData(),
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, _ *http.Request) {
	histDir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "history")
	entries, _ := os.ReadDir(histDir)
	var sessions []map[string]any
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, _ := e.Info()
		sessions = append(sessions, map[string]any{
			"name":  e.Name(),
			"size":  info.Size(),
			"mtime": info.ModTime().Format(time.RFC3339),
		})
	}
	jsonOK(w, map[string]any{"sessions": sessions})
}

func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request) {
	store, err := memory.NewStore(s.memDir)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		jsonOK(w, map[string]any{"memories": store.List()})
	case http.MethodPost:
		var req struct {
			Action string       `json:"action"` // "save" or "delete"
			Entry  memory.Entry `json:"entry"`
			Name   string       `json:"name"` // for delete
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.Action {
		case "save":
			if err := store.Save(req.Entry); err != nil {
				jsonErr(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonOK(w, map[string]any{"saved": req.Entry.Name})
		case "delete":
			if err := store.Delete(req.Name); err != nil {
				jsonErr(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonOK(w, map[string]any{"deleted": req.Name})
		default:
			jsonErr(w, "unknown action", http.StatusBadRequest)
		}
	default:
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── Helpers ─────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ── Embedded web UI ─────────────────────────────────────────

