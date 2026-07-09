package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sync/atomic"

	"lumen/internal/event"
	"lumen/internal/permission"
	"lumen/internal/science/lab/compute"
	"lumen/internal/science/lab/jupyter"
	"lumen/internal/science/lab/project"
	"lumen/internal/science/lab/provenance"
	labruntime "lumen/internal/science/lab/runtime"
	"lumen/internal/science/lab/workspace"
	"lumen/internal/science/native/brief"
	"lumen/internal/science/paths"
	"lumen/internal/science/research"
	"lumen/internal/skill"
)

// API hosts lab REST + SSE handlers.
type API struct {
	sciDir     string
	version    string
	listenPort int
	projects   *project.Store
	fleet      *labruntime.FleetManager
	local      LocalConfig
	turns      *turnPool
	ctrls      *controllerPool
	approvals  *approvalHub
	// activeMode is read by approval hub during a turn.
	modeMu     sync.Mutex
	activeMode permission.Mode
	startedAt  time.Time
	// metrics
	turnsTotal   atomic.Uint64
	turnsFailed  atomic.Uint64
	approvalsTot atomic.Uint64
}

// NewAPI builds the lab API.
func NewAPI(sciDir, version string, fleet *labruntime.FleetManager, listenPort int) *API {
	if listenPort == 0 {
		listenPort = DefaultPort
	}
	store := project.NewStore(sciDir)
	api := &API{
		sciDir:      sciDir,
		version:     version,
		listenPort:  listenPort,
		projects:    store,
		fleet:       fleet,
		local:       loadLocalConfig(sciDir),
		turns:      newTurnPool(MaxConcurrentTurns),
		ctrls:      newControllerPool(sciDir, fleet, store, MaxControllers),
		activeMode: permission.ModeDefault,
		startedAt:  time.Now(),
	}
	api.approvals = newApprovalHub(func() permission.Mode {
		api.modeMu.Lock()
		defer api.modeMu.Unlock()
		return api.activeMode
	})
	return api
}

// Register mounts routes on mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/lab/health", a.handleHealth)
	mux.HandleFunc("/api/lab/readyz", a.handleReadyz)
	mux.HandleFunc("/api/lab/projects", a.handleProjects)
	mux.HandleFunc("/api/lab/projects/", a.handleProjectSub)
	mux.HandleFunc("/api/lab/skills", a.handleSkills)
	mux.HandleFunc("/api/lab/chat", a.handleChat)
	mux.HandleFunc("/api/lab/approve", a.handleApprove)
	mux.HandleFunc("/api/lab/brief", a.handleBrief)
	mux.HandleFunc("/api/lab/artifacts", a.handleArtifacts)
	mux.HandleFunc("/api/lab/files", a.handleFiles)
	mux.HandleFunc("/api/lab/files/", a.handleFiles)
	mux.HandleFunc("/api/lab/provenance", a.handleProvenance)
	mux.HandleFunc("/api/lab/compute/ssh-hosts", a.handleComputeSSHHosts)
	mux.HandleFunc("/api/lab/compute/jobs", a.handleComputeJobs)
	mux.HandleFunc("/api/lab/compute/jobs/", a.handleComputeJob)
	mux.HandleFunc("/api/lab/c2d/algorithms", a.handleC2DAlgorithms)
	mux.HandleFunc("/api/lab/bridge/open", a.handleBridgeOpen)
	mux.HandleFunc("/api/lab/notebooks", a.handleNotebooks)
	mux.HandleFunc("/api/lab/notebooks/", a.handleNotebooks)
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
	ctrlTotal, ctrlBusy := a.ctrls.stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"port":         a.listenPort,
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
		"capacity": map[string]any{
			"turns_active":    a.turns.active(),
			"turns_capacity":  a.turns.capacity(),
			"controllers":     ctrlTotal,
			"controllers_busy": ctrlBusy,
			"max_controllers": MaxControllers,
			"turns_total":     a.turnsTotal.Load(),
			"turns_failed":    a.turnsFailed.Load(),
			"approvals_total": a.approvalsTot.Load(),
		},
		"limits": map[string]any{
			"max_concurrent_turns": MaxConcurrentTurns,
			"approval_timeout_sec": int(ApprovalTimeout.Seconds()),
			"turn_timeout_sec":     int(DefaultTurnTimeout.Seconds()),
		},
	})
}

// handleReadyz is a stricter probe for orchestrators (Caddy/k8s).
func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Ready when server process is up; fleet may still be connecting (non-blocking).
	ready := a.turns.active() < a.turns.capacity()
	status := http.StatusOK
	body := map[string]any{
		"ready":        ready,
		"turns_active": a.turns.active(),
		"turns_cap":    a.turns.capacity(),
	}
	if !ready {
		status = http.StatusServiceUnavailable
		body["reason"] = "at capacity"
	}
	writeJSON(w, status, body)
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
		if r.Method == http.MethodDelete {
			if err := a.projects.Delete(slug); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
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
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	res, err := brief.Generate(r.Context(), a.sciDir, brief.Request{Topic: body.Topic})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	outPath, err := g.Resolve(filepath.Join("reports", "brief.md"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_ = os.MkdirAll(filepath.Dir(outPath), 0o700)
	_ = os.WriteFile(outPath, []byte(res.Markdown), 0o600)
	if projDir, err := a.projects.ProjectDir(slug); err == nil {
		rec, _ := provenance.NewRecorder(projDir, "", os.Getenv("LUMEN_SCIENCE_MODEL"))
		_ = rec.RecordArtifact(outPath)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": "reports/brief.md", "markdown": res.Markdown})
}

func (a *API) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var artifacts []map[string]any
	for _, sub := range []string{"reports", "figures", "data", "notebooks"} {
		dir, err := g.Resolve(sub)
		if err != nil {
			continue
		}
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(ws, path)
			if err != nil {
				return nil
			}
			artifacts = append(artifacts, map[string]any{
				"path":  rel,
				"size":  info.Size(),
				"mtime": info.ModTime().UTC().Format(time.RFC3339),
			})
			return nil
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts, "count": len(artifacts)})
}

func (a *API) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Long scientific prompts may exceed default middleware body cap.
	r.Body = http.MaxBytesReader(w, r.Body, ChatBodyMaxBytes)
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

	if !a.turns.tryAcquire() {
		w.Header().Set("Retry-After", "2")
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("实验室繁忙（并发上限 %d），请稍后重试", MaxConcurrentTurns))
		return
	}
	defer a.turns.release()

	ctrl, err := a.ctrls.acquire(slug)
	if err != nil {
		w.Header().Set("Retry-After", "1")
		writeErr(w, http.StatusConflict, err)
		return
	}
	defer a.ctrls.release(slug)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sink := sseSink{w: w, flusher: flusher}
	mode := req.Mode
	if mode == "" {
		mode = a.local.DefaultMode
	}
	a.setActiveMode(mode)

	// Configure with timeout to prevent indefinite hang
	cfgCtx, cfgCancel := context.WithTimeout(r.Context(), ConfigureTimeout)
	defer cfgCancel()

	done := make(chan error, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				done <- fmt.Errorf("configure panic: %v", rec)
			}
		}()
		done <- ctrl.Configure(slug, req.SessionID, sink, a.makeApprover(sink.emitPayload))
	}()
	select {
	case err := <-done:
		if err != nil {
			a.turnsFailed.Add(1)
			sink.emit("error", err.Error())
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		}
	case <-cfgCtx.Done():
		a.turnsFailed.Add(1)
		sink.emit("error", "配置超时，请刷新页面重试")
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), DefaultTurnTimeout)
	defer cancel()

	a.turnsTotal.Add(1)
	sink.emit("turn_started", "")
	runErr := func() (err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("turn panic: %v", rec)
			}
		}()
		return ctrl.Run(ctx, req.Prompt, mode)
	}()
	if runErr != nil {
		a.turnsFailed.Add(1)
		sink.emit("error", runErr.Error())
	}
	sink.emit("turn_done", "")
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func (a *API) setActiveMode(mode string) {
	var m permission.Mode
	switch mode {
	case "plan", "":
		m = permission.ModePlan
	case "bypass":
		m = permission.ModeBypass
	default:
		m = permission.ModeDefault
	}
	a.modeMu.Lock()
	a.activeMode = m
	a.modeMu.Unlock()
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

// handleFiles serves workspace file tree, content, and downloads.
func (a *API) handleFiles(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Route sub-paths: /api/lab/files/content?path=, /api/lab/files/download?path=, /api/lab/files/upload
	sub := strings.TrimPrefix(r.URL.Path, "/api/lab/files")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case sub == "content" || sub == "":
		a.handleFileContent(w, r, g)
	case sub == "download":
		a.handleFileDownload(w, r, g)
	case sub == "upload":
		a.handleFileUpload(w, r, g)
	default:
		http.NotFound(w, r)
	}
}

func (a *API) handleFileContent(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	rel := r.URL.Query().Get("path")
	if rel == "" || rel == "." {
		a.writeDirListing(w, g, ".")
		return
	}

	abs, err := g.Resolve(rel)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	st, err := os.Stat(abs)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if st.IsDir() {
		a.writeDirListing(w, g, rel)
		return
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	maxSize := 512 * 1024
	trunc := len(data) > maxSize
	if trunc {
		data = data[:maxSize]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":        rel,
		"content":     string(data),
		"size":        st.Size(),
		"truncated":   trunc,
		"previewKind": previewKind(rel),
		"isDir":       false,
	})
}

func (a *API) writeDirListing(w http.ResponseWriter, g *workspace.Guard, rel string) {
	root, err := g.Resolve(rel)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var files []map[string]any
	for _, e := range entries {
		info, _ := e.Info()
		name := e.Name()
		path := name
		if rel != "" && rel != "." {
			path = filepath.Join(rel, name)
		}
		entry := map[string]any{
			"name":        name,
			"path":        path,
			"isDir":       e.IsDir(),
			"previewKind": previewKind(name),
		}
		if info != nil && !e.IsDir() {
			entry["size"] = info.Size()
			entry["mtime"] = info.ModTime().UTC().Format(time.RFC3339)
		}
		files = append(files, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "path": rel, "root": root})
}

func previewKind(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown":
		return "markdown"
	case ".json", ".jsonl", ".yml", ".yaml", ".toml", ".csv", ".tsv", ".txt", ".log", ".py", ".r", ".go", ".js", ".ts", ".html", ".css":
		return "text"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return "image"
	case ".pdf":
		return "pdf"
	case ".pdb", ".cif", ".sdf", ".mol":
		return "molecule"
	default:
		if ext == "" {
			return "text"
		}
		return "binary"
	}
}

func (a *API) handleFileDownload(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	rel := r.URL.Query().Get("path")
	abs, err := g.Resolve(rel)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(abs)))
	_, _ = w.Write(data)
}

// handleFileUpload accepts multipart file uploads into the project workspace.
func (a *API) handleFileUpload(w http.ResponseWriter, r *http.Request, g *workspace.Guard) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Limit upload to 64 MB
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("文件过大或格式错误: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("未选择文件: %w", err))
		return
	}
	defer file.Close()

	// Resolve safe path inside workspace
	abs, err := g.Resolve(header.Filename)
	if err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	dst, err := os.Create(abs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer dst.Close()
	written, err := io.Copy(dst, file)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"uploaded": header.Filename,
		"size":     written,
	})
}

// handleProvenance returns provenance.jsonl records for a project.
func (a *API) handleProvenance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	projectDir, err := a.projects.ProjectDir(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	provPath := filepath.Join(projectDir, "provenance.jsonl")
	data, err := os.ReadFile(provPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"records": []any{}, "count": 0})
		return
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var records []any
	artifactFilter := r.URL.Query().Get("path")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if artifactFilter != "" {
			if p, ok := rec["path"].(string); !ok || p != artifactFilter {
				continue
			}
		}
		records = append(records, rec)
	}
	// Reverse for newest-first
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records, "count": len(records)})
}

// ── Compute endpoints ──

func (a *API) handleComputeSSHHosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hosts, err := compute.ParseSSHConfig()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"hosts": []any{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": hosts, "count": len(hosts)})
}

func (a *API) handleComputeJobs(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	projectDir, err := a.projects.ProjectDir(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	store, err := compute.NewStore(projectDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		jobs, err := store.List()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"jobs": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "count": len(jobs)})
	case http.MethodPost:
		var body struct {
			Host       string `json:"host"`
			Command    string `json:"command"`
			TimeoutSec int    `json:"timeout_sec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Host == "" || body.Command == "" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("host and command required"))
			return
		}
		ws, _ := a.projects.WorkspacePath(slug)
		opts := compute.SubmitOpts{}
		if body.TimeoutSec > 0 {
			opts.Timeout = time.Duration(body.TimeoutSec) * time.Second
		}
		j, err := store.SubmitOpts(body.Host, body.Command, ws, opts)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, j)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleComputeJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/lab/compute/jobs/")
	id = strings.Trim(id, "/")
	slug := r.URL.Query().Get("project_id")
	if slug == "" || id == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id and job id required"))
		return
	}
	projectDir, err := a.projects.ProjectDir(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	store, err := compute.NewStore(projectDir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	j, err := store.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// ── C2D + Bridge endpoints ──

func (a *API) handleC2DAlgorithms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		DatasetID string `json:"dataset_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	_ = a.fleet.ConnectAll()
	text, err := a.fleet.CallNative("c2d", "list_algorithms", map[string]any{
		"dataset_id": strings.TrimSpace(body.DatasetID),
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(text), &payload)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": payload})
}

func (a *API) handleBridgeOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"bridge_url":  "http://127.0.0.1:18990",
		"sandbox_url": "http://127.0.0.1:8990",
		"hint":        "在 Bridge 面板中点击「一键开始」启动沙箱，或运行 lumen science start",
	})
}

// ── Notebook / Jupyter endpoints ──

func (a *API) handleNotebooks(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("project_id")
	if slug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("project_id required"))
		return
	}
	ws, err := a.projects.WorkspacePath(slug)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	g, err := workspace.NewGuard(ws)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	sub := strings.TrimPrefix(r.URL.Path, "/api/lab/notebooks")
	sub = strings.TrimPrefix(sub, "/")

	switch r.Method {
	case http.MethodGet:
		if sub == "" || sub == "list" {
			notebooks, err := jupyter.ListNotebooks(ws)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"notebooks":       notebooks,
				"count":           len(notebooks),
				"jupyter_available": jupyter.IsAvailable(),
			})
			return
		}
		// Get cell content
		name := strings.TrimPrefix(sub, "cells/")
		path := filepath.Join(ws, "notebooks", name)
		nb, err := jupyter.Load(path)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name":  name,
			"cells": nb.Cells,
			"count": len(nb.Cells),
			"markdown": nb.ToMarkdown(),
		})
	case http.MethodPost:
		var body struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if sub == "" || sub == "create" {
			// Create new notebook
			if body.Name == "" {
				body.Name = fmt.Sprintf("notebook_%s.ipynb", time.Now().Format("20060102-150405"))
			}
			if !strings.HasSuffix(body.Name, ".ipynb") {
				body.Name += ".ipynb"
			}
			nb := jupyter.New(strings.TrimSuffix(body.Name, ".ipynb"))
			path, err := g.Resolve(filepath.Join("notebooks", body.Name))
			if err != nil {
				writeErr(w, http.StatusForbidden, err)
				return
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			if err := nb.Save(path); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"name": body.Name, "path": path})
			return
		}
		if strings.HasPrefix(sub, "execute/") {
			// Execute notebook
			name := strings.TrimPrefix(sub, "execute/")
			path := filepath.Join(ws, "notebooks", name)
			nb, err := jupyter.Load(path)
			if err != nil {
				writeErr(w, http.StatusNotFound, err)
				return
			}
			python := labruntime.ResolvePython(paths.DataDir(a.sciDir))
			if err := nb.Execute(path, python); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "cells": nb.Cells})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cells": nb.Cells})
			return
		}
		// Append cell
		if strings.HasPrefix(sub, "cell/") {
			name := strings.TrimPrefix(sub, "cell/")
			path := filepath.Join(ws, "notebooks", name)
			nb, err := jupyter.Load(path)
			if err != nil {
				writeErr(w, http.StatusNotFound, err)
				return
			}
			if body.Source != "" {
				nb.AddCode(body.Source)
			}
			if err := nb.Save(path); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cells": len(nb.Cells)})
			return
		}
		http.NotFound(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
