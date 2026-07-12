//go:build integration

package pgstore

import (
	"database/sql"
	"errors"
	_ "github.com/jackc/pgx/v5/stdlib"
	"lumen/internal/event"
	"lumen/internal/runstate"
	"os"
	"testing"
	"time"
)

func TestPostgresCASAndIdempotentEvents(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// Isolated schema mirrors the migration contract and requires no production rows.
	if _, err = db.Exec(`CREATE TEMP TABLE workbench_runs(id text primary key,account_id uuid not null,workspace_id uuid not null,profile text,status text,version bigint,title text,request jsonb,error_message text,created_at timestamptz,started_at timestamptz,finished_at timestamptz,updated_at timestamptz);CREATE TEMP TABLE workbench_events(id text,run_id text,account_id uuid,workspace_id uuid,seq bigint,type text,payload jsonb,created_at timestamptz,primary key(run_id,seq))`); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	now := time.Now().UTC()
	r := runstate.Run{ID: "run_test", UserID: "00000000-0000-0000-0000-000000000001", WorkspaceID: "00000000-0000-0000-0000-000000000002", Profile: "code", Status: runstate.StatusRunning, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err = s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	r.Version = 2
	r.Status = runstate.StatusSucceeded
	if err = s.UpdateRun(r, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateRun(r, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("CAS got %v", err)
	}
	e := event.Event{RunID: r.ID, EventID: "e", Seq: 1, Kind: event.Notice, Timestamp: now}
	if err = s.AppendEvent(e); err != nil {
		t.Fatal(err)
	}
	if err = s.AppendEvent(e); err != nil {
		t.Fatal(err)
	}
	got, err := s.Events(r.ID, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("events=%v err=%v", got, err)
	}
}
