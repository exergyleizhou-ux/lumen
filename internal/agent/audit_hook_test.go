package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lumen/internal/audit"
	"lumen/internal/provider"
)

// TestMain points the audit trail at a throwaway temp file for the whole agent
// test package. Without this, the per-tool-call audit hook would write to the
// user's real ~/.lumen/audit.jsonl during tests (the default store is enabled
// unless LUMEN_AUDIT=off). The audit Default() store is a sync.Once singleton,
// so the env must be set before any test triggers a tool call.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lumen-agent-audit-")
	if err != nil {
		panic(err)
	}
	os.Setenv(audit.EnvAuditLog, filepath.Join(dir, "audit.jsonl"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// toolThenDoneProvider calls read_test on the first turn, then answers on the
// second — exercising the tool-execution path where audit.Record fires.
type toolThenDoneProvider struct{ calls int }

func (p *toolThenDoneProvider) Name() string { return "tooler" }
func (p *toolThenDoneProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	ch := make(chan provider.Chunk, 4)
	if p.calls == 1 {
		tc := &provider.ToolCall{ID: "c1", Name: "read_test", Arguments: "{}"}
		ch <- provider.Chunk{Type: provider.ChunkToolCallStart, ToolCall: tc}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: tc}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "all done"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}
	close(ch)
	return ch, nil
}

func TestToolCallIsAudited(t *testing.T) {
	a := New(&toolThenDoneProvider{}, testRegistry(), NewSession(""), Options{MaxSteps: 3})
	if err := a.Run(context.Background(), "read the file"); err != nil {
		t.Fatalf("run: %v", err)
	}

	path := os.Getenv(audit.EnvAuditLog)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log %s: %v", path, err)
	}
	if !strings.Contains(string(data), "read_test") {
		t.Errorf("audit log does not mention the executed tool:\n%s", data)
	}
}
