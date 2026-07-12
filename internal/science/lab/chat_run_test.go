package lab

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/config"
	"lumen/internal/runstate"
	sciconfig "lumen/internal/science/config"
)

func TestLabChatRunUsesSharedRuntime(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"science complete\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer model.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath, err := config.UserConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `default_model = "science-test"

[[providers]]
name = "science-test"
kind = "openai"
base_url = "` + model.URL + `"
model = "test"
api_key = "test"
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	sciDir := filepath.Join(home, ".lumen", "science")
	scienceCfg := sciconfig.Default()
	scienceCfg.Providers[sciconfig.DefaultProvider] = sciconfig.ProviderCfg{Key: "science-test"}
	if err := sciconfig.Save(sciDir, scienceCfg); err != nil {
		t.Fatal(err)
	}

	runs := runstate.NewManager(nil)
	api := NewAPI(sciDir, "test", nil, 0, runs)
	proj, err := api.projects.Create("Runtime Science", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"project_id": proj.Slug, "prompt": "analyze", "mode": "bypass"})
	req := httptest.NewRequest(http.MethodPost, "/api/lab/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.handleChat(rec, req)

	runID := ""
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload) == nil {
			if id, _ := payload["run_id"].(string); id != "" {
				runID = id
			}
		}
	}
	if runID == "" {
		t.Fatalf("Lab SSE missing run id: %s", rec.Body.String())
	}
	finished, err := runs.Get(runID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Profile != "science" || finished.Status != runstate.StatusSucceeded {
		t.Fatalf("Lab run=%#v", finished)
	}
	if !strings.Contains(rec.Body.String(), `"kind":"stream_done"`) || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("Lab terminal SSE=%s", rec.Body.String())
	}
}
