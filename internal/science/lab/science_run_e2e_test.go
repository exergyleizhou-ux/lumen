package lab

import (
	"bufio"
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

	"lumen/internal/config"
	"lumen/internal/runstate"
	sciconfig "lumen/internal/science/config"
	"lumen/internal/science/lab/provenance"
)

func TestScienceRunE2ELinksArtifactAndProvenance(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		call := calls.Add(1)
		if call == 1 {
			args, _ := json.Marshal(map[string]string{"path": "reports/result.md", "content": "# Result\n\nEvidence-backed conclusion.\n"})
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"write-report\",\"type\":\"function\",\"function\":{\"name\":\"write_file\",\"arguments\":%q}}]}}]}\n\n", string(args))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		if call > 2 {
			http.Error(w, "provider failed", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"report complete\"}}]}\n\ndata: [DONE]\n\n"))
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
	cfg := `default_model = "science-e2e"

[[providers]]
name = "science-e2e"
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
	proj, err := api.projects.Create("Artifact Runtime", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"project_id": proj.Slug, "prompt": "write the report", "mode": "bypass"})
	req := httptest.NewRequest(http.MethodPost, "/api/lab/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	api.handleChat(rec, req)

	runID := labRunIDFromSSE(rec.Body.String())
	if runID == "" {
		t.Fatalf("missing run id: %s", rec.Body.String())
	}
	run, err := runs.Get(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != runstate.StatusSucceeded || run.SessionID == "" {
		t.Fatalf("run=%#v", run)
	}
	projectDir, _ := api.projects.ProjectDir(proj.Slug)
	provFile, err := os.Open(filepath.Join(projectDir, "provenance.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer provFile.Close()
	var records []provenance.Record
	scanner := bufio.NewScanner(provFile)
	for scanner.Scan() {
		var record provenance.Record
		if json.Unmarshal(scanner.Bytes(), &record) == nil {
			records = append(records, record)
		}
	}
	if len(records) != 1 {
		t.Fatalf("provenance=%#v", records)
	}
	got := records[0]
	if got.RunID != run.ID || got.SessionID != run.SessionID || got.Path != "workspace/reports/result.md" || !strings.HasPrefix(got.ContentHash, "sha256:") {
		t.Fatalf("provenance linkage=%#v run=%#v", got, run)
	}

	failureBody, _ := json.Marshal(map[string]any{"project_id": proj.Slug, "prompt": "provider failure", "mode": "bypass", "session_id": run.SessionID})
	failureReq := httptest.NewRequest(http.MethodPost, "/api/lab/chat", bytes.NewReader(failureBody))
	failureRec := httptest.NewRecorder()
	api.handleChat(failureRec, failureReq)
	failureRunID := labRunIDFromSSE(failureRec.Body.String())
	failureRun, err := runs.Get(failureRunID)
	if err != nil {
		t.Fatal(err)
	}
	if failureRun.Status != runstate.StatusFailed || failureRun.StopReason != "error" {
		t.Fatalf("provider failure became success: %#v\n%s", failureRun, failureRec.Body.String())
	}
}

func labRunIDFromSSE(body string) string {
	runID := ""
	for _, line := range strings.Split(body, "\n") {
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
	return runID
}
