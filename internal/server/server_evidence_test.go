package server

import (
	"archive/zip"
	"bytes"
	"lumen/internal/artifact"
	"lumen/internal/control"
	"lumen/internal/runstate"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCodeEvidenceAndArtifactsAPI(t *testing.T) {
	runs := runstate.NewManager(nil)
	arts := artifact.NewMemoryStore()
	s, err := New(Config{Ctrl: control.New(), Runs: runs, Artifacts: arts})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := runs.Start("", "code", "", "")
	arts.Put(artifact.Record{ID: "a", RunID: r.ID, Owner: runstate.LocalOwner, Name: "out.txt", ObjectKey: "internal/key"}, []byte("ok"))
	for _, suffix := range []string{"artifacts", "evidence"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/runs/"+r.ID+"/"+suffix, nil)
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
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
