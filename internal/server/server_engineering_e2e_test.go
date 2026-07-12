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
	"sync/atomic"
	"testing"

	"lumen/internal/control"
	"lumen/internal/runstate"
)

func TestEngineeringTaskE2ESucceedsOnlyAfterVerification(t *testing.T) {
	result := runEngineeringE2E(t, "package main\n\nfunc main() {}\n")
	if result.run.Status != runstate.StatusSucceeded || result.run.StopReason != "finished" {
		t.Fatalf("terminal run=%#v", result.run)
	}
	if !strings.Contains(result.sse, `"kind":"verify_result"`) || !strings.Contains(result.sse, `"ok":true`) {
		t.Fatalf("missing verified success evidence: %s", result.sse)
	}
}

func TestEngineeringTaskE2ERejectsInvalidCode(t *testing.T) {
	result := runEngineeringE2E(t, "package main\n\nfunc main( {\n")
	if result.run.Status != runstate.StatusFailed || result.run.StopReason != "verification_failed" {
		t.Fatalf("terminal run=%#v", result.run)
	}
	if !strings.Contains(result.sse, `"ok":false`) || !strings.Contains(result.sse, "engineering verification failed") {
		t.Fatalf("invalid code was not reported honestly: %s", result.sse)
	}
}

type engineeringE2EResult struct {
	run runstate.Run
	sse string
}

func runEngineeringE2E(t *testing.T, content string) engineeringE2EResult {
	t.Helper()
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			arguments, _ := json.Marshal(map[string]string{"path": "main.go", "content": content})
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"write-1\",\"type\":\"function\",\"function\":{\"name\":\"write_file\",\"arguments\":%q}}]}}]}\n\n", string(arguments))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"implemented\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer model.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/e2e\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRunTestConfig(t, dir, model.URL)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	t.Setenv("LUMEN_DEMO", "")
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")

	runs := runstate.NewManager(nil)
	s, err := New(Config{Addr: ":0", Ctrl: control.New(), Runs: runs})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"prompt": "create a valid Go entrypoint", "api_key": "sk-test", "provider": "deepseek", "mode": "bypass",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)

	events := parseSSEData(t, rec.Body.String())
	runID := ""
	for _, ev := range events {
		if id, _ := ev["run_id"].(string); id != "" {
			runID = id
		}
	}
	if runID == "" {
		t.Fatalf("no run id in SSE: %s", rec.Body.String())
	}
	finished, err := runs.Get(runID)
	if err != nil {
		t.Fatal(err)
	}
	return engineeringE2EResult{run: finished, sse: rec.Body.String()}
}
