//go:build integration

package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	_ "github.com/jackc/pgx/v5/stdlib"
	"io"
	"lumen/internal/approvalstate"
	"lumen/internal/artifact"
	"lumen/internal/event"
	"lumen/internal/runstate"
	"lumen/internal/usage"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActualOasisMigrationFullContract(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	migration := os.Getenv("OASIS_000036_MIGRATION")
	if migration == "" {
		migration = "/Users/lei/Documents/Codex/2026-07-12/new-chat-2/work/oasis-shared-runtime/backend/migrations/000036_workbench_runtime.up.sql"
	}
	raw, err := os.ReadFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "estimated_cost bigint") {
		t.Fatal("test is not using current Oasis 000036 migration")
	}
	raw37, err := os.ReadFile("/Users/lei/Documents/Codex/2026-07-12/new-chat-2/work/oasis-shared-runtime/backend/migrations/000037_workbench_runtime_execution.up.sql")
	if err != nil || !strings.Contains(string(raw37), "execution_state") || !strings.Contains(string(raw37), "ADD COLUMN step_id text") {
		t.Fatal("current Oasis 000037 execution migration required")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	uid := "10000000-0000-4000-8000-" + time.Now().Format("150405.000000")
	uid = strings.ReplaceAll(uid, ".", "")
	uid = uid[:36]
	wid := "20000000-0000-4000-8000-" + uid[24:]
	_, err = db.Exec(`INSERT INTO users(id,account,account_type,password_hash)VALUES($1,$2,'email','x')`, uid, "lumen-"+uid+"@test.invalid")
	if err == nil {
		_, err = db.Exec(`INSERT INTO workbench_workspaces(id,account_id,slug)VALUES($1,$2,$3)`, wid, uid, "lumen-"+uid[24:])
	}
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM users WHERE id=$1`, uid)
	s := New(db)
	now := time.Now().UTC()
	r := runstate.Run{ID: "run_" + uid, UserID: uid, WorkspaceID: wid, Profile: "code", Status: runstate.StatusRunning, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err = s.CreateRun(r); err != nil {
		t.Fatal(err)
	}
	r.Version = 2
	r.Status = runstate.StatusSucceeded
	if err = s.UpdateRun(r, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateRun(r, 1); !errors.Is(err, runstate.ErrVersionConflict) {
		t.Fatalf("CAS=%v", err)
	}
	e := event.Event{RunID: r.ID, EventID: "e", Seq: 1, Kind: event.Notice, Timestamp: now}
	if err = s.AppendEvent(e); err != nil {
		t.Fatal(err)
	}
	if err = s.AppendEvent(e); err != nil {
		t.Fatal(err)
	}
	h, _ := approvalstate.HashArgs(json.RawMessage(`{"x":1}`))
	aps := approvalstate.PostgresStore{DB: db}
	if err = aps.Create(approvalstate.Approval{ID: "ap_" + uid, RunID: r.ID, StepID: "step", ToolCallID: "tc", Owner: runstate.Owner{UserID: uid, WorkspaceID: wid}, RiskLevel: "high", Reason: "test", ArgsHash: h, EditableArgs: json.RawMessage(`{"x":1}`), EstimatedCostMicros: 7, CreatedAt: now, ExpiresAt: now.Add(time.Minute), Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = aps.Decide(runstate.Owner{UserID: uid, WorkspaceID: wid}, "ap_"+uid, approvalstate.DecisionApproved, uid, now); err != nil {
		t.Fatal(err)
	}
	if _, err = aps.Consume(runstate.Owner{UserID: uid, WorkspaceID: wid}, "ap_"+uid, "exec_"+uid, now); err != nil {
		t.Fatal(err)
	}
	if _, err = aps.Consume(runstate.Owner{UserID: uid, WorkspaceID: wid}, "ap_"+uid, "exec2_"+uid, now); !errors.Is(err, approvalstate.ErrNotExecutable) {
		t.Fatalf("consume replay=%v", err)
	}
	if err = aps.Complete(runstate.Owner{UserID: uid, WorkspaceID: wid}, "ap_"+uid, "exec_"+uid, true, now); err != nil {
		t.Fatal(err)
	}
	// Real connection-loss fault: consume the dangerous operation durably,
	// then lose the event-store connection before its result can be appended.
	// Restart must see the consumed grant and must not report durable success.
	outageDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	outageStore := New(outageDB)
	outageRuns := runstate.NewManager(outageStore)
	owner := runstate.Owner{UserID: uid, WorkspaceID: wid}
	faultRun, err := outageRuns.StartOwned(owner, "fault-session", "code", "fault", "")
	if err != nil {
		t.Fatal(err)
	}
	faultApprovalID := "fault_ap_" + uid
	if err = aps.Create(approvalstate.Approval{ID: faultApprovalID, RunID: faultRun.ID, ToolCallID: "dangerous", Owner: owner, RiskLevel: "high", ArgsHash: h, EditableArgs: json.RawMessage(`{"x":1}`), CreatedAt: now, ExpiresAt: now.Add(time.Minute), Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = aps.Decide(owner, faultApprovalID, approvalstate.DecisionApproved, uid, now); err != nil {
		t.Fatal(err)
	}
	if _, err = aps.Consume(owner, faultApprovalID, "fault-exec", now); err != nil {
		t.Fatal(err)
	}
	if err = outageDB.Close(); err != nil {
		t.Fatal(err)
	}
	outageRuns.WrapSink(faultRun.ID, event.Discard).Emit(event.Event{Kind: event.ToolResult, ToolCallID: "dangerous"})
	if _, err = outageRuns.Finish(faultRun.ID, nil); err == nil {
		t.Fatal("connection loss produced a successful terminal update")
	}
	restartedApproval, err := aps.Get(owner, faultApprovalID)
	if err != nil || restartedApproval.ExecutionState != "consumed" {
		t.Fatalf("restart lost consumed execution: %#v %v", restartedApproval, err)
	}
	if _, err = aps.Consume(owner, faultApprovalID, "fault-exec-retry", now); !errors.Is(err, approvalstate.ErrNotExecutable) {
		t.Fatalf("restart repeated dangerous execution: %v", err)
	}
	durableFaultRun, err := s.GetRun(faultRun.ID)
	if err != nil || durableFaultRun.Status != runstate.StatusRunning {
		t.Fatalf("outage persisted fake success: %#v %v", durableFaultRun, err)
	}
	us := usage.PostgresStore{DB: db}
	if err = us.CreateUsage(usage.Record{RunID: r.ID, EventID: "usage", UserID: uid, WorkspaceID: wid, Provider: "p", Model: "m", InputTokens: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err = us.CreateUsage(usage.Record{RunID: r.ID, EventID: "usage", UserID: uid, WorkspaceID: wid, Provider: "p", Model: "m", CreatedAt: now}); !errors.Is(err, usage.ErrDuplicate) {
		t.Fatalf("usage replay=%v", err)
	}
	backend, _ := artifact.NewLocalBackend(filepath.Join(t.TempDir(), "objects"))
	as := artifact.PostgresStore{DB: db, Objects: backend}
	data := []byte("result")
	ar, _ := artifact.NewRecord(runstate.Owner{UserID: uid, WorkspaceID: wid}, r.ID, "result.txt", "text/plain", data)
	ar.StepID = "step-art"
	ar.ToolCallID = "tc-art"
	if err = artifact.Persist(context.Background(), as, backend, ar, data); err != nil {
		t.Fatal(err)
	}
	listed, err := as.ListRun(ar.Owner, r.ID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("artifacts=%v %v", listed, err)
	}
	rc, err := as.Open(context.Background(), ar.Owner, listed[0])
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
	changed := ar
	changed.SHA256 = strings.Repeat("f", 64)
	changed.Size = 7
	if err = as.Persist(context.Background(), changed, []byte("changed")); !errors.Is(err, artifact.ErrConflict) {
		t.Fatalf("changed identity=%v", err)
	}
	rc, err = as.Open(context.Background(), ar.Owner, listed[0])
	if err != nil {
		t.Fatal(err)
	}
	preserved, _ := io.ReadAll(rc)
	rc.Close()
	if string(preserved) != "result" {
		t.Fatalf("preexisting object overwritten: %q", preserved)
	}
	uniqueConflict := ar
	uniqueConflict.ID = "different_" + uid
	uniqueConflict.ObjectKey += "-different"
	if err = as.Persist(context.Background(), uniqueConflict, data); !errors.Is(err, artifact.ErrConflict) {
		t.Fatalf("run/tool conflict=%v", err)
	}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- as.Persist(context.Background(), ar, data) }()
	}
	for i := 0; i < 2; i++ {
		if e := <-errs; e != nil {
			t.Fatalf("concurrent replay=%v", e)
		}
	}
}
