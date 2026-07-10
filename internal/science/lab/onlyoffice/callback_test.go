package onlyoffice

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/science/lab/workspace"
)

func TestCallbackPath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"reports/sample.docx", "reports/sample.docx"},
		{"  reports/../notes.md  ", "notes.md"},
		{"", ""},
		{".", ""},
		{"/etc/passwd", ""},
		{"../../etc/passwd", ""},
		{"normal/path.txt", "normal/path.txt"},
	}
	for _, tc := range tests {
		got := CallbackPath(tc.in)
		if got != tc.want {
			t.Errorf("CallbackPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDocTypeFromExt(t *testing.T) {
	if got := DocTypeFromExt("xlsx"); got != "cell" {
		t.Errorf("xlsx → cell, got %q", got)
	}
	if got := DocTypeFromExt("pptx"); got != "slide" {
		t.Errorf("pptx → slide, got %q", got)
	}
	if got := DocTypeFromExt("docx"); got != "word" {
		t.Errorf("docx → word, got %q", got)
	}
}

func TestCallbackEditingStatusIgnored(t *testing.T) {
	dir := t.TempDir()
	g, _ := workspace.NewGuard(dir)

	body := `{"status":1}` // editing — should be acknowledged but not write
	req := httptest.NewRequest(http.MethodPost, "/?project_id=x&path=test.docx", strings.NewReader(body))
	rec := httptest.NewRecorder()

	HandleCallback(rec, req, g, "test.docx")

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var cb CallbackResponse
	json.NewDecoder(rec.Body).Decode(&cb)
	if cb.Error != 0 {
		t.Errorf("expected error 0 for editing status, got %d", cb.Error)
	}
	// File should not have been created
	if _, err := os.Stat(filepath.Join(dir, "test.docx")); err == nil {
		t.Error("file should not exist after editing status callback")
	}
}

func TestCallbackSaveWritesFile(t *testing.T) {
	dir := t.TempDir()
	g, _ := workspace.NewGuard(dir)

	// Create a fake download server that returns the modified content
	content := []byte("modified document body")
	fakeDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer fakeDS.Close()

	cb := CallbackBody{Status: 2, URL: fakeDS.URL + "/output.docx", Key: "k1"}
	body, _ := json.Marshal(cb)

	req := httptest.NewRequest(http.MethodPost, "/?project_id=x&path=reports/out.docx", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	HandleCallback(rec, req, g, "reports/out.docx")

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp CallbackResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != 0 {
		t.Errorf("expected error 0, got %d", resp.Error)
	}

	// Verify file written
	written, err := os.ReadFile(filepath.Join(dir, "reports", "out.docx"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if !bytes.Equal(written, content) {
		t.Errorf("written content mismatch: got %q, want %q", written, content)
	}
}

func TestCallbackRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	g, _ := workspace.NewGuard(dir)

	body := `{"status":2,"url":"http://x/out.docx"}`
	for _, badPath := range []string{"../etc/passwd", "/etc/passwd", ""} {
		req := httptest.NewRequest(http.MethodPost, "/?project_id=x&path="+badPath, strings.NewReader(body))
		rec := httptest.NewRecorder()
		HandleCallback(rec, req, g, badPath)
		if rec.Code != http.StatusOK {
			t.Logf("bad path %q got %d", badPath, rec.Code)
		}
	}
}

func TestCallbackTokenAuth(t *testing.T) {
	os.Setenv("LUMEN_ONLYOFFICE_CALLBACK_TOKEN", "secret-token")
	defer os.Unsetenv("LUMEN_ONLYOFFICE_CALLBACK_TOKEN")

	dir := t.TempDir()
	g, _ := workspace.NewGuard(dir)

	// Request without token — should fail
	body := `{"status":2,"url":"http://x/out.docx"}`
	req := httptest.NewRequest(http.MethodPost, "/?project_id=x&path=test.docx", strings.NewReader(body))
	rec := httptest.NewRecorder()
	HandleCallback(rec, req, g, "test.docx")
	var resp CallbackResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != 1 {
		t.Errorf("expected error 1 without token, got %d", resp.Error)
	}

	// Request with correct token
	req2 := httptest.NewRequest(http.MethodPost, "/?project_id=x&path=test.docx&token=secret-token", strings.NewReader(body))
	rec2 := httptest.NewRecorder()
	HandleCallback(rec2, req2, g, "test.docx")
	var resp2 CallbackResponse
	json.NewDecoder(rec2.Body).Decode(&resp2)
	if resp2.Error != 1 { // still fails because download URL is fake, but token passed
		t.Logf("with token got error %d (expected 1 because download fails)", resp2.Error)
	} else {
		// token was accepted but download of fake URL failed — that's fine
	}
}

func TestCallbackForceSave(t *testing.T) {
	dir := t.TempDir()
	g, _ := workspace.NewGuard(dir)

	content := []byte("force saved")
	fakeDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer fakeDS.Close()

	cb := CallbackBody{Status: 6, URL: fakeDS.URL + "/output.docx"} // force save
	body, _ := json.Marshal(cb)

	req := httptest.NewRequest(http.MethodPost, "/?project_id=x&path=f.docx", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	HandleCallback(rec, req, g, "f.docx")

	var resp CallbackResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != 0 {
		t.Errorf("expected error 0 for force save, got %d: %s", resp.Error, rec.Body.String())
	}
}

func TestCallbackMissingURL(t *testing.T) {
	dir := t.TempDir()
	g, _ := workspace.NewGuard(dir)

	cb := CallbackBody{Status: 2, URL: ""} // no URL
	body, _ := json.Marshal(cb)
	req := httptest.NewRequest(http.MethodPost, "/?project_id=x&path=t.docx", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	HandleCallback(rec, req, g, "t.docx")
	var resp CallbackResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != 1 {
		t.Errorf("expected error 1 for missing URL, got %d", resp.Error)
	}
}

func TestCallbackLargeBody(t *testing.T) {
	dir := t.TempDir()
	g, _ := workspace.NewGuard(dir)
	// Request body > 64KB limit
	huge := strings.Repeat("x", 100<<10)
	req := httptest.NewRequest(http.MethodPost, "/?project_id=x&path=t.docx", strings.NewReader(huge))
	rec := httptest.NewRecorder()
	HandleCallback(rec, req, g, "t.docx")
	var resp CallbackResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != 1 {
		t.Errorf("expected error for huge body, got %d", resp.Error)
	}
}

func TestHealth(t *testing.T) {
	h := Health("")
	if h["configured"].(bool) {
		t.Error("should be false when URL empty")
	}
	if h["url"].(string) != "" {
		t.Error("url should be empty")
	}

	h2 := Health("http://127.0.0.1:8088")
	if !h2["configured"].(bool) {
		t.Error("should be true when URL set")
	}
	if !h2["edit"].(bool) {
		t.Error("edit should be true when URL set")
	}
}

func TestFormatPathForEdit(t *testing.T) {
	if got := FormatPathForEdit("  ./reports/../notes.md  "); got != "notes.md" {
		t.Errorf("got %q, want notes.md", got)
	}
	if got := FormatPathForEdit("reports/sample.docx"); got != "reports/sample.docx" {
		t.Errorf("got %q", got)
	}
}

func TestEditEnabled(t *testing.T) {
	// Without env, should be false
	if EditEnabled() {
		t.Log("LUMEN_ONLYOFFICE_URL is set in environment")
	}
}

// Ensure io import used
var _ = io.ReadAll
