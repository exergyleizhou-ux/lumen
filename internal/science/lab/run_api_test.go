package lab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lumen/internal/event"
	"lumen/internal/runstate"
)

func TestLabRunAPIGetReplayAndCancel(t *testing.T) {
	runs := runstate.NewManager(nil)
	api := NewAPI(t.TempDir(), "test", nil, 0, runs)
	mux := http.NewServeMux()
	api.Register(mux)
	run, err := runs.Start("science-session", "science", "experiment", "")
	if err != nil {
		t.Fatal(err)
	}
	sink := runs.WrapSink(run.ID, event.Discard)
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.Text, Text: "result"})
	sink.Emit(event.Event{Kind: event.TurnDone, StopReason: "finished"})
	runCtx, cleanup := api.beginActiveRun(context.Background(), run.ID, time.Minute)
	defer cleanup()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lab/runs/"+run.ID, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"profile":"science"`) {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lab/runs/"+run.ID+"/events?after=2", nil))
	var replay struct {
		Events []event.Event `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&replay); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(replay.Events) != 1 || replay.Events[0].Seq != 3 {
		t.Fatalf("replay status=%d events=%#v", rec.Code, replay.Events)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/lab/runs/"+run.ID+"/cancel", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop active Lab Run")
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lab/runs/run_missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", rec.Code, rec.Body.String())
	}
}
