package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/permission"
)

func newBareServer(t *testing.T) *Server {
	t.Helper()
	ctrl := control.New()
	s, err := New(Config{Addr: ":0", Ctrl: ctrl})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func withDemo(t *testing.T) *Server {
	t.Helper()
	t.Setenv("LUMEN_DEMO", "1")
	return newBareServer(t)
}

func withoutDemo(t *testing.T) *Server {
	t.Helper()
	t.Setenv("LUMEN_DEMO", "")
	return newBareServer(t)
}

func postWorkflowSSE(t *testing.T, s *Server, body any) string {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/workflow", bytes.NewReader(b))
	rec := httptest.NewRecorder()
	s.handleWorkflow(rec, req)
	return rec.Body.String()
}

func writeDeepseekConfig(t *testing.T, dir string) {
	t.Helper()
	cfg := `default_model = "deepseek"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
model = "deepseek-chat"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(filepath.Join(dir, "lumen.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHelpListsWorkflowCommands(t *testing.T) {
	h := helpText()
	for _, cmd := range []string{"/workflow", "/execute", "/reject", "/ultra", "/goal"} {
		if !strings.Contains(h, cmd) {
			t.Errorf("help missing %s", cmd)
		}
	}
}

func TestParseUIModeBypass(t *testing.T) {
	if parseUIMode("bypass") != permission.ModeBypass {
		t.Error("bypass mode")
	}
	if uiModeFromPermission(permission.ModeBypass) != "bypass" {
		t.Error("ui mode bypass")
	}
}

func TestTextCollectorGathersEvents(t *testing.T) {
	c := &textCollector{}
	c.Emit(event.Event{Kind: event.Phase, Text: "planning"})
	c.Emit(event.Event{Kind: event.Text, Text: "step one"})
	if c.Len() == 0 {
		t.Fatal("collector empty")
	}
}

// TestWorkflowOutcomeMatrix exercises every verifier permutation through the real handlers. goal:d6aa846b round9
func TestWorkflowOutcomeMatrix(t *testing.T) {
	t.Run("demo_no_key_sse_plan_ready", func(t *testing.T) {
		s := withDemo(t)
		out := postWorkflowSSE(t, s, map[string]string{"action": "workflow", "prompt": "demo task"})
		if !strings.Contains(out, "[Demo mode] Plan for: demo task") {
			t.Fatalf("demo text missing:\n%s", out)
		}
		if !strings.Contains(out, `"kind":"plan_ready"`) {
			t.Fatalf("plan_ready missing:\n%s", out)
		}
		ready, prompt := s.planStatus()
		if !ready || prompt != "demo task" {
			t.Fatalf("planStatus ready=%v prompt=%q", ready, prompt)
		}
	})

	t.Run("configure_fail_sse_no_plan_ready", func(t *testing.T) {
		dir := t.TempDir()
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(wd) })
		s, err := New(Config{Addr: ":0", Ctrl: control.New(), LocalConfigPath: filepath.Join(dir, "missing.toml")})
		if err != nil {
			t.Fatal(err)
		}
		out := postWorkflowSSE(t, s, map[string]string{"action": "workflow", "prompt": "task"})
		if !strings.Contains(out, "no providers configured") {
			t.Fatalf("configure error missing:\n%s", out)
		}
		if strings.Contains(out, `"kind":"plan_ready"`) {
			t.Fatalf("plan_ready must not appear on configure fail:\n%s", out)
		}
		ready, _ := s.planStatus()
		if ready {
			t.Fatal("plan should not be ready after configure fail")
		}
	})

	t.Run("plan_fail_after_configure_no_plan_ready", func(t *testing.T) {
		dir := t.TempDir()
		wd, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(wd) })
		writeDeepseekConfig(t, dir)
		t.Setenv("LUMEN_DEMO", "")
		t.Setenv("DEEPSEEK_API_KEY", "sk-invalid-test-key")

		s := newBareServer(t)
		out := postWorkflowSSE(t, s, map[string]any{
			"action":   "workflow",
			"prompt":   "auth fail task",
			"api_key":  "sk-invalid-test-key",
			"provider": "deepseek",
		})
		if strings.Contains(out, "[Demo mode]") {
			t.Fatalf("should not demo with api_key:\n%s", out)
		}
		if strings.Contains(out, `"kind":"plan_ready"`) {
			t.Fatalf("plan_ready must not appear when Plan fails:\n%s", out)
		}
		if !strings.Contains(out, "authentication failed") && !strings.Contains(out, `"kind":"error"`) {
			t.Fatalf("expected plan/auth error in SSE:\n%s", out)
		}
		ready, _ := s.planStatus()
		if ready {
			t.Fatal("plan must stay not-ready after Plan error")
		}
	})

	t.Run("plan_fail_command_path_no_plan_ready", func(t *testing.T) {
		dir := t.TempDir()
		wd, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(wd) })
		writeDeepseekConfig(t, dir)
		t.Setenv("LUMEN_DEMO", "")
		t.Setenv("DEEPSEEK_API_KEY", "sk-invalid-test-key")

		s := newBareServer(t)
		text, data, err := s.execWorkflowCommand("/workflow auth fail task", "sk-invalid-test-key", "deepseek")
		if err == nil {
			t.Fatalf("expected Plan error, got text=%q data=%v", text, data)
		}
		if strings.Contains(text, "[Demo mode]") {
			t.Fatalf("command path must not demo with api_key: %q", text)
		}
		ready, _ := s.planStatus()
		if ready {
			t.Fatal("plan must stay not-ready after command-path Plan error")
		}
		m, _ := data.(map[string]any)
		if m != nil && m["plan_ready"] == true {
			t.Fatalf("data must not mark plan_ready: %v", m)
		}
	})

	t.Run("demo_command_path_plan_ready", func(t *testing.T) {
		s := withDemo(t)
		text, data, err := s.execWorkflowCommand("/workflow sync task", "", "")
		if err != nil {
			t.Fatalf("command workflow: %v", err)
		}
		if !strings.Contains(text, "[Demo mode]") {
			t.Fatalf("text: %q", text)
		}
		m, _ := data.(map[string]any)
		if m["plan_ready"] != true {
			t.Errorf("data: %v", m)
		}
		ready, _ := s.planStatus()
		if !ready {
			t.Fatal("plan should be ready")
		}
	})

	t.Run("execute_without_plan_sse", func(t *testing.T) {
		s := withDemo(t)
		out := postWorkflowSSE(t, s, map[string]string{"action": "execute"})
		if !strings.Contains(out, "no plan ready") {
			t.Fatalf("expected no-plan error:\n%s", out)
		}
		if strings.Contains(out, `"kind":"plan_ready"`) {
			t.Fatal("execute without plan must not emit plan_ready")
		}
	})

	t.Run("reject_clears_plan", func(t *testing.T) {
		s := withDemo(t)
		s.setPlanReady("pending")
		text, _, err := s.execWorkflowCommand("/reject", "", "")
		if err != nil || !strings.Contains(text, "rejected") {
			t.Fatalf("reject: err=%v text=%q", err, text)
		}
		ready, _ := s.planStatus()
		if ready {
			t.Fatal("reject should clear plan")
		}
	})
}

// TestCommandPlanFailHTTPTranscript records the full HTTP exchange for plan-fail via /v1/command.
func TestCommandPlanFailHTTPTranscript(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	writeDeepseekConfig(t, dir)
	t.Setenv("LUMEN_DEMO", "")

	s := newBareServer(t)
	payload, _ := json.Marshal(map[string]string{
		"command":  "/workflow auth fail task",
		"api_key":  "sk-invalid-test-key",
		"provider": "deepseek",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/command", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleCommand(rec, req)

	transcript := strings.Join([]string{
		"=== HTTP transcript (temp-dir lumen.toml + bad api_key) ===",
		"POST /v1/command HTTP/1.1",
		"Content-Type: application/json",
		"",
		string(payload),
		"---",
		fmt.Sprintf("HTTP/%d", rec.Code),
		rec.Body.String(),
		fmt.Sprintf("plan_ready_after=%v", func() bool { r, _ := s.planStatus(); return r }()),
	}, "\n")
	t.Log(transcript)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200, got body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "plan_ready") {
		t.Fatal("response must not contain plan_ready")
	}
	ready, _ := s.planStatus()
	if ready {
		t.Fatal("plan must stay not-ready after command-path Plan error")
	}
}

func TestHandleCommandPassesAPIKeyToWorkflow(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	writeDeepseekConfig(t, dir)
	t.Setenv("LUMEN_DEMO", "")

	s := newBareServer(t)
	body, _ := json.Marshal(map[string]string{
		"command":  "/workflow auth fail via http",
		"api_key":  "sk-invalid-test-key",
		"provider": "deepseek",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/command", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleCommand(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected error status, got 200 body=%s", rec.Body.String())
	}
	ready, _ := s.planStatus()
	if ready {
		t.Fatal("handleCommand must not set plan_ready when Plan fails")
	}
}

func TestHandleWorkflowRejectJSON(t *testing.T) {
	s := withDemo(t)
	s.setPlanReady("pending")
	body, _ := json.Marshal(map[string]string{"action": "reject"})
	req := httptest.NewRequest(http.MethodPost, "/v1/workflow", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleWorkflow(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	ready, _ := s.planStatus()
	if ready {
		t.Error("plan should be cleared")
	}
}
