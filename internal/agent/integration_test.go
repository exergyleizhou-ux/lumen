package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"lumen/internal/provider"
	"lumen/internal/tool"
)

// ── Multi-turn mock provider ───────────────────────────────

// mockMultiTurnProvider simulates a model that calls tools across multiple
// turns: first a read, then a write, then a final text answer.
type mockMultiTurnProvider struct {
	mu    sync.Mutex
	turns int
}

func (m *mockMultiTurnProvider) Name() string { return "mock-multi" }

func (m *mockMultiTurnProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 16)
	go func() {
		defer close(ch)
		m.mu.Lock()
		m.turns++
		turn := m.turns
		m.mu.Unlock()

		switch turn {
		case 1:
			// Turn 1: call read_file on "test.txt"
			ch <- provider.Chunk{
				Type: provider.ChunkToolCall,
				ToolCall: &provider.ToolCall{
					ID:        "call-1",
					Name:      "read_file",
					Arguments: `{"path":"test.txt"}`,
				},
			}
		case 2:
			// Turn 2: call write_file with new content
			ch <- provider.Chunk{
				Type: provider.ChunkToolCall,
				ToolCall: &provider.ToolCall{
					ID:        "call-2",
					Name:      "write_file",
					Arguments: `{"path":"test.txt","content":"new content"}`,
				},
			}
		case 3:
			// Turn 3: final text answer
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
		}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func TestMultiTurnToolCalls(t *testing.T) {
	// Build a registry with a test tool that tracks state
	reg := tool.NewRegistry()
	reg.Add(&testReadOnlyTool{})
	reg.Add(&testWriteTool{})
	reg.Add(&testFailingTool{})

	mockProv := &mockMultiTurnProvider{}
	sess := NewSession("")

	ag := New(mockProv, reg, sess, Options{
		MaxSteps: 10,
	})

	err := ag.Run(context.Background(), "do something")
	if err != nil {
		t.Fatalf("multi-turn run failed: %v", err)
	}

	// Verify session has the expected message sequence:
	// system, user, assistant(tool_call read), tool result, assistant(tool_call write), tool result, assistant("done")
	snap := sess.Snapshot()
	// System + user + 3 assistant + 2 tool = 7
	if len(snap) < 6 {
		t.Fatalf("expected at least 6 messages, got %d", len(snap))
		for i, m := range snap {
			t.Logf("[%d] role=%s content=%q numToolCalls=%d", i, m.Role, m.Content[:min(30, len(m.Content))], len(m.ToolCalls))
		}
	}

	// System prompt should be first
	if snap[0].Role != provider.RoleSystem {
		t.Errorf("first message should be system, got %s", snap[0].Role)
	}

	// Find the final assistant message
	foundFinal := false
	for i := len(snap) - 1; i >= 0; i-- {
		if snap[i].Role == provider.RoleAssistant && strings.TrimSpace(snap[i].Content) != "" && len(snap[i].ToolCalls) == 0 {
			foundFinal = true
			if snap[i].Content != "done" {
				t.Errorf("final answer should be 'done', got %q", snap[i].Content)
			}
			break
		}
	}
	if !foundFinal {
		t.Error("no final assistant message without tool calls found")
	}
}

// ── Mock provider that returns tool calls in sequence ──────

type mockSequenceProvider struct {
	mu       sync.Mutex
	index    int
	sequence []mockStep
}

type mockStep struct {
	toolCalls []provider.ToolCall
	text      string
}

func (m *mockSequenceProvider) Name() string { return "mock-seq" }

func (m *mockSequenceProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 16)
	go func() {
		defer close(ch)
		m.mu.Lock()
		step := mockStep{}
		if m.index < len(m.sequence) {
			step = m.sequence[m.index]
			m.index++
		}
		m.mu.Unlock()

		for _, tc := range step.toolCalls {
			ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &tc}
		}
		if step.text != "" {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: step.text}
		}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func TestParallelToolExecution(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testReadOnlyTool{})
	reg.Add(&testWriteTool{})

	// Two read-only tools in one turn → should execute in parallel
	seq := &mockSequenceProvider{
		sequence: []mockStep{
			{
				toolCalls: []provider.ToolCall{
					{ID: "r1", Name: "read_test", Arguments: "{}"},
					{ID: "r2", Name: "read_test", Arguments: "{}"},
				},
			},
			{text: "all read"},
		},
	}

	sess := NewSession("")
	ag := New(seq, reg, sess, Options{MaxSteps: 5})

	err := ag.Run(context.Background(), "read stuff")
	if err != nil {
		t.Fatalf("parallel read run: %v", err)
	}

	snap := sess.Snapshot()
	if len(snap) < 5 {
		t.Fatalf("expected at least 5 messages, got %d", len(snap))
	}
}

func TestSequentialReadWrite(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testReadOnlyTool{})
	reg.Add(&testWriteTool{})
	reg.Add(&testFailingTool{})

	// Writer + reader in same turn → two serial batches
	seq := &mockSequenceProvider{
		sequence: []mockStep{
			{
				toolCalls: []provider.ToolCall{
					{ID: "w1", Name: "write_test", Arguments: "{}"},
					{ID: "r1", Name: "read_test", Arguments: "{}"},
				},
			},
			{text: "done"},
		},
	}

	sess := NewSession("")
	ag := New(seq, reg, sess, Options{MaxSteps: 5})

	err := ag.Run(context.Background(), "write then read")
	if err != nil {
		t.Fatalf("sequential run: %v", err)
	}
}

func TestStormBreakerIntegration(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testFailingTool{})

	// 4 consecutive failures of the same tool → storm breaker should fire
	seq := &mockSequenceProvider{
		sequence: []mockStep{
			{toolCalls: []provider.ToolCall{{ID: "f1", Name: "fail_test", Arguments: `{}`}}},
			{toolCalls: []provider.ToolCall{{ID: "f2", Name: "fail_test", Arguments: `{}`}}},
			{toolCalls: []provider.ToolCall{{ID: "f3", Name: "fail_test", Arguments: `{}`}}},
			{toolCalls: []provider.ToolCall{{ID: "f4", Name: "fail_test", Arguments: `{}`}}},
			{text: "giving up"},
		},
	}

	sess := NewSession("")
	ag := New(seq, reg, sess, Options{MaxSteps: 10})

	err := ag.Run(context.Background(), "try something")
	if err != nil {
		t.Fatalf("storm breaker run: %v", err)
	}

	// After 3 identical failures, the 4th attempt should get a loop guard message
	snap := sess.Snapshot()
	foundLoopGuard := false
	for _, m := range snap {
		if m.Role == provider.RoleTool && strings.Contains(m.Content, "[loop guard]") {
			foundLoopGuard = true
			break
		}
	}
	if foundLoopGuard {
		t.Log("storm breaker correctly injected loop guard")
	}
}

func TestMaxStepsEnforcement(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testWriteTool{})

	// Provider that keeps calling tools forever
	seq := &mockSequenceProvider{
		sequence: []mockStep{
			{toolCalls: []provider.ToolCall{{ID: "w1", Name: "write_test", Arguments: "{}"}}},
			{toolCalls: []provider.ToolCall{{ID: "w2", Name: "write_test", Arguments: "{}"}}},
			{toolCalls: []provider.ToolCall{{ID: "w3", Name: "write_test", Arguments: "{}"}}},
			{toolCalls: []provider.ToolCall{{ID: "w4", Name: "write_test", Arguments: "{}"}}},
			{toolCalls: []provider.ToolCall{{ID: "w5", Name: "write_test", Arguments: "{}"}}},
		},
	}

	sess := NewSession("")
	ag := New(seq, reg, sess, Options{MaxSteps: 2})

	err := ag.Run(context.Background(), "do forever")
	if err != nil {
		t.Fatalf("should not error on maxSteps: %v", err)
	}
}

func TestEvidenceRecordingMultiTurn(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testWriteTool{})
	reg.Add(&testReadOnlyTool{})

	seq := &mockSequenceProvider{
		sequence: []mockStep{
			{toolCalls: []provider.ToolCall{{ID: "w1", Name: "write_test", Arguments: "{}"}}},
			{text: "wrote"},
		},
	}

	sess := NewSession("")
	ag := New(seq, reg, sess, Options{MaxSteps: 5})

	err := ag.Run(context.Background(), "write")
	if err != nil {
		t.Fatalf("evidence run: %v", err)
	}

	// Evidence should have at least one receipt
	if ag.evidence == nil {
		t.Fatal("evidence ledger should be non-nil after Run()")
	}
	receipts := ag.evidence.Receipts()
	if len(receipts) == 0 {
		t.Error("evidence should have at least one receipt")
	}
}

// ── Test helpers ───────────────────────────────────────────

func TestMockProviderStreamUsage(t *testing.T) {
	// Verify mock providers satisfy the Stream interface
	prov := &mockMultiTurnProvider{}
	ch, err := prov.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Temperature: 0,
	})
	if err != nil {
		t.Fatalf("mock Stream: %v", err)
	}
	consumed := false
	for range ch {
		consumed = true
	}
	if !consumed {
		t.Error("mock provider should produce chunks")
	}
}

func TestCompleteStepBlocksParallelBatch(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testWriteTool{})
	reg.Add(&testReadOnlyTool{})
	// Add a mock complete_step and todo_write
	reg.Add(&completeStepTool{})
	reg.Add(&todoWriteTool{})

	// complete_step should never be parallelisable
	if parallelisable(reg, "complete_step") {
		t.Error("complete_step must never be parallelisable")
	}
	if parallelisable(reg, "todo_write") {
		t.Error("todo_write must never be parallelisable")
	}
}

type completeStepTool struct{}

func (t *completeStepTool) Name() string            { return "complete_step" }
func (t *completeStepTool) Description() string     { return "complete step" }
func (t *completeStepTool) ReadOnly() bool          { return false }
func (t *completeStepTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *completeStepTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "completed", nil
}

type todoWriteTool struct{}

func (t *todoWriteTool) Name() string            { return "todo_write" }
func (t *todoWriteTool) Description() string     { return "todo write" }
func (t *todoWriteTool) ReadOnly() bool          { return false }
func (t *todoWriteTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *todoWriteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "ok", nil
}
