package lab

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestSessionSearchAPI(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Search Sess")
	store := project.NewStore(sci)
	sess, err := store.CreateSession(slug, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.AppendTurns(slug, sess.ID,
		project.Turn{Role: "user", Text: "unique-token-zyx literature"},
		project.Turn{Role: "assistant", Text: "found papers"},
	)
	res, err := http.Get(ts.URL + "/api/lab/projects/" + slug + "/sessions?q=unique-token-zyx")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body["count"].(float64) < 1 {
		t.Fatalf("search %v", body)
	}
}

func TestComputeImportToWorkspace(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Import Job")
	// run local job with harvest
	body := `{"host":"local","command":"echo harvest-me > out.dat && echo ok","timeout_sec":10,"output_globs":["*.dat"]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/compute/jobs?project_id="+slug, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var job map[string]any
	_ = json.NewDecoder(res.Body).Decode(&job)
	res.Body.Close()
	jobID, _ := job["id"].(string)
	if jobID == "" {
		t.Fatalf("job %v", job)
	}
	// wait done
	var last map[string]any
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		gr, err := http.Get(ts.URL + "/api/lab/compute/jobs/" + jobID + "?project_id=" + slug)
		if err != nil {
			t.Fatal(err)
		}
		_ = json.NewDecoder(gr.Body).Decode(&last)
		gr.Body.Close()
		if st, _ := last["status"].(string); st == "done" || st == "failed" || st == "timeout" {
			break
		}
	}
	if last["status"] != "done" {
		t.Fatalf("status %v", last)
	}
	// import
	impBody := `{"path":"out.dat"}`
	ireq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/compute/jobs/"+jobID+"/import?project_id="+slug, bytes.NewReader([]byte(impBody)))
	ireq.Header.Set("Content-Type", "application/json")
	ires, err := http.DefaultClient.Do(ireq)
	if err != nil {
		t.Fatal(err)
	}
	defer ires.Body.Close()
	if ires.StatusCode != 200 {
		t.Fatalf("import status %d", ires.StatusCode)
	}
	var imp map[string]any
	_ = json.NewDecoder(ires.Body).Decode(&imp)
	wp, _ := imp["workspace_path"].(string)
	if wp == "" || !strings.Contains(wp, "imports/") {
		t.Fatalf("workspace_path %v", imp)
	}
	// file exists on disk
	store := project.NewStore(sci)
	ws, _ := store.WorkspacePath(slug)
	if _, err := os.Stat(filepath.Join(ws, filepath.FromSlash(wp))); err != nil {
		t.Fatal(err)
	}
	_ = slug
}

func TestSessionExportMarkdown(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Export Sess")
	store := project.NewStore(sci)
	sess, err := store.CreateSession(slug, "export-me")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.AppendTurns(slug, sess.ID,
		project.Turn{Role: "user", Text: "hello export"},
		project.Turn{Role: "assistant", Text: "world **md**"},
	)
	res, err := http.Get(ts.URL + "/api/lab/projects/" + slug + "/sessions/" + sess.ID + "/export?format=md")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	body := buf.String()
	if !strings.Contains(body, "hello export") || !strings.Contains(body, "export-me") {
		t.Fatalf("md body %q", body)
	}
	jres, err := http.Get(ts.URL + "/api/lab/projects/" + slug + "/sessions/" + sess.ID + "/export?format=json")
	if err != nil {
		t.Fatal(err)
	}
	defer jres.Body.Close()
	var sessOut project.Session
	if err := json.NewDecoder(jres.Body).Decode(&sessOut); err != nil {
		t.Fatal(err)
	}
	if len(sessOut.Turns) != 2 {
		t.Fatalf("turns %d", len(sessOut.Turns))
	}
}

func TestComputeImportAll(t *testing.T) {
	ts, _ := testLabServer(t)
	slug := createProject(t, ts, "Import All")
	body := `{"host":"local","command":"echo a > a.txt && echo b > b.txt","timeout_sec":10,"output_globs":["*.txt"]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/compute/jobs?project_id="+slug, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var job map[string]any
	_ = json.NewDecoder(res.Body).Decode(&job)
	res.Body.Close()
	jobID, _ := job["id"].(string)
	var last map[string]any
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		gr, _ := http.Get(ts.URL + "/api/lab/compute/jobs/" + jobID + "?project_id=" + slug)
		_ = json.NewDecoder(gr.Body).Decode(&last)
		gr.Body.Close()
		if last["status"] == "done" || last["status"] == "failed" {
			break
		}
	}
	if last["status"] != "done" {
		t.Fatalf("%v", last)
	}
	ireq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/compute/jobs/"+jobID+"/import?project_id="+slug,
		bytes.NewReader([]byte(`{"all":true}`)))
	ireq.Header.Set("Content-Type", "application/json")
	ires, err := http.DefaultClient.Do(ireq)
	if err != nil {
		t.Fatal(err)
	}
	defer ires.Body.Close()
	var imp map[string]any
	_ = json.NewDecoder(ires.Body).Decode(&imp)
	if imp["count"].(float64) < 1 {
		t.Fatalf("import all %v", imp)
	}
}

func TestWorkspaceZipImportExport(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Zip IO")
	store := project.NewStore(sci)
	ws, _ := store.WorkspacePath(slug)
	_ = os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("hi"), 0o600)

	// export
	res, err := http.Get(ts.URL + "/api/lab/files/export?project_id=" + slug)
	if err != nil {
		t.Fatal(err)
	}
	zipData, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || len(zipData) < 20 {
		t.Fatalf("export status=%d len=%d", res.StatusCode, len(zipData))
	}
	if zipData[0] != 'P' || zipData[1] != 'K' {
		t.Fatal("not a zip")
	}

	// import back
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "pack.zip")
	_, _ = part.Write(zipData)
	_ = w.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/files/import?project_id="+slug, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	ires, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer ires.Body.Close()
	if ires.StatusCode != 200 {
		t.Fatalf("import %d", ires.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(ires.Body).Decode(&body)
	if body["count"].(float64) < 1 {
		t.Fatalf("%v", body)
	}
}

func TestComputeCancelAPI(t *testing.T) {
	ts, _ := testLabServer(t)
	slug := createProject(t, ts, "Cancel Job")
	body := `{"host":"local","command":"sleep 60","timeout_sec":120}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/compute/jobs?project_id="+slug, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var job map[string]any
	_ = json.NewDecoder(res.Body).Decode(&job)
	res.Body.Close()
	id, _ := job["id"].(string)
	time.Sleep(80 * time.Millisecond)
	creq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/compute/jobs/"+id+"/cancel?project_id="+slug, bytes.NewReader([]byte("{}")))
	cres, err := http.DefaultClient.Do(creq)
	if err != nil {
		t.Fatal(err)
	}
	defer cres.Body.Close()
	if cres.StatusCode != 200 {
		t.Fatalf("cancel %d", cres.StatusCode)
	}
}

func TestHostsRegistryAPI(t *testing.T) {
	ts, _ := testLabServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/lab/compute/ssh-hosts",
		bytes.NewReader([]byte(`{"alias":"box1","hostname":"1.2.3.4","user":"u","notes":"n"}`)))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("post %d", res.StatusCode)
	}
	get, err := http.Get(ts.URL + "/api/lab/compute/ssh-hosts")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.NewDecoder(get.Body).Decode(&body)
	get.Body.Close()
	hosts, _ := body["hosts"].([]any)
	found := false
	for _, h := range hosts {
		m, _ := h.(map[string]any)
		if m["alias"] == "box1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("hosts %v", body)
	}
}

func TestFileTreeAPI(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Tree")
	store := project.NewStore(sci)
	ws, _ := store.WorkspacePath(slug)
	_ = os.MkdirAll(filepath.Join(ws, "sub"), 0o700)
	_ = os.WriteFile(filepath.Join(ws, "sub", "a.md"), []byte("x"), 0o600)
	res, err := http.Get(ts.URL + "/api/lab/files/tree?project_id=" + slug + "&depth=3")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body["tree"] == nil {
		t.Fatalf("%v", body)
	}
}

func TestDeleteSessionAPI(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Del Sess")
	store := project.NewStore(sci)
	sess, err := store.CreateSession(slug, "to-delete")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/lab/projects/"+slug+"/sessions/"+sess.ID, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	_, err = store.GetSession(slug, sess.ID)
	if err == nil {
		t.Fatal("expected deleted")
	}
}

func TestFileRecentAPI(t *testing.T) {
	ts, sci := testLabServer(t)
	slug := createProject(t, ts, "Recent")
	store := project.NewStore(sci)
	ws, _ := store.WorkspacePath(slug)
	_ = os.WriteFile(filepath.Join(ws, "fresh.md"), []byte("# fresh\n"), 0o600)
	res, err := http.Get(ts.URL + "/api/lab/files/recent?project_id=" + slug + "&limit=10")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body["count"].(float64) < 1 {
		t.Fatalf("%v", body)
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
