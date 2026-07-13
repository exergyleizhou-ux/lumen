package lumenstore

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestRunAndEventPersistence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	run := RunRecord{
		ID: "run_1", SessionID: "session_1", Profile: "code", Title: "fix parser",
		Status: "running", CreatedAt: "2026-07-12T00:00:00Z", UpdatedAt: "2026-07-12T00:00:00Z",
		StartedAt: "2026-07-12T00:00:00Z", Version: 1,
	}
	if err := store.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != run.ID || got.Status != "running" || got.Version != 1 {
		t.Fatalf("unexpected stored run: %#v", got)
	}

	next := run
	next.Status = "succeeded"
	next.StopReason = "finished"
	next.FinishedAt = "2026-07-12T00:01:00Z"
	next.UpdatedAt = next.FinishedAt
	next.Version = 2
	if err := store.UpdateRun(next, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRun(next, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}

	events := []RunEventRecord{
		{RunID: run.ID, Seq: 1, EventID: "evt_1", Kind: "turn_started", CreatedAt: "2026-07-12T00:00:00Z", Payload: `{"seq":1}`},
		{RunID: run.ID, Seq: 2, EventID: "evt_2", Kind: "turn_done", CreatedAt: "2026-07-12T00:01:00Z", Payload: `{"seq":2}`},
	}
	for _, ev := range events {
		if err := store.AppendRunEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := store.LoadRunEvents(run.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Seq != 2 || loaded[0].Payload != `{"seq":2}` {
		t.Fatalf("unexpected replay: %#v", loaded)
	}
	duplicate := events[0]
	duplicate.Payload = `{"seq":1,"overwritten":true}`
	if err := store.AppendRunEvent(duplicate); !errors.Is(err, ErrEventConflict) {
		t.Fatalf("expected event conflict, got %v", err)
	}
	loaded, err = store.LoadRunEvents(run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].Payload != `{"seq":1}` {
		t.Fatalf("duplicate overwrote event: %#v", loaded)
	}
}

func TestCreateRunRejectsDuplicateID(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunRecord{ID: "run_dup", Profile: "code", Title: "x", Status: "running", CreatedAt: "now", UpdatedAt: "now", Version: 1}
	if err := store.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRun(run); !errors.Is(err, ErrRunConflict) {
		t.Fatalf("expected run conflict, got %v", err)
	}
}
