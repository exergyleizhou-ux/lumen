package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"lumen/internal/control"
)

func TestEmbeddedAppJSSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	cmd := exec.Command(node, "--check", "static/assets/app.js")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("embedded app.js must parse: %v\n%s", err, out)
	}
}

func TestEmbeddedAppJSWorkbenchSnapshotV2(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	cmd := exec.Command(node, "static/assets/codeui_test.mjs")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Code WorkbenchSnapshotV2 contract: %v\n%s", err, out)
	}
}

func TestWorkbenchParentOriginIsExact(t *testing.T) {
	if got := cfgWorkbenchOrigin("https://oasis.example/"); got != "https://oasis.example" {
		t.Fatalf("got %q", got)
	}
	for _, bad := range []string{"*", "javascript:alert(1)", "https://oasis.example/path", "https://oasis.example?q=x"} {
		if got := cfgWorkbenchOrigin(bad); got != "" {
			t.Fatalf("accepted %q as %q", bad, got)
		}
	}
}

func TestIndexInjectsWorkbenchParentOrigin(t *testing.T) {
	s, err := New(Config{Ctrl: control.New(), WorkbenchOrigin: "https://oasis.example"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `window.__LUMEN_WORKBENCH_ORIGIN__="https://oasis.example"`) {
		t.Fatalf("index injection: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "https://oasis.example") {
		t.Fatalf("frame ancestor missing: %s", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestEmbeddedAppJSRunReplayContract(t *testing.T) {
	data, err := os.ReadFile("static/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"/v1/runs/", "run_id", "after=", "currentRunSeq", "修改未通过工程验证",
		"/cancel", "restoreStoredRun", `sessionStorage.getItem("lumen_active_run")`, "setTimeout(resolve, 1000)",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("app.js missing run replay contract %q", required)
		}
	}
}
