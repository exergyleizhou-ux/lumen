package agent

import (
	"context"
	"errors"
	"testing"

	"lumen/internal/editverify"
	"lumen/internal/provider"
	"lumen/internal/tool"
	"lumen/internal/tool/builtin"
	runworkspace "lumen/internal/workspace"
)

type completionVerifier struct {
	result editverify.Result
	calls  int
	root   string
}

func (v *completionVerifier) Verify(ctx context.Context, changed []string) editverify.Result {
	v.calls++
	if ws, ok := runworkspace.FromContext(ctx); ok {
		v.root = ws.Root
	}
	return v.result
}

func newCompletionAgent(t *testing.T, result *editverify.Result) (*Agent, *completionVerifier, context.Context) {
	t.Helper()
	ws, err := runworkspace.NewLocal("completion", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := runworkspace.WithContext(context.Background(), ws)
	reg := tool.NewRegistry()
	reg.Add(&builtin.WriteFileTool{})
	p := &mockSequenceProvider{sequence: []mockStep{
		{toolCalls: []provider.ToolCall{{ID: "write-1", Name: "write_file", Arguments: `{"path":"main.go","content":"package main\n"}`}}},
		{text: "implemented"},
	}}
	a := New(p, reg, NewSession(""), Options{MaxSteps: 4})
	var verifier *completionVerifier
	if result != nil {
		verifier = &completionVerifier{result: *result}
		a.SetVerifier(verifier, editverify.DefaultConfig())
	}
	return a, verifier, ctx
}

func TestFinalRequiresVerificationPass(t *testing.T) {
	a, verifier, ctx := newCompletionAgent(t, &editverify.Result{OK: true, Ran: 1})
	if err := a.Run(ctx, "implement"); err != nil {
		t.Fatal(err)
	}
	if verifier.calls != 1 || verifier.root == "" {
		t.Fatalf("verification did not use run workspace: calls=%d root=%q", verifier.calls, verifier.root)
	}
}

func TestFinalRequiresVerificationIncomplete(t *testing.T) {
	a, _, ctx := newCompletionAgent(t, &editverify.Result{OK: true, Ran: 0})
	err := a.Run(ctx, "implement")
	if !errors.Is(err, ErrVerificationIncomplete) {
		t.Fatalf("error=%v want ErrVerificationIncomplete", err)
	}
}

func TestFinalRequiresVerificationFailure(t *testing.T) {
	failed := &editverify.Step{Name: "test"}
	a, _, ctx := newCompletionAgent(t, &editverify.Result{OK: false, Ran: 1, Failed: failed, Output: "failed"})
	err := a.Run(ctx, "implement")
	if !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("error=%v want ErrVerificationFailed", err)
	}
}

func TestFinalWithoutProjectVerifierRemainsCompatible(t *testing.T) {
	a, _, ctx := newCompletionAgent(t, nil)
	if err := a.Run(ctx, "write a plain file"); err != nil {
		t.Fatalf("non-project writing should remain compatible: %v", err)
	}
}
