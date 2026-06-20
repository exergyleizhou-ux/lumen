package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/audit"
)

// TestAuditQueryReadsStore: the audit_query tool surfaces the recorded
// tool-call trail (tool, why, args, result) from the persistent store.
func TestAuditQueryReadsStore(t *testing.T) {
	store := audit.NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	t.Cleanup(func() { store.Close() })
	store.RecordToolCall(audit.ToolCall{Tool: "bash", Why: "run the build", Args: `{"command":"go build"}`, Result: "ok", OK: true})
	store.RecordToolCall(audit.ToolCall{Tool: "edit_file", Why: "apply the fix", Args: `{"path":"x.go"}`, Result: "done", OK: true})

	// Swap the tool's store source to the fixture.
	orig := auditStoreFn
	auditStoreFn = func() *audit.Store { return store }
	t.Cleanup(func() { auditStoreFn = orig })

	tool := &AuditQueryTool{}

	// No filter → both entries.
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("audit_query failed: %v", err)
	}
	if !strings.Contains(out, "bash") || !strings.Contains(out, "edit_file") {
		t.Errorf("expected both tool calls in output, got:\n%s", out)
	}

	// Filter by action → only the matching tool.
	out2, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"edit_file"}`))
	if err != nil {
		t.Fatalf("filtered audit_query failed: %v", err)
	}
	if strings.Contains(out2, "bash") || !strings.Contains(out2, "edit_file") {
		t.Errorf("action filter not applied, got:\n%s", out2)
	}
}

// TestAuditQueryDisabledStore: when auditing is disabled (nil store), the tool
// degrades gracefully rather than panicking.
func TestAuditQueryDisabledStore(t *testing.T) {
	orig := auditStoreFn
	auditStoreFn = func() *audit.Store { return nil }
	t.Cleanup(func() { auditStoreFn = orig })

	tool := &AuditQueryTool{}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("audit_query on disabled store errored: %v", err)
	}
	if !strings.Contains(out, "No audit entries") {
		t.Errorf("disabled store should report no entries, got: %q", out)
	}
}
