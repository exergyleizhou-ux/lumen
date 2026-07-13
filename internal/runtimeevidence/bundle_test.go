package runtimeevidence

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"lumen/internal/approvalstate"
	"lumen/internal/artifact"
	"lumen/internal/event"
	"lumen/internal/runstate"
	"lumen/internal/usage"
	"testing"
	"time"
)

func TestBundleContractRedactionAndOwner(t *testing.T) {
	o := runstate.Owner{UserID: "u", WorkspaceID: "w"}
	runs := runstate.NewManager(nil)
	r, err := runs.StartOwned(o, "s", "code", "title", "")
	if err != nil {
		t.Fatal(err)
	}
	sink := runs.WrapSink(r.ID, event.Discard)
	sink.Emit(event.Event{Kind: event.Notice, Text: "ok", Tool: event.Tool{Args: `{"reasoning":"private","api_key":"secret"}`}})
	arts := artifact.NewMemoryStore()
	approvals := approvalstate.NewMemoryStore()
	hash, _ := approvalstate.HashArgs([]byte(`{"command":"private command"}`))
	approvals.Create(approvalstate.Approval{ID: "ap", RunID: r.ID, ToolCallID: "tc", Owner: o, Reason: "private reason", ArgsHash: hash, EditableArgs: []byte(`{"command":"private command"}`), ExpiresAt: time.Now().Add(time.Minute)})
	if err = arts.Put(artifact.Record{ID: "a", RunID: r.ID, Owner: o, Name: "../bad name.txt", ObjectKey: "system/key", MIME: "text/plain", CreatedAt: time.Now()}, []byte("result")); err != nil {
		t.Fatal(err)
	}
	usageStore := usage.NewMemoryStore()
	usageStore.CreateUsage(usage.Record{RunID: r.ID, EventID: "usage", UserID: o.UserID, WorkspaceID: o.WorkspaceID, InputTokens: 11, OutputTokens: 7, CacheHitTokens: 3, CreatedAt: time.Now()})
	svc := Service{Runs: runs, Approvals: approvals, Artifacts: arts, Usage: usageStore}
	b, err := svc.Build(context.Background(), o, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"manifest.json": true, "run.json": true, "events.jsonl": true, "approvals.json": true, "verification.json": true, "provenance.jsonl": true, "usage.json": true, "artifacts/bad_name.txt": true, "SHA256SUMS": true}
	for _, f := range zr.File {
		delete(want, f.Name)
		if f.Name == "events.jsonl" {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			if bytes.Contains(raw, []byte("private")) || bytes.Contains(raw, []byte("secret")) {
				t.Fatal("secret leaked")
			}
		}
		if f.Name == "approvals.json" {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			if bytes.Contains(raw, []byte("private")) {
				t.Fatal("approval secret leaked")
			}
		}
		if f.Name == "usage.json" {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			if !bytes.Contains(raw, []byte(`"input_tokens": 11`)) || !bytes.Contains(raw, []byte(`"output_tokens": 7`)) {
				t.Fatalf("usage counters redacted: %s", raw)
			}
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing %v", want)
	}
	if _, err = svc.Build(context.Background(), runstate.Owner{UserID: "other", WorkspaceID: "w"}, r.ID); err == nil {
		t.Fatal("cross-owner bundle")
	}
}
func TestBundleRejectsOversize(t *testing.T) {
	o := runstate.Owner{UserID: "u", WorkspaceID: "w"}
	runs := runstate.NewManager(nil)
	r, _ := runs.StartOwned(o, "", "code", "", "")
	arts := artifact.NewMemoryStore()
	arts.Put(artifact.Record{ID: "a", RunID: r.ID, Owner: o, Name: "x", ObjectKey: "k"}, []byte("large"))
	_, err := (Service{Runs: runs, Artifacts: arts, MaxBytes: 2}).Build(context.Background(), o, r.ID)
	if err == nil {
		t.Fatal("oversize accepted")
	}
}
func TestBundleHonorsTimeoutAcrossSerialization(t *testing.T) {
	o := runstate.LocalOwner
	runs := runstate.NewManager(nil)
	r, _ := runs.Start("", "code", "", "")
	_, err := (Service{Runs: runs, Timeout: time.Nanosecond}).Build(context.Background(), o, r.ID)
	if err == nil {
		t.Fatal("expired bundle succeeded")
	}
}
