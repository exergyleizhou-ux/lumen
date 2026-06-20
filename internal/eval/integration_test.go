package eval_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"lumen/internal/agent"
	"lumen/internal/eval"
	"lumen/internal/provider"
	"lumen/internal/tool"
	_ "lumen/internal/tool/builtin" // register builtins (write_file, …) via init
)

// scriptedProvider replays a fixed sequence of tool-call turns so the agent loop
// runs deterministically with no model — the way the eval harness is exercised in
// CI. Once the script is exhausted it returns a final "done" answer.
type scriptedProvider struct {
	mu    sync.Mutex
	turns [][]provider.ToolCall
	i     int
}

func (s *scriptedProvider) Name() string { return "scripted" }

func (s *scriptedProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 8)
	go func() {
		defer close(ch)
		s.mu.Lock()
		var calls []provider.ToolCall
		if s.i < len(s.turns) {
			calls = s.turns[s.i]
			s.i++
		}
		s.mu.Unlock()
		if len(calls) == 0 {
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
			ch <- provider.Chunk{Type: provider.ChunkDone}
			return
		}
		for i := range calls {
			ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &calls[i]}
		}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func writeFileCall(id, path, content string) provider.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return provider.ToolCall{ID: id, Name: "write_file", Arguments: string(args)}
}

func builtinRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	for _, bt := range tool.Builtins() {
		reg.Add(bt)
	}
	return reg
}

// runFixture loads the testdata fixture task, copies it to a temp workspace, runs
// the agent (driven by prov) inside it, then scores + anti-cheat-checks the result.
func runFixture(t *testing.T, prov provider.Provider) (pass bool, changedTests []string) {
	t.Helper()
	tasks, err := eval.LoadTasks("testdata")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("load fixture tasks: err=%v n=%d", err, len(tasks))
	}
	task := tasks[0]

	ws := t.TempDir()
	if err := eval.CopyDir(task.Workspace, ws); err != nil {
		t.Fatalf("copy workspace: %v", err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(ws); err != nil { // builtins resolve paths against cwd
		t.Fatal(err)
	}
	ag := agent.New(prov, builtinRegistry(t), agent.NewSession(""), agent.Options{MaxSteps: 5})
	runErr := ag.Run(context.Background(), task.Prompt)
	_ = os.Chdir(orig) // restore before scoring against absolute/relative paths
	if runErr != nil {
		t.Fatalf("agent run: %v", runErr)
	}

	pass, _ = eval.Score(context.Background(), ws, task.Check)
	_, changedTests = eval.ProtectedTestsUnchanged(task.Workspace, ws)
	return pass, changedTests
}

const fixedCalc = `package calc

// Double returns n*2.
func Double(n int) int {
	return n * 2
}
`

const tamperedTest = `package calc

import "testing"

func TestDouble(t *testing.T) {} // assertion removed
`

// The whole eval pipeline — load task, drive the agent through a real tool call,
// score by go test — runs end-to-end with a scripted provider (no model). CI's
// `go test` therefore gates a regression in the harness, agent loop, tool
// execution, or scoring.
func TestEvalPipeline_SolvesFixtureEndToEnd(t *testing.T) {
	prov := &scriptedProvider{turns: [][]provider.ToolCall{
		{writeFileCall("c1", "calc/calc.go", fixedCalc)},
	}}
	pass, changed := runFixture(t, prov)
	if !pass {
		t.Fatal("fixture should pass after the agent writes the correct fix")
	}
	if len(changed) != 0 {
		t.Fatalf("a legitimate fix must not touch tests, got %v", changed)
	}
}

// A "pass" earned by gutting the test must be caught by the anti-cheat gate: go
// test goes green (no assertion left), but the protected-tests check flags it.
func TestEvalPipeline_RejectsTestTampering(t *testing.T) {
	prov := &scriptedProvider{turns: [][]provider.ToolCall{
		{writeFileCall("c1", "calc/calc_test.go", tamperedTest)},
	}}
	_, changed := runFixture(t, prov)
	if len(changed) == 0 {
		t.Fatal("tampering with the protected test file must be flagged")
	}
}
