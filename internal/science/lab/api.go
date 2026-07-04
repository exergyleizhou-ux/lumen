package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/event"
	"lumen/internal/science/lab/project"
	labruntime "lumen/internal/science/lab/runtime"
	"lumen/internal/science/native/brief"
	"lumen/internal/science/paths"
	"lumen/internal/science/research"
	"lumen/internal/skill"
)

// API hosts lab REST + SSE handlers.
type API struct {
	sciDir    string
	version   string
	projects  *project.Store
	fleet     *labruntime.FleetManager
	local     LocalConfig
	turnMu    sync.Mutex
	labCtrl   *Controller
	startedAt time.Time
}

// NewAPI builds the lab API.
func NewAPI(sciDir, version string, fleet *labruntime.FleetManager) *API {
	return &API{
		sciDir:    sciDir,
		version:   version,
		projects:  project.NewStore(sciDir),
		fleet:     fleet,
		local:     loadLocalConfig(sciDir),
		labCtrl:   NewController(sciDir, fleet, project.NewStore(sciDir)),
		startedAt: time.Now(),
	}
}

// Register mounts routes on mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/lab/health", a.handleHealth)
	mux.HandleFunc("/api/lab/projects", a.handleProjects)
	mux.HandleFunc("/api/lab/projects/", a.handleProjectSub)
	mux.HandleFunc("/api/lab/skills", a.handleSkills)
	mux.HandleFunc("/api/lab/chat", a.handleChat)
	mux.HandleFunc("/api/lab/brief", a.handleBrief)
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sciCfg, _ := scienceConfig(a.sciDir)
	masked, adapter := providerStatus(sciCfg)
	rep := research.Report{}
	if rrep, err := research.Scan(paths.DataDir(a.sciDir)); err == nil {
		rep = rrep
	}
	fleetSt := map[string]any{}
	if a.fleet != nil {
		fleetSt = a.fleet.Status()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"port":         DefaultPort,
		"panel":        "lumen://science-lab",
		"version":      a.version,
		"science_mode": sciCfg.ScienceMode,
		"uptime_sec":   int(time.Since(a.startedAt).Seconds()),
		"research_pack": map[string]any{
			"healthy":      rep.Healthy(),
			"bio_clients":  rep.BioLibPackages,
			"domain_tools": rep.TotalDomainTools,
			"skills":       len(rep.Skills),
			"domains":      len(rep.Domains),
		},
		"fleet": fleetSt,
		"provider": map[string]any{
			"set":     masked != "" && masked != "—",
			"masked":  masked,
			"adapter": adapter,
		},
	})
}

func (a *API) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := a.projects.List()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var body struct {
			Title    string `json:"title"`
			Template string `json:"template"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Title) == "" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("title required"))
			return
		}
		p, err := a.projects.Create(body.Title, body.Template)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleProjectSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/lab/projects/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	slug := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p, err := a.projects.Get(slug)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		sessions, _ := a.projects.ListSessions(slug)
		writeJSON(w, http.StatusOK, map[string]any{"project": p, "sessions": sessions})
		return
	}
	if parts[1] == "sessions" && r.Method == http.MethodPost {
		var body struct {
			Title string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sess, err := a.projects.CreateSession(slug, body.Title)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, sess)
		return
	}
	http.NotFound(w, r)
}

func (a *API) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := r.URL.Query().Get("project_id")
	ws := ""
	if slug != "" {
		if p, err := a.projects.Get(slug); err == nil {
			ws, _ = a.projects.WorkspacePath(p.Slug)
		}
	}
	home, _ := os.UserHomeDir()
	skillPaths := []string{filepath.Join(a.sciDir, "skills")}
	if packSkills := labruntime.SkillsDir(a.sciDir); packSkills != "" {
		skillPaths = append(skillPaths, packSkills)
	}
	store := skill.New(skill.Options{
		HomeDir:     home,
		ProjectRoot: ws,
		CustomPaths: skillPaths,
	})
	list := store.List()
	out := make([]map[string]string, 0, len(list))
	for _, sk := range list {
		out = append(out, map[string]string{
			"name":        sk.Name,
			"description": sk.Description,
			"scope":       string(sk.Scope),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out, "count": len(out)})
}

func (a *API) handleBrief(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ProjectID string `json:"project_id"`
		Topic     string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Topic == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("topic required"))
		return
	}
	slug := body.ProjectID
	if p, err := a.projects.Get(slug); err == nil {
		slug = p.Slug
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := brief.Generate(r.Context(), a.sciDir, brief.Request{Topic: body.Topic})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	outPath := filepath.Join(ws, "reports", "brief.md")
	_ = os.MkdirAll(filepath.Dir(outPath), 0o700)
	_ = os.WriteFile(outPath, []byte(res.Markdown), 0o600)
	writeJSON(w, http.StatusOK, map[string]any{"path": "reports/brief.md", "markdown": res.Markdown})
}

func (a *API) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ProjectID string `json:"project_id"`
		SessionID string `json:"session_id"`
		Prompt    string `json:"prompt"`
		Mode      string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("prompt required"))
		return
	}
	slug := req.ProjectID
	if p, err := a.projects.Get(slug); err == nil {
		slug = p.Slug
	} else if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	a.turnMu.Lock()
	defer a.turnMu.Unlock()

	sink := sseSink{w: w, flusher: flusher}
	if err := a.labCtrl.Configure(slug, req.SessionID, sink, webApprover(sink.emitPayload)); err != nil {
		sink.emit("error", err.Error())
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	sink.emit("turn_started", "")
	mode := req.Mode
	if mode == "" {
		mode = a.local.DefaultMode
	}
	if err := a.labCtrl.Run(ctx, req.Prompt, mode); err != nil {
		sink.emit("error", err.Error())
	}
	sink.emit("turn_done", "")
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s sseSink) Emit(e event.Event) {
	data, _ := json.Marshal(map[string]any{
		"kind": e.Kind, "text": e.Text, "tool": e.Tool,
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
