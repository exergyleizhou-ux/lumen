package agent

import (
	"context"
	"strings"
	"testing"

	"lumen/internal/editverify"
	"lumen/internal/event"
	"lumen/internal/tool"
)

// capSink records emitted events for assertions.
type capSink struct{ events []event.Event }

func (c *capSink) Emit(e event.Event) { c.events = append(c.events, e) }

func (c *capSink) count(k event.Kind) int {
	n := 0
	for _, e := range c.events {
		if e.Kind == k {
			n++
		}
	}
	return n
}

// fakeVerifier returns scripted results in order, repeating the last one.
type fakeVerifier struct {
	results []editverify.Result
	calls   int
}

func (f *fakeVerifier) Verify(ctx context.Context, changed []string) editverify.Result {
	r := f.results[min(f.calls, len(f.results)-1)]
	f.calls++
	return r
}

func newVerifyAgent(t *testing.T, fv changeVerifier, cfg editverify.Config) (*Agent, *capSink) {
	t.Helper()
	cs := &capSink{}
	ag := New(nil, tool.NewRegistry(), NewSession(""), Options{Sink: cs, MaxSteps: 10})
	if fv != nil {
		ag.SetVerifier(fv, cfg)
	}
	return ag, cs
}

func okResult() editverify.Result   { return editverify.Result{OK: true} }
func failResult() editverify.Result {
	return editverify.Result{
		OK:     false,
		Failed: &editverify.Step{Name: "build", Args: []string{"go", "build", "./..."}},
		Diagnostics: []editverify.Diagnostic{
			{File: "x.go", Line: 1, Col: 2, Msg: "undefined: foo", Sev: "error"},
		},
	}
}

func TestVerifyHook_Pass(t *testing.T) {
	fv := &fakeVerifier{results: []editverify.Result{okResult()}}
	ag, cs := newVerifyAgent(t, fv, editverify.Config{Enabled: true, MaxRepairCycles: 3})
	if fb := ag.verifyAfterEdits(context.Background(), []string{"x.go"}); fb != "" {
		t.Errorf("pass should produce no feedback, got %q", fb)
	}
	if ag.repairCycle != 0 {
		t.Errorf("repairCycle should stay 0 on pass, got %d", ag.repairCycle)
	}
	if cs.count(event.VerifyStarted) != 1 || cs.count(event.VerifyResult) != 1 {
		t.Errorf("expected 1 started + 1 result event, got %d/%d", cs.count(event.VerifyStarted), cs.count(event.VerifyResult))
	}
}

func TestVerifyHook_FailInjectsFeedback(t *testing.T) {
	fv := &fakeVerifier{results: []editverify.Result{failResult()}}
	ag, _ := newVerifyAgent(t, fv, editverify.Config{Enabled: true, MaxRepairCycles: 3})
	fb := ag.verifyAfterEdits(context.Background(), []string{"x.go"})
	if !strings.Contains(fb, "undefined: foo") {
		t.Errorf("feedback should contain the diagnostic, got %q", fb)
	}
	if !strings.Contains(fb, "1/3") {
		t.Errorf("feedback should mark repair cycle 1/3, got %q", fb)
	}
	if ag.repairCycle != 1 {
		t.Errorf("repairCycle should be 1, got %d", ag.repairCycle)
	}
}

func TestVerifyHook_ExhaustsAfterCap(t *testing.T) {
	fv := &fakeVerifier{results: []editverify.Result{failResult()}}
	ag, _ := newVerifyAgent(t, fv, editverify.Config{Enabled: true, MaxRepairCycles: 2})

	// cycles 1 and 2: normal repair feedback
	for i := 1; i <= 2; i++ {
		fb := ag.verifyAfterEdits(context.Background(), []string{"x.go"})
		if strings.Contains(fb, "Stopping auto-verify") {
			t.Fatalf("cycle %d should not give up yet, got %q", i, fb)
		}
	}
	// cycle 3: over cap → giving-up message + exhausted
	fb := ag.verifyAfterEdits(context.Background(), []string{"x.go"})
	if !strings.Contains(fb, "Stopping auto-verify") {
		t.Errorf("over-cap should give up, got %q", fb)
	}
	if !ag.verifyExhausted {
		t.Error("verifyExhausted should be true after cap")
	}
	// subsequent calls: no-op (exhausted)
	if fb := ag.verifyAfterEdits(context.Background(), []string{"x.go"}); fb != "" {
		t.Errorf("exhausted should be silent, got %q", fb)
	}
}

func TestVerifyHook_DisabledAndNil(t *testing.T) {
	// disabled config
	fv := &fakeVerifier{results: []editverify.Result{failResult()}}
	ag, _ := newVerifyAgent(t, fv, editverify.Config{Enabled: false, MaxRepairCycles: 3})
	if fb := ag.verifyAfterEdits(context.Background(), []string{"x.go"}); fb != "" {
		t.Errorf("disabled should be silent, got %q", fb)
	}
	if fv.calls != 0 {
		t.Errorf("disabled should not call verifier, got %d calls", fv.calls)
	}

	// nil verifier (feature off)
	ag2, _ := newVerifyAgent(t, nil, editverify.Config{})
	if fb := ag2.verifyAfterEdits(context.Background(), []string{"x.go"}); fb != "" {
		t.Errorf("nil verifier should be silent, got %q", fb)
	}
}
