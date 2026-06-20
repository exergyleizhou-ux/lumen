package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRecordAndQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s := NewStore(path)
	defer s.Close()

	s.RecordToolCall(ToolCall{Tool: "bash", Why: "run the tests", Args: `{"command":"go test ./..."}`, Result: "ok", OK: true})
	s.RecordToolCall(ToolCall{Tool: "edit_file", Why: "fix the bug", Args: `{"path":"a.go"}`, Result: "boom", OK: false})

	got := s.Query("", "", "", time.Time{}, time.Time{})
	if len(got) != 2 {
		t.Fatalf("Query returned %d entries, want 2", len(got))
	}

	// The "why" / args / result must be recorded in the details so the trail can
	// answer "why did the agent do X".
	e := got[0]
	if e.Action != "bash" {
		t.Errorf("Action = %q, want bash", e.Action)
	}
	if e.Result != "success" {
		t.Errorf("Result = %q, want success", e.Result)
	}
	if e.Details["why"] != "run the tests" {
		t.Errorf("details[why] = %v, want 'run the tests'", e.Details["why"])
	}
	if e.Details["args"] == nil || e.Details["result"] == nil {
		t.Error("details must carry args and result")
	}
	if got[1].Result != "failure" {
		t.Errorf("failed call Result = %q, want failure", got[1].Result)
	}

	// JSONL file must have exactly one line per record.
	if n := countLines(t, path); n != 2 {
		t.Errorf("JSONL has %d lines, want 2", n)
	}
}

func TestStorePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s1 := NewStore(path)
	s1.RecordToolCall(ToolCall{Tool: "bash", Why: "a", Args: "{}", Result: "r", OK: true})
	s1.RecordToolCall(ToolCall{Tool: "grep", Why: "b", Args: "{}", Result: "r", OK: true})
	s1.Close()

	// Reopen: prior entries must load from disk and remain queryable.
	s2 := NewStore(path)
	defer s2.Close()
	got := s2.Query("", "", "", time.Time{}, time.Time{})
	if len(got) != 2 {
		t.Fatalf("after reopen Query returned %d, want 2", len(got))
	}
	// A new record appends after the loaded ones, preserving the hash chain.
	s2.RecordToolCall(ToolCall{Tool: "ls", Why: "c", Args: "{}", Result: "r", OK: true})
	if ok, issues := s2.Verify(); !ok {
		t.Errorf("hash chain broken after reopen+append: %v", issues)
	}
	if n := countLines(t, path); n != 3 {
		t.Errorf("JSONL has %d lines after append, want 3", n)
	}
}

func TestStoreMemoryOnly(t *testing.T) {
	// Empty path → memory-only, no file written, records still queryable.
	s := NewStore("")
	defer s.Close()
	s.RecordToolCall(ToolCall{Tool: "bash", Why: "x", Args: "{}", Result: "r", OK: true})
	if len(s.Query("", "", "", time.Time{}, time.Time{})) != 1 {
		t.Error("memory-only store should still record in-memory")
	}
}

func TestStoreArgTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s := NewStore(path)
	defer s.Close()
	big := make([]byte, maxArgLen+5000)
	for i := range big {
		big[i] = 'x'
	}
	e := s.RecordToolCall(ToolCall{Tool: "bash", Args: string(big), OK: true})
	args, _ := e.Details["args"].(string)
	if len(args) > maxArgLen+64 { // +slack for the truncation marker
		t.Errorf("args not truncated: len=%d", len(args))
	}
}

func TestStoreConfig(t *testing.T) {
	t.Setenv(EnvAudit, "off")
	if _, enabled := storeConfig(); enabled {
		t.Error("LUMEN_AUDIT=off must disable auditing")
	}
	t.Setenv(EnvAudit, "")
	t.Setenv(EnvAuditLog, "/custom/audit.jsonl")
	path, enabled := storeConfig()
	if !enabled {
		t.Error("auditing should be enabled by default")
	}
	if path != "/custom/audit.jsonl" {
		t.Errorf("path = %q, want the LUMEN_AUDIT_LOG override", path)
	}
}

func TestRecordNilSafe(t *testing.T) {
	// A disabled store must make Record a safe no-op.
	var s *Store
	if e := s.RecordToolCall(ToolCall{Tool: "bash"}); e != nil {
		t.Error("nil store RecordToolCall should be a no-op returning nil")
	}
	if got := s.Query("", "", "", time.Time{}, time.Time{}); got != nil {
		t.Error("nil store Query should return nil")
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Errorf("line is not valid JSON: %v", err)
		}
		n++
	}
	return n
}
