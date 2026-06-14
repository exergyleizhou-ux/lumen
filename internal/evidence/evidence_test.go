package evidence

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRecordAndHasEvidence(t *testing.T) {
	l := NewLedger()

	// Record a complete_step receipt with a specific step
	l.Record(Receipt{
		ToolName: "complete_step",
		Success:  true,
		ReadOnly: false,
		Step:     "step1",
	})

	if !l.HasEvidence("step1") {
		t.Error("HasEvidence should return true when step name matches a successful receipt")
	}
	if l.HasEvidence("step2") {
		t.Error("HasEvidence should return false for unmatched step names")
	}
}

func TestVerifyEvidenceNoWriter(t *testing.T) {
	l := NewLedger()

	// Only read-only tools ran — should reject
	l.Record(Receipt{ToolName: "read_file", Success: true, ReadOnly: true})
	l.Record(Receipt{ToolName: "grep", Success: true, ReadOnly: true})

	ok, msg := l.VerifyEvidence("step1", "done", []EvidenceItem{
		{Kind: "manual", Summary: "checked manually"},
	})
	if ok {
		t.Errorf("should reject when no writer tool ran, got: %s", msg)
	}
}

func TestVerifyEvidenceBashMatch(t *testing.T) {
	l := NewLedger()

	// Record a bash call
	l.Record(Receipt{
		ToolName: "bash",
		Success:  true,
		ReadOnly: false,
		Command:  "go build ./...",
	})

	// Verification evidence that cites that exact command
	ok, msg := l.VerifyEvidence("step1", "built", []EvidenceItem{
		{Kind: "verification", Summary: "build passed", Command: "go build ./..."},
	})
	if !ok {
		t.Errorf("should accept matching bash receipt: %s", msg)
	}
}

func TestVerifyEvidenceBashMismatch(t *testing.T) {
	l := NewLedger()

	l.Record(Receipt{
		ToolName: "bash",
		Success:  true,
		ReadOnly: false,
		Command:  "go build ./...",
	})

	// Cites a different command than what was run
	ok, msg := l.VerifyEvidence("step1", "tested", []EvidenceItem{
		{Kind: "verification", Summary: "test passed", Command: "go test ./..."},
	})
	if ok {
		t.Errorf("should reject mismatched bash command, got: %s", msg)
	}
}

func TestVerifyEvidenceFileMatch(t *testing.T) {
	l := NewLedger()

	l.Record(Receipt{
		ToolName: "write_file",
		Success:  true,
		ReadOnly: false,
		Paths:    []string{"/tmp/test.go"},
	})

	ok, msg := l.VerifyEvidence("step2", "wrote file", []EvidenceItem{
		{Kind: "files", Summary: "wrote test.go", Paths: []string{"/tmp/test.go"}},
	})
	if !ok {
		t.Errorf("should accept matching file receipt: %s", msg)
	}
}

func TestVerifyEvidenceFileMismatch(t *testing.T) {
	l := NewLedger()

	l.Record(Receipt{
		ToolName: "edit_file",
		Success:  true,
		ReadOnly: false,
		Paths:    []string{"foo.go"},
	})

	ok, msg := l.VerifyEvidence("step2", "edited", []EvidenceItem{
		{Kind: "files", Summary: "edited bar", Paths: []string{"bar.go"}},
	})
	if ok {
		t.Errorf("should reject mismatched file path, got: %s", msg)
	}
}

func TestVerifyEvidenceManual(t *testing.T) {
	l := NewLedger()

	// Manual evidence always accepted as long as a writer ran
	l.Record(Receipt{
		ToolName: "write_file",
		Success:  true,
		ReadOnly: false,
		Paths:    []string{"main.go"},
	})

	ok, msg := l.VerifyEvidence("step3", "reviewed", []EvidenceItem{
		{Kind: "manual", Summary: "manually verified"},
	})
	if !ok {
		t.Errorf("should accept manual evidence when a writer ran: %s", msg)
	}
}

func TestVerifyEvidenceUnknownKind(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "write_file", Success: true, ReadOnly: false, Paths: []string{"x.go"}})

	ok, _ := l.VerifyEvidence("step", "done", []EvidenceItem{
		{Kind: "magic", Summary: "magic happened"},
	})
	if ok {
		t.Error("should reject unknown evidence kind")
	}
}

func TestVerifyEvidenceMultipleItems(t *testing.T) {
	l := NewLedger()

	l.Record(Receipt{ToolName: "bash", Success: true, ReadOnly: false, Command: "go build"})
	l.Record(Receipt{ToolName: "write_file", Success: true, ReadOnly: false, Paths: []string{"main.go"}})

	ok, msg := l.VerifyEvidence("step", "done", []EvidenceItem{
		{Kind: "verification", Summary: "build ok", Command: "go build"},
		{Kind: "files", Summary: "wrote main", Paths: []string{"main.go"}},
	})
	if !ok {
		t.Errorf("should accept all matched evidence: %s", msg)
	}
}

func TestReceiptFromToolCallBash(t *testing.T) {
	args := json.RawMessage(`{"command":"echo hello","run_in_background":false}`)
	r := ReceiptFromToolCall("bash", args, true, false)

	if r.Command != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", r.Command)
	}
	if !r.Success {
		t.Error("expected success=true")
	}
}

func TestReceiptFromToolCallWriteFile(t *testing.T) {
	args := json.RawMessage(`{"path":"/tmp/x.go","content":"package main"}`)
	r := ReceiptFromToolCall("write_file", args, true, false)

	if len(r.Paths) != 1 || r.Paths[0] != "/tmp/x.go" {
		t.Errorf("expected paths [/tmp/x.go], got %v", r.Paths)
	}
}

func TestReceiptFromToolCallTodoWrite(t *testing.T) {
	args := json.RawMessage(`{"todos":[{"content":"add tests","status":"in_progress","activeForm":"adding tests","level":0}]}`)
	r := ReceiptFromToolCall("todo_write", args, true, false)

	if len(r.Todos) != 1 || r.Todos[0].Content != "add tests" {
		t.Errorf("expected todo 'add tests', got %v", r.Todos)
	}
}

func TestContextPropagation(t *testing.T) {
	l := NewLedger()
	ctx := WithLedger(context.Background(), l)

	got := FromContext(ctx)
	if got != l {
		t.Error("FromContext should return the same ledger")
	}

	// No ledger in ctx
	empty := FromContext(context.Background())
	if empty != nil {
		t.Error("FromContext should return nil when no ledger is set")
	}
}

func TestLedgerConcurrency(t *testing.T) {
	l := NewLedger()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			l.Record(Receipt{ToolName: "bash", Success: true, ReadOnly: false, Command: "cmd"})
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			l.Record(Receipt{ToolName: "read_file", Success: true, ReadOnly: true})
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	if len(l.Receipts()) != 200 {
		t.Errorf("expected 200 receipts after concurrent writes, got %d", len(l.Receipts()))
	}
}
