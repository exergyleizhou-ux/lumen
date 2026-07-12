package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/control"
	"lumen/internal/runstate"
)

func TestChatRunLifecycleIncludesRunIDAndSucceeds(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"finished\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer model.Close()

	dir := t.TempDir()
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
		"prompt": "finish this task", "api_key": "sk-test", "provider": "deepseek", "mode": "bypass",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)

	events := parseSSEData(t, rec.Body.String())
	var runID string
	var firstSeq float64
	for _, ev := range events {
		if id, _ := ev["run_id"].(string); id != "" {
			if runID == "" {
				runID = id
				firstSeq, _ = ev["seq"].(float64)
			} else if runID != id {
				t.Fatalf("response mixed run ids %q and %q", runID, id)
			}
		}
	}
	if runID == "" || firstSeq != 1 {
		t.Fatalf("missing first stamped run event: %s", rec.Body.String())
	}
	finished, err := runs.Get(runID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != runstate.StatusSucceeded || finished.StopReason != "finished" {
		t.Fatalf("unexpected run terminal state: %#v", finished)
	}
	if !strings.Contains(rec.Body.String(), `"kind":"stream_done"`) ||
		!strings.Contains(rec.Body.String(), `"run_id":"`+runID+`"`) {
		t.Fatalf("stream_done missing run id: %s", rec.Body.String())
	}
}

func TestChatConfigureFailureCreatesNoGhostRun(t *testing.T) {
	t.Setenv("LUMEN_DEMO", "0")
	runs := runstate.NewManager(nil)
	s, err := New(Config{Addr: ":0", Ctrl: control.New(), Runs: runs})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"prompt": "ping"})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleChat(rec, req)
	if strings.Contains(rec.Body.String(), `"run_id":"run_`) {
		t.Fatalf("configure failure created a ghost run: %s", rec.Body.String())
	}
}

func writeRunTestConfig(t *testing.T, dir, baseURL string) {
	t.Helper()
	cfg := `default_model = "chat-test"

[[providers]]
name = "chat-test"
kind = "openai"
base_url = "` + baseURL + `"
model = "test-model"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(filepath.Join(dir, "lumen.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func parseSSEData(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("decode SSE line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}
