package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/doctor"
	"lumen/internal/permission"
	"lumen/internal/provider"
	"lumen/internal/skill"
	"lumen/internal/timeline"
)

func (s *Server) routesAPI() {
	s.mux.HandleFunc("/v1/mode", s.handleMode)
	s.mux.HandleFunc("/v1/command", s.handleCommand)
	s.mux.HandleFunc("/v1/skills", s.handleSkills)
	s.mux.HandleFunc("/v1/doctor", s.handleDoctor)
	s.mux.HandleFunc("/v1/timeline", s.handleTimeline)
	s.mux.HandleFunc("/v1/rewind", s.handleRewind)
	s.mux.HandleFunc("/v1/sessions/content", s.handleSessionContent)
}

func (s *Server) handleMode(w http.ResponseWriter, r *http.Request) {
	ctrl := s.cfg.Ctrl
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
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Command) == "" {
		jsonErr(w, "command required", http.StatusBadRequest)
		return
	}
	text, data, err := s.execCommand(strings.TrimSpace(req.Command))
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, map[string]any{"text": text, "data": data})
}

func (s *Server) execCommand(cmd string) (string, any, error) {
	ctrl := s.cfg.Ctrl
	lower := strings.ToLower(cmd)

	switch {
	case lower == "/help":
		return helpText(), nil, nil
	case lower == "/status":
		return s.formatStatus(), s.statusData(), nil
	case lower == "/cost":
		return s.formatCost(), s.costData(), nil
	case lower == "/cache":
		return s.formatCache(), s.cacheData(), nil
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
		entries, err := timeline.LoadTimeline(s.timelinePath())
		if err != nil || len(entries) == 0 {
			return "no timeline yet", nil, nil
		}
		return timeline.FormatTimeline(entries), map[string]any{"entries": entries}, nil
	case lower == "/changes":
		changes, err := timeline.LoadChanges(s.timelinePath())
		if err != nil || len(changes) == 0 {
			return "no changes yet", nil, nil
		}
		return timeline.FormatChanges(changes), map[string]any{"changes": changes}, nil
	case lower == "/skills":
		return s.formatSkills(), s.skillsData(), nil
	default:
		if strings.HasPrefix(cmd, "/") && !strings.Contains(cmd, " ") {
			name := strings.TrimPrefix(cmd, "/")
			if sk := s.findSkill(name); sk != nil {
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
	jsonOK(w, s.skillsData())
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
	kind := r.URL.Query().Get("kind")
	path := s.timelinePath()
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
	rewound, err := s.cfg.Ctrl.Rewind()
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, map[string]any{"rewound": rewound})
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
	path := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "history", name)
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

func (s *Server) timelinePath() string {
	p := s.cfg.Ctrl.TimelinePath()
	if p == "" {
		return filepath.Join(".lumen", "timeline.jsonl")
	}
	return p
}

func parseUIMode(s string) permission.Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "agent":
		return permission.ModeBypass
	default:
		return permission.ParseMode(s)
	}
}

func uiModeFromPermission(m permission.Mode) string {
	if m == permission.ModePlan {
		return "plan"
	}
	return "agent"
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
`)
}

func (s *Server) formatStatus() string {
	ctrl := s.cfg.Ctrl
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

func (s *Server) statusData() map[string]any {
	ctrl := s.cfg.Ctrl
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

func (s *Server) formatCost() string {
	d := s.costData()
	return fmt.Sprintf("session tokens: %.1fk · est. cost $%.4f",
		d["total_tokens_k"], d["cost_usd"])
}

func (s *Server) costData() map[string]any {
	ag := s.cfg.Ctrl.Agent()
	var ti, to int64
	if ag != nil {
		hit, miss := ag.SessionCache()
		ti = hit + miss
		last := ag.LastUsage()
		if last != nil {
			to = int64(last.CompletionTokens)
		}
	}
	cost := estimateCost(s.cfg.Ctrl)
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

func (s *Server) formatCache() string {
	d := s.cacheData()
	return fmt.Sprintf("cache hits %v · %v%% efficiency", d["cache_hit"], d["cache_pct"])
}

func (s *Server) cacheData() map[string]any {
	ag := s.cfg.Ctrl.Agent()
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

func (s *Server) skillsData() map[string]any {
	store := s.cfg.Ctrl.Skills()
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

func (s *Server) formatSkills() string {
	d := s.skillsData()
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

func (s *Server) findSkill(name string) *skill.Skill {
	store := s.cfg.Ctrl.Skills()
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