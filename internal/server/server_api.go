// REST + slash command handlers (goal:d6aa846b round9 — command passes api_key).
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/doctor"
	"lumen/internal/permission"
	"lumen/internal/provider"
	"lumen/internal/runstate"
	"lumen/internal/skill"
	"lumen/internal/timeline"
	"lumen/internal/workspace"
)

func (s *Server) routesAPI() {
	s.handleBusiness("/v1/mode", s.handleMode)
	s.handleBusiness("/v1/command", s.handleCommand)
	s.handleBusiness("/v1/skills", s.handleSkills)
	s.handleBusiness("/v1/doctor", s.handleDoctor)
	s.handleBusiness("/v1/timeline", s.handleTimeline)
	s.handleBusiness("/v1/rewind", s.handleRewind)
	s.handleBusiness("/v1/sessions/content", s.handleSessionContent)
	s.handleBusiness("/v1/sessions/resume", s.handleSessionResume)
	s.handleBusiness("/v1/runs/", s.handleRuns)
	// File API
	s.handleBusiness("/api/files", s.handleFilesList)
	s.handleBusiness("/api/files/", s.handleFilesList)
	s.routesApproval()
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	owner := ownerFromRequest(r)
	rel := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
	if rel == "" || strings.Contains(rel, "..") {
		jsonErr(w, "invalid run path", http.StatusBadRequest)
		return
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 1 && parts[0] != "" {
		if r.Method != http.MethodGet {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := s.runs.GetOwned(owner, parts[0])
		if errors.Is(err, runstate.ErrRunNotFound) {
			jsonErr(w, "run not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]any{"run": run})
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "events" {
		if r.Method != http.MethodGet {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var after uint64
		if raw := r.URL.Query().Get("after"); raw != "" {
			value, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				jsonErr(w, "after must be a non-negative integer", http.StatusBadRequest)
				return
			}
			after = value
		}
		events, err := s.runs.EventsOwned(owner, parts[0], after)
		if errors.Is(err, runstate.ErrRunNotFound) {
			jsonErr(w, "run not found", http.StatusNotFound)
			return
		}
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]any{"events": events, "run_id": parts[0], "after": after})
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "cancel" {
		if r.Method != http.MethodPost {
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, err := s.runs.GetOwned(owner, parts[0]); errors.Is(err, runstate.ErrRunNotFound) {
			jsonErr(w, "run not found", http.StatusNotFound)
			return
		} else if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !s.cancelActiveRun(owner, parts[0]) {
			jsonErr(w, "run is not active", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "run_id": parts[0]})
		return
	}
	jsonErr(w, "invalid run path", http.StatusBadRequest)
}

func (s *Server) handleMode(w http.ResponseWriter, r *http.Request) {
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	ctrl := rt.ctrl
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, map[string]any{
			"mode": string(ctrl.PermissionMode()),
			"ui":   uiModeFromPermission(ctrl.PermissionMode()),
		})
	case http.MethodPut, http.MethodPost:
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Mode == "" {
			jsonErr(w, "mode required", http.StatusBadRequest)
			return
		}
		m := parseUIMode(req.Mode)
		ctrl.SetPermissionMode(m)
		jsonOK(w, map[string]any{
			"mode": string(m),
			"ui":   uiModeFromPermission(m),
		})
	default:
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Command  string `json:"command"`
		APIKey   string `json:"api_key,omitempty"`
		Provider string `json:"provider,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Command) == "" {
		jsonErr(w, "command required", http.StatusBadRequest)
		return
	}
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	text, data, err := s.execCommand(rt, strings.TrimSpace(req.Command), req.APIKey, req.Provider)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, map[string]any{"text": text, "data": data})
}

func (s *Server) execCommand(rt *requestRuntime, cmd, apiKey, provider string) (string, any, error) {
	ctrl := rt.ctrl
	lower := strings.ToLower(cmd)

	switch {
	case lower == "/help":
		return helpText(), nil, nil
	case lower == "/status":
		return formatStatus(ctrl), statusData(ctrl), nil
	case lower == "/cost":
		return formatCost(ctrl), costData(ctrl), nil
	case lower == "/cache":
		return formatCache(ctrl), cacheData(ctrl), nil
	case lower == "/models":
		return formatModels(), map[string]any{"presets": config.ModelPresets()}, nil
	case strings.HasPrefix(lower, "/model "):
		name := strings.TrimSpace(cmd[len("/model "):])
		n, err := ctrl.SwitchModel(name)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("model = %s", n), map[string]string{"model": n}, nil
	case lower == "/mode":
		return "modes: bypass · plan · default · accept-edits", map[string]string{
			"modes": "bypass,plan,default,accept-edits",
		}, nil
	case strings.HasPrefix(lower, "/mode "):
		m := parseUIMode(strings.TrimSpace(cmd[len("/mode "):]))
		ctrl.SetPermissionMode(m)
		return fmt.Sprintf("mode = %s", m), map[string]string{"mode": string(m)}, nil
	case lower == "/undo" || lower == "/rewind":
		rewound, err := ctrl.Rewind()
		if err != nil {
			return "", nil, err
		}
		if len(rewound) == 0 {
			return "nothing to undo", map[string]any{"rewound": []string{}}, nil
		}
		return fmt.Sprintf("rewound %d file(s): %s", len(rewound), strings.Join(rewound, ", ")),
			map[string]any{"rewound": rewound}, nil
	case lower == "/replay":
		entries, err := timeline.LoadTimeline(timelinePath(ctrl, rt.ws))
		if err != nil || len(entries) == 0 {
			return "no timeline yet", nil, nil
		}
		return timeline.FormatTimeline(entries), map[string]any{"entries": entries}, nil
	case lower == "/changes":
		changes, err := timeline.LoadChanges(timelinePath(ctrl, rt.ws))
		if err != nil || len(changes) == 0 {
			return "no changes yet", nil, nil
		}
		return timeline.FormatChanges(changes), map[string]any{"changes": changes}, nil
	case lower == "/skills":
		return formatSkills(ctrl), skillsData(ctrl), nil
	case lower == "/execute", lower == "/reject",
		strings.HasPrefix(lower, "/workflow "),
		strings.HasPrefix(lower, "/ultra "),
		strings.HasPrefix(lower, "/goal "):
		return s.execWorkflowCommandRuntime(rt, cmd, apiKey, provider)
	default:
		if strings.HasPrefix(cmd, "/") && !strings.Contains(cmd, " ") {
			name := strings.TrimPrefix(cmd, "/")
			if sk := findSkill(ctrl, name); sk != nil {
				return fmt.Sprintf("skill: %s — send a message to invoke via run_skill", sk.Name),
					map[string]any{"skill": sk}, nil
			}
		}
		return "", nil, fmt.Errorf("unknown command — try /help")
	}
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	jsonOK(w, skillsData(rt.ctrl))
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	cfg, err := config.LoadWithEnv(config.FindConfig(), config.FindDotEnv())
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	report := doctor.Run(cfg)
	jsonOK(w, report)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	kind := r.URL.Query().Get("kind")
	path := timelinePath(rt.ctrl, rt.ws)
	switch kind {
	case "changes":
		changes, err := timeline.LoadChanges(path)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]any{"changes": changes})
	default:
		entries, err := timeline.LoadTimeline(path)
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]any{"entries": entries})
	}
}

func (s *Server) handleRewind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	rewound, err := rt.ctrl.Rewind()
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, map[string]any{"rewound": rewound})
}

func (s *Server) handleSessionResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		jsonErr(w, "name required", http.StatusBadRequest)
		return
	}
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	histDir := filepath.Join(rt.ws.Root, ".lumen", "history")
	if s.auth == nil {
		histDir = filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "history")
	}
	if err := rt.ctrl.LoadSessionFromDir(histDir, strings.TrimSpace(req.Name)); err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, map[string]any{
		"resumed":  req.Name,
		"messages": rt.ctrl.Session().Len(),
	})
}

func (s *Server) handleSessionContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		jsonErr(w, "name required", http.StatusBadRequest)
		return
	}
	if !strings.HasSuffix(name, ".jsonl") {
		name += ".jsonl"
	}
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	histDir := filepath.Join(rt.ws.Root, ".lumen", "history")
	if s.auth == nil {
		histDir = filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "history")
	}
	path := filepath.Join(histDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		jsonErr(w, "session not found", http.StatusNotFound)
		return
	}
	var messages []map[string]any
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m provider.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m.Role != provider.RoleUser && m.Role != provider.RoleAssistant {
			continue
		}
		content := m.Content
		if len(content) > 500 {
			content = content[:497] + "..."
		}
		messages = append(messages, map[string]any{
			"role":    m.Role,
			"content": content,
		})
	}
	if len(messages) > 40 {
		messages = messages[len(messages)-40:]
	}
	jsonOK(w, map[string]any{"name": name, "messages": messages})
}

// ── helpers ─────────────────────────────────────────────────

func timelinePath(ctrl *control.Controller, ws workspace.Context) string {
	p := ctrl.TimelinePath()
	if p == "" {
		return filepath.Join(ws.Root, ".lumen", "timeline.jsonl")
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(ws.Root, p)
}

func parseUIMode(s string) permission.Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "agent", "bypass":
		return permission.ModeBypass
	default:
		return permission.ParseMode(s)
	}
}

func uiModeFromPermission(m permission.Mode) string {
	switch m {
	case permission.ModePlan:
		return "plan"
	case permission.ModeDefault:
		return "default"
	case permission.ModeAcceptEdits:
		return "accept-edits"
	case permission.ModeBypass:
		return "bypass"
	default:
		return "bypass"
	}
}

func helpText() string {
	return strings.TrimSpace(`
Commands:
  /help      show this help
  /status    provider, model, tokens
  /cost      token usage and cost
  /cache     cache hit rate
  /models    list model presets
  /model X   switch model preset
  /mode X    bypass · plan · default · accept-edits
  /undo      rewind last file edits
  /rewind    alias for /undo
  /replay    session timeline
  /changes   changed files inbox
  /skills    list available skills
  /doctor    health check (API)
  /workflow  plan → review → execute
  /execute   run approved plan
  /reject    discard pending plan
  /ultra     plan → auto-execute
  /goal      autonomous goal execution
`)
}

func formatStatus(ctrl *control.Controller) string {
	ag := ctrl.Agent()
	var ti, to int
	pct := 0
	if ag != nil {
		last := ag.LastUsage()
		if last != nil {
			ti = last.PromptTokens
			to = last.CompletionTokens
		}
		hit, miss := ag.SessionCache()
		if hit+miss > 0 {
			pct = int(float64(hit) / float64(hit+miss) * 100)
		}
	}
	return fmt.Sprintf("%s/%s · mode %s · %.1fk tokens · cache %d%%",
		ctrl.ProviderName(), ctrl.ModelName(), ctrl.PermissionMode(),
		float64(ti+to)/1000, pct)
}

func statusData(ctrl *control.Controller) map[string]any {
	ag := ctrl.Agent()
	out := map[string]any{
		"provider": ctrl.ProviderName(),
		"model":    ctrl.ModelName(),
		"mode":     string(ctrl.PermissionMode()),
	}
	if ag != nil {
		last := ag.LastUsage()
		if last != nil {
			out["last_usage"] = last
		}
		hit, miss := ag.SessionCache()
		out["cache_hit"] = hit
		out["cache_miss"] = miss
	}
	sess := ctrl.Session()
	if sess != nil {
		out["messages"] = sess.Len()
	}
	return out
}

func formatCost(ctrl *control.Controller) string {
	d := costData(ctrl)
	return fmt.Sprintf("session tokens: %.1fk · est. cost $%.4f",
		d["total_tokens_k"], d["cost_usd"])
}

func costData(ctrl *control.Controller) map[string]any {
	ag := ctrl.Agent()
	var ti, to int64
	if ag != nil {
		hit, miss := ag.SessionCache()
		ti = hit + miss
		last := ag.LastUsage()
		if last != nil {
			to = int64(last.CompletionTokens)
		}
	}
	cost := estimateCost(ctrl)
	return map[string]any{
		"input_tokens":   ti,
		"output_tokens":  to,
		"total_tokens_k": float64(ti+to) / 1000,
		"cost_usd":       cost,
	}
}

func estimateCost(ctrl *control.Controller) float64 {
	ag := ctrl.Agent()
	if ag == nil {
		return 0
	}
	last := ag.LastUsage()
	if last == nil {
		return 0
	}
	pr := ctrl.Pricing()
	if pr == nil {
		return 0
	}
	return pr.Cost(last)
}

func formatCache(ctrl *control.Controller) string {
	d := cacheData(ctrl)
	return fmt.Sprintf("cache hits %v · %v%% efficiency", d["cache_hit"], d["cache_pct"])
}

func cacheData(ctrl *control.Controller) map[string]any {
	ag := ctrl.Agent()
	var hit, miss int64
	if ag != nil {
		hit, miss = ag.SessionCache()
	}
	ti := hit + miss
	pct := 0
	if ti > 0 {
		pct = int(float64(hit) / float64(ti) * 100)
	}
	return map[string]any{"cache_hit": hit, "cache_miss": miss, "cache_pct": pct}
}

func formatModels() string {
	var sb strings.Builder
	sb.WriteString("Model presets:\n")
	for _, p := range config.ModelPresets() {
		fmt.Fprintf(&sb, "  %s (%s)\n", p.Name, p.Model)
	}
	sb.WriteString("Use /model <name> to switch")
	return sb.String()
}

func skillsData(ctrl *control.Controller) map[string]any {
	store := ctrl.Skills()
	if store == nil {
		return map[string]any{"skills": []any{}}
	}
	list := store.List()
	out := make([]map[string]string, 0, len(list))
	for _, sk := range list {
		out = append(out, map[string]string{
			"name":        sk.Name,
			"description": sk.Description,
		})
	}
	return map[string]any{"skills": out}
}

func formatSkills(ctrl *control.Controller) string {
	d := skillsData(ctrl)
	skills, _ := d["skills"].([]map[string]string)
	if len(skills) == 0 {
		return "no skills loaded"
	}
	var sb strings.Builder
	for _, sk := range skills {
		fmt.Fprintf(&sb, "  %s — %s\n", sk["name"], sk["description"])
	}
	return strings.TrimSpace(sb.String())
}

func findSkill(ctrl *control.Controller, name string) *skill.Skill {
	store := ctrl.Skills()
	if store == nil {
		return nil
	}
	for _, sk := range store.List() {
		if strings.EqualFold(sk.Name, name) {
			cp := sk
			return &cp
		}
	}
	return nil
}

// ── File API ──

func (s *Server) workspaceRoot() string {
	if d := os.Getenv("LUMEN_WORKSPACE_ROOT"); d != "" {
		return d
	}
	wd, _ := os.Getwd()
	return wd
}

func (s *Server) resolveSafe(rel string) (string, error) {
	root, err := filepath.Abs(s.workspaceRoot())
	if err != nil {
		return "", err
	}
	target := filepath.Clean(filepath.Join(root, rel))
	if !strings.HasPrefix(target, root) {
		return "", fmt.Errorf("路径越界")
	}
	return target, nil
}

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)
	sub := strings.TrimPrefix(r.URL.Path, "/api/files")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case sub == "upload":
		s.handleFileUpload(rt, w, r)
	case sub == "content" || strings.HasPrefix(sub, "content"):
		s.handleFileContent(rt, w, r)
	case sub == "write":
		s.handleFileWrite(rt, w, r)
	default:
		s.handleFileList(rt, w, r)
	}
}

func (s *Server) handleFileList(rt *requestRuntime, w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" {
		rel = "."
	}
	abs, err := s.resolveRuntimePath(rt, rel, false)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusForbidden)
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusNotFound)
		return
	}
	var files []map[string]any
	for _, e := range entries {
		info, _ := e.Info()
		entry := map[string]any{
			"name":  e.Name(),
			"isDir": e.IsDir(),
		}
		if info != nil && !e.IsDir() {
			entry["size"] = info.Size()
			entry["mtime"] = info.ModTime().Format("2006-01-02T15:04:05Z")
		}
		files = append(files, entry)
	}
	jsonOK(w, map[string]any{"files": files, "root": "."})
}

func (s *Server) handleFileContent(rt *requestRuntime, w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" {
		jsonErr(w, "path required", http.StatusBadRequest)
		return
	}
	abs, err := s.resolveRuntimePath(rt, rel, false)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusForbidden)
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusNotFound)
		return
	}
	maxSize := 512 * 1024
	if len(data) > maxSize {
		data = data[:maxSize]
	}
	jsonOK(w, map[string]any{
		"path":      rel,
		"content":   string(data),
		"size":      len(data),
		"truncated": len(data) >= maxSize,
	})
}

func (s *Server) handleFileUpload(rt *requestRuntime, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		jsonErr(w, "文件过大或格式错误", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, "未选择文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	abs, err := s.resolveRuntimePath(rt, header.Filename, true)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dst, err := os.Create(abs)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()
	written, err := io.Copy(dst, file)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"uploaded": header.Filename, "size": written})
}

func (s *Server) handleFileWrite(rt *requestRuntime, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		jsonErr(w, "path and content required", http.StatusBadRequest)
		return
	}
	abs, err := s.resolveRuntimePath(rt, req.Path, true)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(abs, []byte(req.Content), 0644); err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"ok": true, "path": req.Path})
}
