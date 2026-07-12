package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"lumen/internal/event"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

func TestAgentSetSinkConcurrentWithEmit(t *testing.T) {
	// SetSink (e.g. a TUI redirect) must be safe against the turn goroutine
	// reading the sink. Run under -race.
	a := New(&mockProvider{name: "test"}, testRegistry(), NewSession(""), Options{MaxSteps: 1})
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				a.Sink().Emit(event.Event{Kind: event.Text})
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			a.SetSink(event.Discard)
		}
		close(stop)
	}()
	wg.Wait()
}

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

// emptyStreamProvider streams zero chunks then closes — simulating a dead
// connection or a 200 response with no usable body.
type emptyStreamProvider struct{}

func (emptyStreamProvider) Name() string { return "empty" }
func (emptyStreamProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

func TestRunReturnsErrorOnEmptyStream(t *testing.T) {
	a := New(emptyStreamProvider{}, testRegistry(), NewSession(""), Options{MaxSteps: 3})
	err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("a zero-chunk stream is a provider failure and must return an error, not nil (silent success)")
	}
}

// interruptThenOKProvider interrupts the first stream mid-output, then succeeds.
type interruptThenOKProvider struct{ calls int }

func (p *interruptThenOKProvider) Name() string { return "interrupt" }
func (p *interruptThenOKProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	n := p.calls
	ch := make(chan provider.Chunk, 4)
	go func() {
		defer close(ch)
		if n == 1 {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "partial"}
			ch <- provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: errors.New("connection reset")}}
			return
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "recovered answer."}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func TestRunRecoversFromStreamInterruption(t *testing.T) {
	p := &interruptThenOKProvider{}
	a := New(p, testRegistry(), NewSession(""), Options{MaxSteps: 5})
	err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("a mid-stream interruption should auto-recover, got error: %v", err)
	}
	if p.calls != 2 {
		t.Fatalf("expected 2 stream attempts (interrupt + 1 recovery), got %d", p.calls)
	}
}

// recordingProvider captures the request it was asked to stream.
type recordingProvider struct {
	lastReq provider.Request
	calls   int
}

func (p *recordingProvider) Name() string { return "rec" }
func (p *recordingProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.lastReq = req
	p.calls++
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestRunSanitizesOrphanedToolCallBeforeRequest(t *testing.T) {
	p := &recordingProvider{}
	a := New(p, testRegistry(), NewSession(""), Options{MaxSteps: 1})
	// Seed an assistant tool_call with NO matching tool result, then a later
	// non-tool message — so the last message is not a tool result and the old
	// narrow "needsRepair" gate would skip sanitization, sending an orphaned
	// tool_call the provider rejects with HTTP 400.
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	a.session.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "x", Name: "read_test"}}})

	a.Run(context.Background(), "continue")

	if p.calls == 0 {
		t.Fatal("provider was never called")
	}
	msgs := p.lastReq.Messages
	for i, m := range msgs {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "x" {
			if i+1 >= len(msgs) || msgs[i+1].Role != provider.RoleTool || msgs[i+1].ToolCallID != "x" {
				t.Fatalf("orphaned tool_call sent to provider: assistant tool_call 'x' not followed by its tool result; got %+v", msgs)
			}
			return
		}
	}
	t.Fatalf("expected the assistant tool_call in the request, messages=%+v", msgs)
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

type testCommandTool struct{}

func (t *testCommandTool) Name() string            { return "command_test" }
func (t *testCommandTool) Description() string     { return "test command tool" }
func (t *testCommandTool) ReadOnly() bool          { return false }
func (t *testCommandTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *testCommandTool) Effects() tool.Effects   { return tool.Effects{RunsCommands: true} }
func (t *testCommandTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "command ok", nil
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

func TestCommandEffectDoesNotReportFileWrite(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testCommandTool{})
	a := New(&mockProvider{name: "test"}, reg, NewSession(""), Options{MaxSteps: 3})
	outcome := a.executeOne(context.Background(), provider.ToolCall{
		ID: "command-1", Name: "command_test", Arguments: "{}",
	})
	if outcome.errMsg != "" {
		t.Fatalf("command failed: %s", outcome.errMsg)
	}
	if outcome.wroteFile {
		t.Fatal("command-only effects must not trigger edit verification")
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

func (g *denyAllGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, json.RawMessage, error) {
	return false, "denied by test", nil, nil
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
	a.autoCompact(context.Background())

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
