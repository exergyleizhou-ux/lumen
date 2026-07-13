package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/control"
	"lumen/internal/event"
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

func TestRunCancelAPI(t *testing.T) {
	runs := runstate.NewManager(nil)
	s, err := New(Config{Addr: ":0", Ctrl: control.New(), Runs: runs})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runs.Start("session", "code", "cancel", "")
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cleanup := s.beginActiveRun(context.Background(), runstate.LocalOwner, run.ID, time.Minute)
	defer cleanup()

	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/runs/"+run.ID+"/cancel", nil))
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel API did not cancel run context")
	}

	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/runs/"+run.ID+"/cancel", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("repeat cancel status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/runs/run_missing/cancel", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatConfigureFailureCreatesNoGhostRun(t *testing.T) {
	t.Setenv("LUMEN_DEMO", "0")
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
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

func TestRunAPIGetAndReplay(t *testing.T) {
	runs := runstate.NewManager(nil)
	run, err := runs.Start("session-api", "code", "api test", "")
	if err != nil {
		t.Fatal(err)
	}
	sink := runs.WrapSink(run.ID, event.Discard)
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.Text, Text: "hello"})
	sink.Emit(event.Event{Kind: event.TurnDone, StopReason: "finished"})
	if _, err := runs.Finish(run.ID, nil); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{Addr: ":0", Ctrl: control.New(), Runs: runs})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/runs/"+run.ID, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("get run status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/runs/"+run.ID+"/events", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get events status=%d body=%s", rec.Code, rec.Body.String())
	}
	var all struct {
		Events []event.Event `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&all); err != nil {
		t.Fatal(err)
	}
	if len(all.Events) != 3 {
		t.Fatalf("expected 3 events, got %#v", all.Events)
	}

	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/runs/"+run.ID+"/events?after=2", nil))
	var replay struct {
		Events []event.Event `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&replay); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(replay.Events) != 1 || replay.Events[0].Seq != 3 {
		t.Fatalf("unexpected replay status=%d events=%#v", rec.Code, replay.Events)
	}
}

func TestRunAPIRejectsMissingAndInvalidRequests(t *testing.T) {
	runs := runstate.NewManager(nil)
	s, err := New(Config{Addr: ":0", Ctrl: control.New(), Runs: runs})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		want int
	}{
		{"/v1/runs/run_missing", http.StatusNotFound},
		{"/v1/runs/run_missing/events?after=x", http.StatusBadRequest},
		{"/v1/runs/", http.StatusBadRequest},
		{"/v1/runs/run_missing/events/extra", http.StatusBadRequest},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("GET %s status=%d want=%d body=%s", tc.path, rec.Code, tc.want, rec.Body.String())
		}
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
