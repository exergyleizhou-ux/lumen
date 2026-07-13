package lab

import (
	"archive/zip"
	"bytes"
	"lumen/internal/artifact"
	"lumen/internal/runstate"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLabEvidenceAndArtifactsAPI(t *testing.T) {
	runs := runstate.NewManager(nil)
	a := NewAPI(t.TempDir(), "test", nil, 0, runs)
	r, _ := runs.Start("", "lab", "", "")
	a.artifactStore.(*artifact.MemoryStore).Put(artifact.Record{ID: "a", RunID: r.ID, Owner: runstate.LocalOwner, Name: "result.csv", ObjectKey: "internal/key"}, []byte("x\n1\n"))
	for _, suffix := range []string{"artifacts", "evidence"} {
		req := httptest.NewRequest(http.MethodGet, "/api/lab/runs/"+r.ID+"/"+suffix, nil)
		rec := httptest.NewRecorder()
		a.handleRuns(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", suffix, rec.Code, rec.Body.String())
		}
		if suffix == "evidence" {
			if _, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len())); err != nil {
				t.Fatal(err)
			}
		}
	}
}
