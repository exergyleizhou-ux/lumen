// Unified workflow orchestrator for /v1/command and /v1/workflow SSE. goal:d6aa846b round9
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"lumen/internal/event"
	"lumen/internal/permission"
	"lumen/internal/runstate"
	"lumen/internal/workspace"
)

// planState tracks the workflow review gate (mirrors cmd/lumen/terminal.go).
type planState struct {
	lastPlan  string
	planReady bool
}

func (s *Server) setRuntimePlanReady(rt *requestRuntime, prompt string) {
	if rt.entry == nil {
		s.setPlanReady(prompt)
		return
	}
	rt.entry.Plan.lastPlan, rt.entry.Plan.planReady = prompt, true
}
func (s *Server) clearRuntimePlan(rt *requestRuntime) {
	if rt.entry == nil {
		s.clearPlan()
		return
	}
	rt.entry.Plan = planState{}
}
func (s *Server) runtimePlanStatus(rt *requestRuntime) (bool, string) {
	if rt.entry == nil {
		return s.planStatus()
	}
	return rt.entry.Plan.planReady, rt.entry.Plan.lastPlan
}

// workflowEmit streams workflow events to SSE (nil discards).
type workflowEmit func(kind, text string)

func (s *Server) setPlanReady(prompt string) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	s.plan.lastPlan = prompt
	s.plan.planReady = true
}

func (s *Server) clearPlan() {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	s.plan.planReady = false
	s.plan.lastPlan = ""
}

func (s *Server) planStatus() (ready bool, prompt string) {
	s.planMu.Lock()
	defer s.planMu.Unlock()
	return s.plan.planReady, s.plan.lastPlan
}

func (s *Server) execWorkflowCommandRuntime(rt *requestRuntime, cmd, apiKey, provider string) (string, any, error) {
	lower := strings.ToLower(strings.TrimSpace(cmd))

	var action, prompt string
	switch {
	case lower == "/reject":
		ready, _ := s.runtimePlanStatus(rt)
		if !ready {
			return "no plan to reject", map[string]any{"plan_ready": false}, nil
		}
		s.clearRuntimePlan(rt)
		return "✗ plan rejected", map[string]any{"rejected": true}, nil
	case strings.HasPrefix(lower, "/workflow "):
		action, prompt = "workflow", strings.TrimSpace(cmd[len("/workflow "):])
	case lower == "/execute":
		action, prompt = "execute", ""
	case strings.HasPrefix(lower, "/ultra "):
		action, prompt = "ultra", strings.TrimSpace(cmd[len("/ultra "):])
	case strings.HasPrefix(lower, "/goal "):
		action, prompt = "goal", strings.TrimSpace(cmd[len("/goal "):])
	default:
		return "", nil, fmt.Errorf("not a workflow command")
	}

	if rt.entry == nil {
		s.turnMu.Lock()
		defer s.turnMu.Unlock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return s.runWorkflowAction(rt, ctx, action, prompt, apiKey, provider, "", nil)
}

func (s *Server) execWorkflowCommand(cmd, apiKey, provider string) (string, any, error) {
	rt := &requestRuntime{owner: runstate.LocalOwner, session: "local", ctrl: s.cfg.Ctrl}
	wd, _ := os.Getwd()
	rt.ws, _ = workspace.NewLocal("local", wd, "", nil)
	return s.execWorkflowCommandRuntime(rt, cmd, apiKey, provider)
}

func (s *Server) handleWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Action   string `json:"action"`
		Prompt   string `json:"prompt"`
		APIKey   string `json:"api_key,omitempty"`
		Provider string `json:"provider,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Action == "" {
		jsonErr(w, "action required", http.StatusBadRequest)
		return
	}
	rt := s.runtimeOrError(w, r)
	if rt == nil {
		return
	}
	defer s.releaseRuntime(rt)

	if req.Action == "reject" {
		text, data, err := s.execWorkflowCommandRuntime(rt, "/reject", "", "")
		if err != nil {
			jsonErr(w, err.Error(), http.StatusBadRequest)
			return
		}
		jsonOK(w, map[string]any{"text": text, "data": data})
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

	if rt.entry == nil {
		s.turnMu.Lock()
		defer s.turnMu.Unlock()
	}

	sink := sseSink{w: w, flusher: flusher}
	emit := func(kind, text string) { sink.emit(kind, text) }
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	_, _, _ = s.runWorkflowAction(rt, ctx, req.Action, req.Prompt, req.APIKey, req.Provider, "", emit)

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

// runWorkflowAction is the single orchestrator for sync (/v1/command) and SSE (/v1/workflow).
func (s *Server) runWorkflowAction(rt *requestRuntime, ctx context.Context, action, prompt, apiKey, provider, cfgPath string, emit workflowEmit) (string, map[string]any, error) {
	if emit == nil {
		emit = func(_, _ string) {}
	}
	if rt.entry != nil && (apiKey != "" || provider != "") {
		return "", nil, fmt.Errorf("request provider overrides are unsupported; configure the tenant provider")
	}
	if rt.entry == nil {
		applyRuntimeKey(apiKey, provider)
	}

	if workflowDemoOnly(apiKey) {
		return s.runWorkflowDemo(rt, action, prompt, emit)
	}

	collector := &textCollector{}
	sink := workflowEventSink(collector, emit)
	if err := s.configureRuntime(rt, sink, cfgPath); err != nil {
		if rt.entry != nil {
			s.controllers.discard(rt.owner, rt.session, rt.ctrl)
		}
		emit("error", err.Error())
		return "", nil, err
	}

	body := func() string {
		if collector.Len() == 0 {
			return "(no text output)"
		}
		return collector.String()
	}

	switch action {
	case "workflow":
		if strings.TrimSpace(prompt) == "" {
			emit("error", "prompt required")
			return "", nil, fmt.Errorf("prompt required")
		}
		emit("phase", "📋 Plan phase (read-only)")
		rt.ctrl.SetPermissionMode(permission.ModePlan)
		if err := rt.ctrl.Plan(ctx, prompt); err != nil {
			emit("error", err.Error())
			return "", nil, err
		}
		s.setRuntimePlanReady(rt, prompt)
		emit("plan_ready", prompt)
		return strings.TrimSpace("📋 Plan ready — review above, then /execute or /reject\n\n" + body()),
			map[string]any{"plan_ready": true, "prompt": prompt}, nil

	case "execute":
		ready, p := s.runtimePlanStatus(rt)
		if !ready {
			emit("error", "no plan ready — use workflow first")
			return "no plan ready — use /workflow <task> first", map[string]any{"plan_ready": false}, nil
		}
		emit("phase", "🚀 Executing plan")
		if ag := rt.ctrl.Agent(); ag != nil {
			ag.SetPlanMode(false)
		}
		rt.ctrl.SetPermissionMode(permission.ModeBypass)
		if err := rt.ctrl.Run(ctx, p); err != nil {
			emit("error", err.Error())
			return "", nil, err
		}
		s.clearRuntimePlan(rt)
		emit("workflow_done", "complete")
		return strings.TrimSpace("🚀 Executing plan…\n\n" + body()),
			map[string]any{"executed": true, "prompt": p}, nil

	case "ultra":
		if strings.TrimSpace(prompt) == "" {
			emit("error", "prompt required")
			return "", nil, fmt.Errorf("prompt required")
		}
		emit("phase", "⚡ Ultra: plan → auto-execute")
		rt.ctrl.SetPermissionMode(permission.ModePlan)
		if err := rt.ctrl.Plan(ctx, prompt); err != nil {
			emit("error", err.Error())
			return "", nil, err
		}
		planText := body()
		if ag := rt.ctrl.Agent(); ag != nil {
			ag.SetPlanMode(false)
		}
		rt.ctrl.SetPermissionMode(permission.ModeBypass)
		emit("phase", "🚀 Executing")
		if err := rt.ctrl.Run(ctx, prompt); err != nil {
			emit("error", err.Error())
			return "", nil, err
		}
		emit("workflow_done", "ultra complete")
		return strings.TrimSpace(fmt.Sprintf("⚡ Ultra mode\n\n%s\n\n🚀 Executing…\n\n%s", planText, body())),
			map[string]any{"ultra": true, "prompt": prompt}, nil

	case "goal":
		if strings.TrimSpace(prompt) == "" {
			emit("error", "prompt required")
			return "", nil, fmt.Errorf("prompt required")
		}
		emit("phase", "🎯 Goal: autonomous execution")
		if err := rt.ctrl.Run(ctx, prompt); err != nil {
			emit("error", err.Error())
			return "", nil, err
		}
		emit("workflow_done", "goal complete")
		return strings.TrimSpace("🎯 Goal execution\n\n" + body()),
			map[string]any{"goal": true, "prompt": prompt}, nil

	default:
		emit("error", "unknown action: "+action)
		return "", nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (s *Server) runWorkflowDemo(rt *requestRuntime, action, prompt string, emit workflowEmit) (string, map[string]any, error) {
	switch action {
	case "workflow":
		if strings.TrimSpace(prompt) == "" {
			emit("error", "prompt required")
			return "", nil, fmt.Errorf("prompt required")
		}
		emit("phase", "📋 Plan phase (read-only)")
		text := "[Demo mode] Plan for: " + prompt
		emit("text", text)
		s.setRuntimePlanReady(rt, prompt)
		emit("plan_ready", prompt)
		return strings.TrimSpace("📋 Plan ready — review above, then /execute or /reject\n\n" + text),
			map[string]any{"plan_ready": true, "prompt": prompt}, nil

	case "execute":
		ready, p := s.runtimePlanStatus(rt)
		if !ready {
			emit("error", "no plan ready — use workflow first")
			return "no plan ready — use /workflow <task> first", map[string]any{"plan_ready": false}, nil
		}
		emit("phase", "🚀 Executing plan")
		text := "[Demo mode] Executed plan: " + p
		emit("text", text)
		s.clearRuntimePlan(rt)
		emit("workflow_done", "complete")
		return strings.TrimSpace("🚀 Executing plan…\n\n" + text),
			map[string]any{"executed": true, "prompt": p}, nil

	case "ultra":
		if strings.TrimSpace(prompt) == "" {
			emit("error", "prompt required")
			return "", nil, fmt.Errorf("prompt required")
		}
		emit("phase", "⚡ Ultra: plan → auto-execute")
		planText := "[Demo mode] Plan for: " + prompt
		emit("text", planText)
		emit("phase", "🚀 Executing")
		execText := "[Demo mode] Ultra complete: " + prompt
		emit("text", execText)
		emit("workflow_done", "ultra complete")
		return strings.TrimSpace(fmt.Sprintf("⚡ Ultra mode\n\n%s\n\n🚀 Executing…\n\n%s", planText, execText)),
			map[string]any{"ultra": true, "prompt": prompt}, nil

	case "goal":
		if strings.TrimSpace(prompt) == "" {
			emit("error", "prompt required")
			return "", nil, fmt.Errorf("prompt required")
		}
		emit("phase", "🎯 Goal: autonomous execution")
		text := "[Demo mode] Goal: " + prompt
		emit("text", text)
		emit("workflow_done", "goal complete")
		return strings.TrimSpace("🎯 Goal execution\n\n" + text),
			map[string]any{"goal": true, "prompt": prompt}, nil

	default:
		emit("error", "unknown action: "+action)
		return "", nil, fmt.Errorf("unknown action: %s", action)
	}
}

func workflowEventSink(collector *textCollector, emit workflowEmit) event.Sink {
	return event.FuncSink(func(e event.Event) {
		collector.Emit(e)
		switch e.Kind {
		case event.Text, event.Reasoning, event.Phase:
			if e.Text != "" {
				emit(string(e.Kind), e.Text)
			}
		case event.Notice:
			if e.Text != "" {
				if e.Level == event.LevelErr {
					emit("error", e.Text)
				} else {
					emit("notice", e.Text)
				}
			}
		}
	})
}

func demoMode() bool { return os.Getenv("LUMEN_DEMO") == "1" }

func workflowDemoOnly(apiKey string) bool { return demoMode() && apiKey == "" }

func applyRuntimeKey(apiKey, provider string) {
	if apiKey == "" {
		return
	}
	envVar := "DEEPSEEK_API_KEY"
	switch provider {
	case "qwen":
		envVar = "DASHSCOPE_API_KEY"
	case "moonshot":
		envVar = "MOONSHOT_API_KEY"
	case "zhipu":
		envVar = "ZHIPU_API_KEY"
	}
	_ = os.Setenv(envVar, apiKey)
}

type textCollector struct {
	buf strings.Builder
}

func (t *textCollector) Emit(e event.Event) {
	switch e.Kind {
	case event.Text, event.Reasoning, event.Phase, event.Notice:
		if e.Text != "" {
			t.buf.WriteString(e.Text)
			if e.Kind == event.Phase {
				t.buf.WriteString("\n")
			}
		}
	}
}

func (t *textCollector) String() string { return t.buf.String() }
func (t *textCollector) Len() int       { return t.buf.Len() }
