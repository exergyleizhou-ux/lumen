package lab

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/science/lab/project"
)

func testLabServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	sci := filepath.Join(t.TempDir(), "science")
	if err := os.MkdirAll(sci, 0o700); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{SciDir: sci, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, sci
}

func createProject(t *testing.T, ts *httptest.Server, title string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/projects", bytes.NewReader([]byte(`{"title":"`+title+`"}`)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var proj map[string]string
	_ = json.NewDecoder(res.Body).Decode(&proj)
	if proj["slug"] == "" {
		t.Fatalf("no slug: %v", proj)
	}
	return proj["slug"]
}

func TestSessionHistoryAPI(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Session Hist")
	// Create session via API
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/projects/"+slug+"/sessions",
		bytes.NewReader([]byte(`{"title":"s1"}`)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sess project.Session
	_ = json.NewDecoder(res.Body).Decode(&sess)
	res.Body.Close()
	if sess.ID == "" {
		t.Fatal("no session id")
	}
	// Append turns via store (simulates chat persistence)
	store := project.NewStore(sci)
	_, err = store.AppendTurns(slug, sess.ID,
		project.Turn{Role: "user", Text: "persist me"},
		project.Turn{Role: "assistant", Text: "ok **bold**", Tools: []project.ToolSummary{{Name: "bash", Status: "done"}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	// List
	listRes, err := http.Get(ts.URL + "/api/lab/projects/" + slug + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	var listBody map[string]any
	_ = json.NewDecoder(listRes.Body).Decode(&listBody)
	listRes.Body.Close()
	if listBody["count"].(float64) < 1 {
		t.Fatalf("list %v", listBody)
	}
	// Get full history
	getRes, err := http.Get(ts.URL + "/api/lab/projects/" + slug + "/sessions/" + sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer getRes.Body.Close()
	if getRes.StatusCode != 200 {
		t.Fatalf("status %d", getRes.StatusCode)
	}
	var full project.Session
	if err := json.NewDecoder(getRes.Body).Decode(&full); err != nil {
		t.Fatal(err)
	}
	if len(full.Turns) != 2 || full.Turns[0].Text != "persist me" {
		t.Fatalf("turns %+v", full.Turns)
	}
}

func TestFileSearchAPI(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Search Proj")
	store := project.NewStore(sci)
	ws, _ := store.WorkspacePath(slug)
	_ = os.WriteFile(filepath.Join(ws, "notes.md"), []byte("findme unique-token-xyz\n"), 0o600)

	res, err := http.Get(ts.URL + "/api/lab/files/search?project_id=" + slug + "&q=unique-token-xyz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body struct {
		Hits  []FileSearchHit `json:"hits"`
		Count int             `json:"count"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Count < 1 {
		t.Fatalf("hits %+v", body)
	}
	found := false
	for _, h := range body.Hits {
		if h.Path == "notes.md" && h.Match == "content" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected content hit: %+v", body.Hits)
	}
}

func TestSkillsEnableAPI(t *testing.T) {
	ts, _ := testLabServer(t)
	slug := createProject(t, ts, "Skills Proj")
	// PUT enabled
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/lab/skills?project_id="+slug,
		bytes.NewReader([]byte(`{"project_id":"`+slug+`","enabled":["alpha","beta"]}`)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("put %d", res.StatusCode)
	}
	getRes, err := http.Get(ts.URL + "/api/lab/skills?project_id=" + slug)
	if err != nil {
		t.Fatal(err)
	}
	defer getRes.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(getRes.Body).Decode(&body)
	en, ok := body["enabled"].([]any)
	if !ok || len(en) != 2 {
		t.Fatalf("enabled %v", body["enabled"])
	}
	if body["enabled_filter"] != true {
		t.Fatalf("filter %v", body["enabled_filter"])
	}
}

func TestComputeJobGlobsInBody(t *testing.T) {
	ts, _ := testLabServer(t)
	slug := createProject(t, ts, "Compute Proj")
	// Submit with globs — will fail SSH quickly but should accept contract
	body := `{"host":"no-such-host.invalid","command":"echo hi","timeout_sec":1,"output_globs":["*.csv"],"work_dir":"/tmp"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/compute/jobs?project_id="+slug, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var job map[string]any
	_ = json.NewDecoder(res.Body).Decode(&job)
	if job["id"] == nil || job["id"] == "" {
		t.Fatalf("job %v", job)
	}
	globs, _ := job["output_globs"].([]any)
	if len(globs) != 1 {
		t.Fatalf("globs %v", job["output_globs"])
	}
}
