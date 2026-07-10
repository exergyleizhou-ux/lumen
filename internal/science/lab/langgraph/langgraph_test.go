package langgraph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthUnavailableWithoutEnv(t *testing.T) {
	t.Setenv("LUMEN_LANGGRAPH", "")
	h := Health()
	if h["available"] != false {
		t.Fatalf("expected available false without env: %v", h)
	}
	hint, _ := h["hint"].(string)
	if strings.TrimSpace(hint) == "" {
		t.Fatal("hint must be non-empty")
	}
}

func TestRunUnavailableClearError(t *testing.T) {
	t.Setenv("LUMEN_LANGGRAPH", "0")
	resp := Run(context.Background(), RunRequest{Prompt: "x"})
	if resp.OK {
		t.Fatal("expected ok=false")
	}
	if strings.TrimSpace(resp.Error) == "" {
		t.Fatal("error must not be empty")
	}
}

func TestRunEmptyPrompt(t *testing.T) {
	// Force available path only if real env ready; else skip empty-prompt via unavailable
	home := os.Getenv("HOME")
	venv := filepath.Join(home, ".lumen", "langgraph-venv", "bin", "python3")
	if _, err := os.Stat(venv); err != nil {
		t.Skip("no local langgraph venv")
	}
	t.Setenv("LUMEN_LANGGRAPH", "1")
	t.Setenv("LUMEN_LANGGRAPH_VENV", filepath.Join(home, ".lumen", "langgraph-venv"))
	t.Setenv("LUMEN_LANGGRAPH_SCRIPT", filepath.Join(home, ".lumen", "langgraph_runner.py"))
	if !IsAvailable() {
		t.Skip("langgraph import not available in venv")
	}
	resp := Run(context.Background(), RunRequest{Prompt: "   "})
	if resp.OK || !strings.Contains(resp.Error, "prompt") {
		t.Fatalf("expected prompt error, got %+v", resp)
	}
}

func TestRunIntegration(t *testing.T) {
	home := os.Getenv("HOME")
	venv := filepath.Join(home, ".lumen", "langgraph-venv")
	script := filepath.Join(home, ".lumen", "langgraph_runner.py")
	if _, err := os.Stat(filepath.Join(venv, "bin", "python3")); err != nil {
		t.Skip("no local langgraph venv")
	}
	if _, err := os.Stat(script); err != nil {
		t.Skip("no runner script")
	}
	t.Setenv("LUMEN_LANGGRAPH", "1")
	t.Setenv("LUMEN_LANGGRAPH_VENV", venv)
	t.Setenv("LUMEN_LANGGRAPH_SCRIPT", script)
	if !IsAvailable() {
		t.Skip("langgraph not importable")
	}
	resp := Run(context.Background(), RunRequest{ProjectID: "demo", Prompt: "integration hello"})
	if !resp.OK {
		t.Fatalf("run failed: %+v", resp)
	}
	if !strings.Contains(resp.Result, "LangGraph processed") {
		t.Fatalf("unexpected result: %q", resp.Result)
	}
	h := Health()
	if h["available"] != true {
		t.Fatalf("health available: %v", h)
	}
}
