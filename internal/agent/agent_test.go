package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

// ── Mock provider for testing ──────────────────────────────

type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	go func() {
		defer close(ch)
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

// ── Simple test tool ────────────────────────────────────────

type testReadOnlyTool struct{}

func (t *testReadOnlyTool) Name() string            { return "read_test" }
func (t *testReadOnlyTool) Description() string     { return "test read-only tool" }
func (t *testReadOnlyTool) ReadOnly() bool          { return true }
func (t *testReadOnlyTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *testReadOnlyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "read result", nil
}

type testWriteTool struct{}

func (t *testWriteTool) Name() string            { return "write_test" }
func (t *testWriteTool) Description() string     { return "test write tool" }
func (t *testWriteTool) ReadOnly() bool          { return false }
func (t *testWriteTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *testWriteTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "wrote ok", nil
}

type testFailingTool struct{}

func (t *testFailingTool) Name() string            { return "fail_test" }
func (t *testFailingTool) Description() string     { return "always fails" }
func (t *testFailingTool) ReadOnly() bool          { return false }
func (t *testFailingTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *testFailingTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "", errors.New("simulated failure")
}

// ── Test agent helpers ─────────────────────────────────────

func testRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	reg.Add(&testReadOnlyTool{})
	reg.Add(&testWriteTool{})
	reg.Add(&testFailingTool{})
	return reg
}

func testAgent() *Agent {
	return New(
		&mockProvider{name: "test"},
		testRegistry(),
		NewSession(""),
		Options{
			MaxSteps:    5,
			Temperature: 0,
		},
	)
}

// ── Tests ──────────────────────────────────────────────────

func TestExecuteOneUnknownTool(t *testing.T) {
	a := testAgent()
	outcome := a.executeOne(context.Background(), provider.ToolCall{
		ID: "1", Name: "nonexistent", Arguments: "{}",
	})
	if outcome.errMsg == "" {
		t.Error("unknown tool should return error")
	}
}

func TestExecuteOneReadOnlyTool(t *testing.T) {
	a := testAgent()
	outcome := a.executeOne(context.Background(), provider.ToolCall{
		ID: "2", Name: "read_test", Arguments: "{}",
	})
	if outcome.errMsg != "" {
		t.Errorf("read-only tool should succeed, got: %s", outcome.errMsg)
	}
	if outcome.output != "read result" {
		t.Errorf("expected 'read result', got %q", outcome.output)
	}
}

func TestPlanModeBlocksWriter(t *testing.T) {
	a := testAgent()
	a.SetPlanMode(true)

	outcome := a.executeOne(context.Background(), provider.ToolCall{
		ID: "3", Name: "write_test", Arguments: "{}",
	})
	if !outcome.blocked {
		t.Error("plan mode should block writer tools")
	}
}

func TestPlanModeAllowsReader(t *testing.T) {
	a := testAgent()
	a.SetPlanMode(true)

	outcome := a.executeOne(context.Background(), provider.ToolCall{
		ID: "4", Name: "read_test", Arguments: "{}",
	})
	if outcome.blocked {
		t.Error("plan mode should NOT block read-only tools")
	}
}

func TestPermissionGateBlocks(t *testing.T) {
	a := testAgent()
	a.SetGate(&denyAllGate{})

	outcome := a.executeOne(context.Background(), provider.ToolCall{
		ID: "5", Name: "write_test", Arguments: "{}",
	})
	if !outcome.blocked {
		t.Error("deny-all gate should block the tool")
	}
}

func TestExecuteFailingTool(t *testing.T) {
	a := testAgent()
	outcome := a.executeOne(context.Background(), provider.ToolCall{
		ID: "7", Name: "fail_test", Arguments: "{}",
	})
	if outcome.errMsg == "" {
		t.Error("failing tool should return error message")
	}
}

func TestStormBreakerDetectsLoop(t *testing.T) {
	a := testAgent()

	// First failure
	calls := []provider.ToolCall{{ID: "10", Name: "fail_test", Arguments: "{}"}}
	outcomes := []toolOutcome{{errMsg: "simulated failure", output: "error: simulated failure\n"}}
	a.applyStormBreaker(calls, outcomes)
	if a.stormCount != 1 {
		t.Errorf("stormCount should be 1 after first failure, got %d", a.stormCount)
	}

	// Second failure — same sig
	a.applyStormBreaker(calls, outcomes)
	if a.stormCount != 2 {
		t.Errorf("stormCount should be 2, got %d", a.stormCount)
	}

	// Third failure — should append loop guard
	a.applyStormBreaker(calls, outcomes)
	if a.stormCount != 3 {
		t.Errorf("stormCount should be 3, got %d", a.stormCount)
	}
	if !contains(outcomes[0].output, "[loop guard]") {
		t.Error("expected loop guard message after 3 consecutive failures")
	}
}

func TestStormBreakerResetsOnSuccess(t *testing.T) {
	a := testAgent()
	failCalls := []provider.ToolCall{{ID: "10", Name: "fail_test", Arguments: "{}"}}
	failOut := []toolOutcome{{errMsg: "simulated failure", output: "error\n"}}

	a.applyStormBreaker(failCalls, failOut)
	a.applyStormBreaker(failCalls, failOut)

	// Now a success should reset
	successCalls := []provider.ToolCall{{ID: "11", Name: "write_test", Arguments: "{}"}}
	successOut := []toolOutcome{{output: "ok"}}
	a.applyStormBreaker(successCalls, successOut)

	if a.stormCount != 0 {
		t.Errorf("stormCount should reset to 0 after success, got %d", a.stormCount)
	}
}

func TestStormBreakerResetsOnDifferentError(t *testing.T) {
	a := testAgent()

	calls1 := []provider.ToolCall{{ID: "10", Name: "fail_test", Arguments: "{}"}}
	out1 := []toolOutcome{{errMsg: "error A", output: "error A\n"}}
	a.applyStormBreaker(calls1, out1)

	calls2 := []provider.ToolCall{{ID: "11", Name: "fail_test", Arguments: "{}"}}
	out2 := []toolOutcome{{errMsg: "error B", output: "error B\n"}}
	a.applyStormBreaker(calls2, out2)

	// Different error should reset
	if a.stormCount != 1 {
		t.Errorf("different error should reset stormSig, got count %d", a.stormCount)
	}
}

func TestToolCallPartitioning(t *testing.T) {
	reg := testRegistry()

	// Two read-only tools → parallel
	calls := []provider.ToolCall{
		{ID: "1", Name: "read_test", Arguments: "{}"},
		{ID: "2", Name: "read_test", Arguments: "{}"},
	}
	batches := partitionToolCalls(reg, calls)
	if len(batches) != 1 || !batches[0].parallel {
		t.Error("two read-only tools should be one parallel batch")
	}

	// Writer + reader → two serial batches
	calls2 := []provider.ToolCall{
		{ID: "1", Name: "write_test", Arguments: "{}"},
		{ID: "2", Name: "read_test", Arguments: "{}"},
	}
	batches2 := partitionToolCalls(reg, calls2)
	if len(batches2) != 2 {
		t.Errorf("writer+reader should be 2 batches, got %d", len(batches2))
	}
	if batches2[0].parallel {
		t.Error("writer batch should not be parallel")
	}
}

func TestParallelisable(t *testing.T) {
	reg := testRegistry()

	if parallelisable(reg, "complete_step") {
		t.Error("complete_step should never be parallelisable")
	}
	if parallelisable(reg, "todo_write") {
		t.Error("todo_write should never be parallelisable")
	}
	if !parallelisable(reg, "read_test") {
		t.Error("read_test should be parallelisable")
	}
	if parallelisable(reg, "write_test") {
		t.Error("write_test should NOT be parallelisable")
	}
}

func TestSystemPromptInjected(t *testing.T) {
	a := testAgent()
	if a.session.Len() != 0 {
		t.Skip("session already populated")
	}

	err := a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	snap := a.session.Snapshot()
	if len(snap) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(snap))
	}
	if snap[0].Role != provider.RoleSystem {
		t.Errorf("first message should be system, got %s", snap[0].Role)
	}
}

func TestRunWithoutToolsProducesAnswer(t *testing.T) {
	a := testAgent()
	err := a.Run(context.Background(), "say hi")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// Should have system prompt + user + assistant (final answer)
	snap := a.session.Snapshot()
	if len(snap) < 3 {
		t.Errorf("expected at least 3 messages, got %d", len(snap))
	}
}

// ── Helpers ────────────────────────────────────────────────

type denyAllGate struct{}

func (g *denyAllGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	return false, "denied by test", nil
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// callCtxProbeTool records whether a call context was stamped onto ctx.
type callCtxProbeTool struct {
	gotID string
	gotOK bool
}

func (t *callCtxProbeTool) Name() string            { return "probe" }
func (t *callCtxProbeTool) Description() string     { return "probe call context" }
func (t *callCtxProbeTool) ReadOnly() bool          { return true }
func (t *callCtxProbeTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *callCtxProbeTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	id, _, _, ok := CallContext(ctx)
	t.gotID = id
	t.gotOK = ok
	return "ok", nil
}

func TestExecuteOneStampsCallContext(t *testing.T) {
	probe := &callCtxProbeTool{}
	reg := tool.NewRegistry()
	reg.Add(probe)
	a := New(&mockProvider{name: "test"}, reg, NewSession(""), Options{MaxSteps: 3})
	a.executeOne(context.Background(), provider.ToolCall{ID: "call_42", Name: "probe", Arguments: "{}"})
	if !probe.gotOK {
		t.Fatal("executeOne should stamp a call context onto ctx so sub-agents can nest events")
	}
	if probe.gotID != "call_42" {
		t.Errorf("parentID: want call_42, got %q", probe.gotID)
	}
}

func TestAutoCompactMarkerDoesNotClaimSummary(t *testing.T) {
	a := New(&mockProvider{name: "test"}, testRegistry(), NewSession(""), Options{
		MaxSteps: 5, ContextWindow: 90, RecentKeep: 2, Sink: event.Discard,
	})
	for i := 0; i < 12; i++ {
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("x", 40)})
	}
	a.autoCompact()

	var marker string
	for _, m := range a.session.Snapshot() {
		if strings.Contains(strings.ToLower(m.Content), "compact") {
			marker = m.Content
		}
	}
	if marker == "" {
		t.Fatal("autoCompact should have inserted a compaction marker")
	}
	low := strings.ToLower(marker)
	if strings.Contains(low, "summariz") {
		t.Errorf("sliding-window compaction must not claim messages were summarized: %q", marker)
	}
	if !strings.Contains(low, "omit") && !strings.Contains(low, "drop") {
		t.Errorf("marker should honestly say earlier messages were dropped/omitted: %q", marker)
	}
}
