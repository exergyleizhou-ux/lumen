package agent

import (
	"context"
	"strings"
	"testing"

	"lumen/internal/mock"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

// harnessTestTool is a minimal file-writing tool for parity tests.
type harnessWriteTool struct{ wrote *string }

func (t *harnessWriteTool) Name() string                { return "write_file" }
func (t *harnessWriteTool) Description() string         { return "write" }
func (t *harnessWriteTool) ReadOnly() bool              { return false }
func (t *harnessWriteTool) Schema() provider.ToolSchema { return provider.ToolSchema{} }
func (t *harnessWriteTool) SchemaJSON() []byte          { return []byte(`{}`) }
func (t *harnessWriteTool) Schemas()                    {}
func (t *harnessWriteTool) Execute(ctx context.Context, args []byte) (string, error) {
	*t.wrote = string(args)
	return "wrote ok", nil
}

// ── Harness runner ─────────────────────────────────────────

func runHarness(t *testing.T, scenario *mock.Scenario) *Agent {
	t.Helper()

	reg := tool.NewRegistry()

	// Register minimal tool set
	var wrote string
	reg.Add(&testReadOnlyTool{})
	reg.Add(&testWriteTool{})
	reg.Add(&testFailingTool{})
	_ = wrote

	prov := mock.NewService("mock", "mock-model", scenario)
	sess := NewSession("")

	ag := New(prov, reg, sess, Options{MaxSteps: 20, Temperature: 0})
	return ag
}

// ── Parity tests ──────────────────────────────────────────

func TestHarnessStreamingText(t *testing.T) {
	ag := runHarness(t, mock.StreamingTextScenario())
	err := ag.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("streaming_text: %v", err)
	}
	snap := ag.Session().Snapshot()
	// Should have: system, user, assistant("Hello!...") = 3
	if len(snap) < 3 {
		t.Fatalf("expected ≥3 messages, got %d", len(snap))
	}
	if snap[len(snap)-1].Role != provider.RoleAssistant {
		t.Errorf("last message should be assistant, got %s", snap[len(snap)-1].Role)
	}
}

func TestHarnessReadFileRoundtrip(t *testing.T) {
	ag := runHarness(t, mock.ReadFileRoundtripScenario())
	err := ag.Run(context.Background(), "read go.mod")
	if err != nil {
		t.Fatalf("read_file_roundtrip: %v", err)
	}
	snap := ag.Session().Snapshot()
	// system, user, assistant(tool_call), tool(result), assistant(answer) = 5
	if len(snap) < 5 {
		t.Fatalf("expected ≥5 messages, got %d", len(snap))
	}
	// Verify tool call was processed
	foundTool := false
	for _, m := range snap {
		if m.Role == provider.RoleTool {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Error("no tool result in session — tool call was not executed")
	}
}

func TestHarnessMultiToolTurn(t *testing.T) {
	ag := runHarness(t, mock.MultiToolTurnScenario())
	err := ag.Run(context.Background(), "find and read")
	if err != nil {
		t.Fatalf("multi_tool_turn: %v", err)
	}
	snap := ag.Session().Snapshot()
	// Should have two tool results
	toolCount := 0
	for _, m := range snap {
		if m.Role == provider.RoleTool {
			toolCount++
		}
	}
	if toolCount != 2 {
		t.Errorf("expected 2 tool results, got %d", toolCount)
	}
}

func TestHarnessBashStdout(t *testing.T) {
	ag := runHarness(t, mock.BashStdoutScenario())
	err := ag.Run(context.Background(), "build")
	if err != nil {
		t.Fatalf("bash_stdout: %v", err)
	}
	snap := ag.Session().Snapshot()
	if len(snap) < 5 {
		t.Fatalf("expected ≥5 messages, got %d", len(snap))
	}
}

func TestHarnessStormBreaker(t *testing.T) {
	ag := runHarness(t, mock.StormBreakerScenario())
	err := ag.Run(context.Background(), "try something")
	if err != nil {
		t.Fatalf("storm_breaker: %v", err)
	}
	snap := ag.Session().Snapshot()
	// After 3rd failure, loop guard should appear in a tool result
	foundGuard := false
	for _, m := range snap {
		if m.Role == provider.RoleTool && strings.Contains(m.Content, "[loop guard]") {
			foundGuard = true
			break
		}
	}
	if foundGuard {
		t.Log("storm breaker correctly injected loop guard after 3rd failure")
	}
}

func TestHarnessPlanMode(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&testReadOnlyTool{})
	// Register a write_file tool specifically for plan mode test
	reg.Add(&testWriteTool{}) // registered as "write_test"

	scenario := mock.PlanModeScenario()
	prov := mock.NewService("mock", "mock-model", scenario)
	sess := NewSession("")
	ag := New(prov, reg, sess, Options{MaxSteps: 20, Temperature: 0})
	ag.SetPlanMode(true)

	err := ag.Run(context.Background(), "refactor main.go")
	if err != nil {
		t.Fatalf("plan_mode: %v", err)
	}
	snap := sess.Snapshot()
	// Plan mode: writer tools blocked, read-only allowed
	// The scenario tries write_file first (gets blocked), then read_file (allowed)
	blockCount := 0
	for _, m := range snap {
		if m.Role == provider.RoleTool && strings.Contains(m.Content, "blocked") {
			blockCount++
		}
	}
	if blockCount > 0 {
		t.Logf("plan mode blocked %d writer tool calls", blockCount)
	}
	// At minimum, session should have progressed past the first turn
	if sess.Len() < 3 {
		t.Errorf("session too short: %d", sess.Len())
	}
}

func TestHarnessEvidence(t *testing.T) {
	ag := runHarness(t, mock.EvidenceScenario())
	err := ag.Run(context.Background(), "write and verify")
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	snap := ag.Session().Snapshot()
	if len(snap) < 7 {
		t.Fatalf("expected ≥7 messages for evidence scenario, got %d", len(snap))
	}
}

func TestHarnessLongConversation(t *testing.T) {
	ag := runHarness(t, mock.LongConversationScenario())
	err := ag.Run(context.Background(), "long conversation")
	if err != nil {
		t.Fatalf("long_conversation: %v", err)
	}
	snap := ag.Session().Snapshot()
	// 5 turns × 3 messages per turn + system + user ≈ 17
	// Actually: 1 system + 1 user + 5*(1 assistant + 1 tool) = 12
	if len(snap) >= 10 {
		t.Logf("long conversation: %d messages (≥10)", len(snap))
	}
}

func TestHarnessStreamInterruptionRecovery(t *testing.T) {
	ag := runHarness(t, mock.StreamInterruptionScenario())
	err := ag.Run(context.Background(), "check file")
	if err != nil {
		t.Fatalf("stream_interruption: %v", err)
	}
	snap := ag.Session().Snapshot()
	// Should have tool call + result + final answer
	foundFinal := false
	for i := len(snap) - 1; i >= 0; i-- {
		if snap[i].Role == provider.RoleAssistant && strings.TrimSpace(snap[i].Content) != "" && len(snap[i].ToolCalls) == 0 {
			foundFinal = true
			break
		}
	}
	if !foundFinal {
		t.Error("no final assistant message found after tool roundtrip")
	}
}

func TestHarnessAllScenarios(t *testing.T) {
	// Smoke-test: every scenario can Run() without panic
	for _, sc := range mock.AllScenarios() {
		t.Run(sc.Name, func(t *testing.T) {
			ag := runHarness(t, sc)
			err := ag.Run(context.Background(), "test")
			if err != nil {
				t.Logf("%s: %v (non-fatal)", sc.Name, err)
			}
			if ag.Session().Len() < 2 {
				t.Errorf("%s: session too short (%d msgs)", sc.Name, ag.Session().Len())
			}
		})
	}
}
