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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/approvalstate"
	"lumen/internal/artifact"
	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/hostedauth"
	"lumen/internal/lumenstore"
	"lumen/internal/memory"
	"lumen/internal/permission"
	"lumen/internal/runstate"
	"lumen/internal/runstate/pgstore"
	"lumen/internal/runtimeevidence"
	"lumen/internal/usage"
)

// Config holds the server configuration.
type Config struct {
	Addr               string // listen address, e.g. ":8080"
	Ctrl               *control.Controller
	Static             string // path to static files (empty = use embedded)
	Runs               *runstate.Manager
	Hosted             bool
	WorkbenchJWTSecret string
	// HostedProviders is the startup-resolved provider/model allowlist. API keys
	// are never accepted from requests.
	HostedProviders     []config.ProviderConfig
	Usage               usage.Store
	Approvals           approvalstate.Store
	Artifacts           artifact.Store
	ArtifactObjects     artifact.ObjectBackend
	HostedWorkspaceRoot string
	WorkbenchOrigin     string // exact http(s) Oasis parent origin; empty means same-origin
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

	approvals       sync.Map
	approvalSeq     atomic.Uint64
	runs            *runstate.Manager
	auth            *hostedauth.Verifier
	activeRuns      sync.Map // run_id -> activeRun
	controllers     *serverControllerPool
	hostedProviders map[string]config.ProviderConfig
	hostedDefault   *config.ProviderConfig
	usage           usage.Store
	approvalStore   approvalstate.Store
	artifactStore   artifact.Store
	evidence        runtimeevidence.Service
}

type activeRun struct {
	Owner  runstate.Owner
	Cancel context.CancelFunc
}

func ownerFromRequest(r *http.Request) runstate.Owner {
	if id, ok := hostedauth.FromContext(r.Context()); ok {
		return runstate.Owner{UserID: id.UserID, WorkspaceID: id.WorkspaceID}
	}
	return runstate.LocalOwner
}

func (s *Server) requestProvider(apiKey, providerName, model string) (*config.ProviderConfig, error) {
	if s.auth != nil {
		if providerName == "" && model == "" && s.hostedDefault != nil {
			copy := *s.hostedDefault
			return &copy, nil
		}
		var match *config.ProviderConfig
		for _, pc := range s.hostedProviders {
			if providerName != "" && pc.Name != providerName && pc.Kind != providerName {
				continue
			}
			if model != "" && pc.Model != model {
				continue
			}
			if match != nil {
				return nil, fmt.Errorf("provider and model selection is ambiguous")
			}
			copy := pc
			match = &copy
		}
		if match == nil {
			return nil, fmt.Errorf("provider/model is not in the hosted allowlist")
		}
		return match, nil
	}
	if apiKey == "" && providerName == "" && model == "" {
		return nil, nil
	}
	var base *config.ProviderConfig
	if s.cfg.Ctrl != nil {
		base = s.cfg.Ctrl.ProviderConfig()
	}
	if base == nil {
		base = &config.ProviderConfig{}
	}
	if providerName != "" && providerName != base.Name && providerName != base.Kind {
		preset := config.FindPreset(providerName)
		if preset == nil {
			for _, candidate := range config.ModelPresets() {
				if candidate.Provider == providerName {
					copy := candidate
					preset = &copy
					break
				}
			}
		}
		if preset == nil {
			return nil, fmt.Errorf("provider %q not found", providerName)
		}
		base.Name, base.Kind, base.BaseURL, base.Model = preset.Name, preset.Kind, preset.BaseURL, preset.Model
	}
	if model != "" {
		base.Model = model
	}
	if base.Name == "" {
		base.Name, base.Kind, base.Model = "request", "openai", model
	}
	if apiKey != "" {
		base.APIKey = apiKey
		base.APIKeyEnv = ""
	}
	return base, nil
}

func (s *Server) beginActiveRun(parent context.Context, owner runstate.Owner, runID string, timeout time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	s.activeRuns.Store(runID, activeRun{Owner: owner, Cancel: cancel})
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			s.activeRuns.Delete(runID)
			cancel()
		})
	}
	return ctx, cleanup
}

func (s *Server) cancelActiveRun(owner runstate.Owner, runID string) bool {
	value, ok := s.activeRuns.Load(runID)
	if !ok {
		return false
	}
	active, ok := value.(activeRun)
	if !ok || active.Owner != owner {
		return false
	}
	s.activeRuns.Delete(runID)
	active.Cancel()
	return true
}

// New creates a new Server.
func New(cfg Config) (*Server, error) {
	if cfg.WorkbenchOrigin == "" {
		cfg.WorkbenchOrigin = os.Getenv("WORKBENCH_PARENT_ORIGIN")
	}
	if cfg.Hosted && cfg.Runs == nil && os.Getenv("WORKBENCH_DATABASE_URL") == "" {
		return nil, errors.New("WORKBENCH_DATABASE_URL required in hosted mode")
	}
	if cfg.HostedWorkspaceRoot == "" {
		cfg.HostedWorkspaceRoot = os.Getenv("HOSTED_WORKSPACE_ROOT")
	}
	var verifier *hostedauth.Verifier
	if cfg.Hosted {
		var err error
		verifier, err = hostedauth.NewVerifier(cfg.WorkbenchJWTSecret)
		if err != nil {
			return nil, fmt.Errorf("hosted auth: %w", err)
		}
	}
	runs := cfg.Runs
	if runs == nil {
		if raw := os.Getenv("WORKBENCH_DATABASE_URL"); cfg.Hosted && raw != "" {
			if cfg.ArtifactObjects == nil {
				root := os.Getenv("WORKBENCH_OBJECT_DIR")
				if root == "" {
					return nil, errors.New("WORKBENCH_OBJECT_DIR required in hosted mode")
				}
				backend, backendErr := artifact.NewLocalBackend(root)
				if backendErr != nil {
					return nil, backendErr
				}
				cfg.ArtifactObjects = backend
			}
			store, err := pgstore.Open(raw)
			if err != nil {
				return nil, err
			}
			runs = runstate.NewManager(store)
			if cfg.Usage == nil {
				cfg.Usage = usage.PostgresStore{DB: store.DB()}
			}
			if cfg.Approvals == nil {
				cfg.Approvals = approvalstate.PostgresStore{DB: store.DB()}
			}
			if cfg.Artifacts == nil {
				cfg.Artifacts = artifact.PostgresStore{DB: store.DB(), Objects: cfg.ArtifactObjects}
			}
		} else {
			runs = runstate.NewManager(runstate.NewSQLiteStore(lumenstore.Default()))
		}
	}
	allowed := make(map[string]config.ProviderConfig)
	var hostedDefault *config.ProviderConfig
	for _, pc := range cfg.HostedProviders {
		allowed[pc.Name+"\x00"+pc.Model] = pc
		if hostedDefault == nil {
			copy := pc
			hostedDefault = &copy
		}
	}
	if cfg.Hosted && len(allowed) == 0 && cfg.Ctrl != nil {
		if pc := cfg.Ctrl.ProviderConfig(); pc != nil {
			allowed[pc.Name+"\x00"+pc.Model] = *pc
			hostedDefault = pc
		}
	}
	usageStore := cfg.Usage
	if usageStore == nil {
		usageStore = usage.NewMemoryStore()
	}
	approvalStore := cfg.Approvals
	if approvalStore == nil {
		approvalStore = approvalstate.NewMemoryStore()
	}
	artifactStore := cfg.Artifacts
	if artifactStore == nil {
		artifactStore = artifact.NewMemoryStore()
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux(), runs: runs, auth: verifier, controllers: newServerControllerPool(controllerLimits{}), hostedProviders: allowed, hostedDefault: hostedDefault, usage: usageStore, approvalStore: approvalStore, artifactStore: artifactStore}
	if ur, ok := usageStore.(runtimeevidence.UsageReader); ok {
		s.evidence = runtimeevidence.Service{Runs: runs, Approvals: approvalStore, Artifacts: artifactStore, Usage: ur}
	}
	s.memDir = filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "memories")
	s.routes()
	return s, nil
}

func (s *Server) handleBusiness(pattern string, handler http.HandlerFunc) {
	if s.auth != nil {
		s.mux.Handle(pattern, s.auth.RequireFor(codePermission)(handler))
		return
	}
	s.mux.HandleFunc(pattern, handler)
}

func codePermission(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/v1/runs/") {
		if strings.HasSuffix(r.URL.Path, "/cancel") {
			return "run:cancel"
		}
		if strings.Contains(r.URL.Path, "/artifacts/") && strings.HasSuffix(r.URL.Path, "/download") {
			return "artifact:read"
		}
		return "run:read"
	}
	if r.URL.Path == "/v1/approve" {
		return "approval:decide"
	}
	if strings.HasPrefix(r.URL.Path, "/api/files") {
		if r.Method == http.MethodGet {
			return "artifact:read"
		}
		return "code:run"
	}
	if r.URL.Path == "/v1/chat" || r.URL.Path == "/v1/command" || r.URL.Path == "/v1/mode" || r.URL.Path == "/v1/rewind" || r.URL.Path == "/v1/workflow" {
		return "code:run"
	}
	return "run:read"
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
	s.handleBusiness("/v1/chat", s.handleChat)
	s.handleBusiness("/v1/models", s.handleModels)
	s.handleBusiness("/v1/status", s.handleStatus)
	s.handleBusiness("/v1/sessions", s.handleSessions)
	s.handleBusiness("/v1/memories", s.handleMemories)
	s.handleBusiness("/v1/workflow", s.handleWorkflow)
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
	if raw, marshalErr := json.Marshal(cfgWorkbenchOrigin(s.cfg.WorkbenchOrigin)); marshalErr == nil {
		data = bytes.Replace(data, []byte("</head>"), append(append([]byte(`<script>window.__LUMEN_WORKBENCH_ORIGIN__=`), raw...), []byte(`;</script></head>`)...), 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	ancestors := "frame-ancestors 'self' https://demo.oasisdata2026.xyz https://*.oasisdata2026.xyz"
	if origin := cfgWorkbenchOrigin(s.cfg.WorkbenchOrigin); origin != "" {
		ancestors += " " + origin
	}
	w.Header().Set("Content-Security-Policy", ancestors)
	w.Write(data)
}

func cfgWorkbenchOrigin(value string) string {
	value = strings.TrimSuffix(strings.TrimSpace(value), "/")
	if value == "" {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return u.String()
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
		ParentID string   `json:"parent_run_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		http.Error(w, `{"error":"prompt required"}`, http.StatusBadRequest)
		return
	}

	// Request credentials must never mutate process state. Hosted credentials and
	// model routing are configured in the tenant Controller's immutable config.
	if s.auth != nil && req.APIKey != "" {
		jsonCodeErr(w, "provider_key_forbidden", "request provider keys are forbidden", http.StatusBadRequest)
		return
	}
	pc, err := s.requestProvider(req.APIKey, req.Provider, req.Model)
	if err != nil {
		jsonCodeErr(w, "provider_not_allowed", err.Error(), http.StatusBadRequest)
		return
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
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	rt.provider = pc
	owner, ctrl := rt.owner, rt.ctrl
	if req.ParentID != "" {
		if _, parentErr := s.runs.ValidateRetryParent(owner, req.ParentID); parentErr != nil {
			jsonCodeErr(w, "invalid_parent_run", "parent run must be an owned terminal run", http.StatusConflict)
			return
		}
	}

	// Serialize turns: the shared Controller/Agent/Session is not safe for
	// concurrent Configure+Run. One chat at a time (acceptable for a single-
	// session agent); concurrent requests queue here rather than corrupt state.
	if s.auth == nil {
		s.turnMu.Lock()
		defer s.turnMu.Unlock()
	}

	if os.Getenv("LUMEN_DEMO") == "1" && req.APIKey == "" { // goal:d6aa846b round9
		// Demo echo only when no runtime API key — browser-local keys still hit the real provider.
		sink.emit("turn_started", "")
		sink.emit("text", "[Demo mode] You said: "+req.Prompt)
		if len(req.Images) > 0 {
			sink.emit("text", " (with "+strconv.Itoa(len(req.Images))+" image(s))")
		}
		sink.emit("turn_done", "")
		sink.done("", nil)
		return
	}

	configureErr := s.configureRuntime(rt, sink, "")
	if configureErr != nil {
		if rt.entry != nil {
			s.controllers.discard(owner, rt.session, ctrl)
		}
		sink.emit("turn_started", "")
		sink.emit("error", configureErr.Error())
		sink.emit("turn_done", "")
		sink.done("", configureErr)
		return
	}

	if req.Mode != "" {
		ctrl.SetPermissionMode(parseUIMode(req.Mode))
	}

	sessionID := ""
	if sess := ctrl.Session(); sess != nil && sess.Path != "" {
		sessionID = lumenstore.SessionIDFromPath(sess.Path)
	}
	run, err := s.runs.StartOwned(owner, sessionID, "code", summarizeRunTitle(req.Prompt), req.ParentID)
	if err != nil {
		sink.emit("error", err.Error())
		sink.done("", err)
		return
	}
	ctrl.SetApprover(s.webApprover(owner, run.ID, func(kind string, payload map[string]any) { sink.emitPayload(kind, payload) }))
	ctx, cleanupRun := s.beginActiveRun(r.Context(), owner, run.ID, 5*time.Minute)
	defer cleanupRun()
	pricing := usage.Pricing{}
	if active := ctrl.ProviderConfig(); active != nil && active.Pricing != nil {
		pricing = usage.Pricing{Input: active.Pricing.Input, Output: active.Pricing.Output, CacheHit: active.Pricing.CacheHit}
	}
	artifactCapture := &artifact.CapturingSink{Context: ctx, Store: s.artifactStore, Owner: owner, RunID: run.ID, Model: ctrl.ModelName(), Workspace: rt.ws, Next: sink, Failure: func(e error) {
		_, _ = s.runs.Finish(run.ID, fmt.Errorf("artifact persistence failed: %w", e))
		s.cancelActiveRun(owner, run.ID)
	}}
	capture := usage.CapturingSink{Store: s.usage, Owner: owner, Provider: ctrl.ProviderName(), Model: ctrl.ModelName(), Pricing: pricing, Next: artifactCapture, Failure: func(e error) {
		_, _ = s.runs.Finish(run.ID, fmt.Errorf("usage persistence failed: %w", e))
		s.cancelActiveRun(owner, run.ID)
	}}
	ctrl.SetSink(s.runs.WrapSink(run.ID, capture))

	var runErr error
	if ctrl.PermissionMode() == permission.ModePlan || req.Mode == "plan" {
		runErr = ctrl.Plan(ctx, req.Prompt)
		if runErr != nil {
			sink.emit("error", runErr.Error())
		}
	} else {
		runErr = ctrl.Run(ctx, req.Prompt)
		if runErr != nil {
			sink.emit("error", runErr.Error())
		}
	}
	if _, err := s.runs.Finish(run.ID, runErr); err != nil {
		sink.emit("error", "finish run: "+err.Error())
		if runErr == nil {
			runErr = err
		}
	}
	sink.done(run.ID, runErr)
}

type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s sseSink) Emit(e event.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		s.emit("error", "encode event: "+err.Error())
		return
	}
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

func (s sseSink) done(runID string, err error) {
	terminal := map[string]any{"kind": "stream_done", "ok": err == nil}
	if runID != "" {
		terminal["run_id"] = runID
	}
	if err != nil {
		terminal["error"] = err.Error()
	}
	data, _ := json.Marshal(terminal)
	fmt.Fprintf(s.w, "event: done\ndata: %s\n\n", data)
	s.flusher.Flush()
}

func summarizeRunTitle(prompt string) string {
	runes := []rune(strings.TrimSpace(prompt))
	if len(runes) > 120 {
		runes = runes[:120]
	}
	return string(runes)
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

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	ctrl := rt.ctrl
	jsonOK(w, map[string]any{
		"provider": ctrl.ProviderName(),
		"model":    ctrl.ModelName(),
		"mode":     string(ctrl.PermissionMode()),
		"ui_mode":  uiModeFromPermission(ctrl.PermissionMode()),
		"presets":  config.ModelPresets(),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	jsonOK(w, map[string]any{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
		"agent":  statusData(rt.ctrl),
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	histDir := filepath.Join(rt.ws.Root, ".lumen", "history")
	if s.auth == nil {
		histDir = filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "history")
	}
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
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	memDir := filepath.Join(rt.ws.Root, ".lumen", "memories")
	if s.auth == nil {
		memDir = s.memDir
	}
	store, err := memory.NewStore(memDir)
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

func jsonCodeErr(w http.ResponseWriter, stableCode, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "code": stableCode})
}

// ── Embedded web UI ─────────────────────────────────────────
