package langgraph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHealthUnavailableByDefault(t *testing.T) {
	h := Health()
	if h["available"].(bool) {
		t.Log("langgraph available on this host (LUMEN_LANGGRAPH=1 set)")
	}
	if h["hint"].(string) == "" {
		t.Error("health hint is empty")
	}
}

func TestRunUnavailable(t *testing.T) {
	if IsAvailable() {
		t.Skip("langgraph venv available; skipping unavailable test")
	}
	ctx := context.Background()
	resp := Run(ctx, RunRequest{ProjectID: "t", Prompt: "hi"})
	if resp.OK {
		t.Error("expected ok=false when langgraph unavailable")
	}
	if resp.Error == "" {
		t.Error("expected non-empty error when langgraph unavailable")
	}
}

func TestRunEmptyPrompt(t *testing.T) {
	if !IsAvailable() {
		t.Skip("langgraph venv not available")
	}
	ctx := context.Background()
	resp := Run(ctx, RunRequest{ProjectID: "t", Prompt: ""})
	if resp.OK {
		t.Error("expected ok=false for empty prompt")
	}
	if resp.Error == "" {
		t.Error("expected error message for empty prompt")
	}
}

func TestRunIntegration(t *testing.T) {
	if !IsAvailable() {
		t.Skip("langgraph venv not available")
	}
	ctx := context.Background()
	resp := Run(ctx, RunRequest{
		ProjectID: "test-integration",
		Prompt:    "总结工作区内容",
	})
	if !resp.OK {
		t.Fatalf("expected ok=true, got error: %s", resp.Error)
	}
	// Result should contain LangGraph or workspace or suggestions
	r := resp.Result
	hasMarker := strings.Contains(r, "LangGraph") ||
		strings.Contains(r, "工作区") ||
		strings.Contains(r, "建议")
	if !hasMarker {
		t.Errorf("result missing expected markers: %s", r[:min(len(r), 200)])
	}
}

func TestRunWithWorkspace(t *testing.T) {
	if !IsAvailable() {
		t.Skip("langgraph venv not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# hello\ncontent for graph\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "script.py"), []byte("print('ok')\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp := Run(ctx, RunRequest{
		ProjectID: "test-ws",
		Prompt:    "总结工作区",
		Workspace: dir,
	})
	if !resp.OK {
		t.Fatalf("expected ok=true, got error: %s", resp.Error)
	}
	if !strings.Contains(resp.Result, "notes.md") {
		t.Errorf("result should contain notes.md, got: %s", resp.Result[:min(len(resp.Result), 300)])
	}
	if !strings.Contains(resp.Result, "script.py") {
		t.Errorf("result should contain script.py, got: %s", resp.Result[:min(len(resp.Result), 300)])
	}
	if !strings.Contains(resp.Result, "建议") {
		t.Error("result should contain suggestions")
	}
}

func TestRunNoWorkspace(t *testing.T) {
	if !IsAvailable() {
		t.Skip("langgraph venv not available")
	}
	ctx := context.Background()
	resp := Run(ctx, RunRequest{
		ProjectID: "no-ws",
		Prompt:    "分析",
	})
	if !resp.OK {
		t.Fatalf("expected ok=true even without workspace, got error: %s", resp.Error)
	}
	// Should report no workspace gracefully
	if !strings.Contains(resp.Result, "无") && !strings.Contains(resp.Result, "空") {
		t.Logf("result (no ws): %s", resp.Result[:min(len(resp.Result), 200)])
	}
}

func TestRunBadWorkspace(t *testing.T) {
	if !IsAvailable() {
		t.Skip("langgraph venv not available")
	}
	ctx := context.Background()
	resp := Run(ctx, RunRequest{
		ProjectID: "bad-ws",
		Prompt:    "分析",
		Workspace: "/nonexistent/path/xyz",
	})
	if !resp.OK {
		t.Fatalf("expected ok=true (runner handles bad path gracefully), got error: %s", resp.Error)
	}
	if !strings.Contains(resp.Result, "不存在") && !strings.Contains(resp.Result, "无") {
		t.Logf("result (bad ws): %s", resp.Result[:min(len(resp.Result), 200)])
	}
}
