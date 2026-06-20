package eval

import "testing"

// Classify must be a pure, deterministic, mutually-exclusive ORDERED classifier
// (the research design §3: overflow → timeout → harness-break → empty-stream →
// empty-final → max-steps → test-tamper → malformed-args → wrong-edit →
// no-tool-call → pass). First matching rule wins. These cases pin each bucket
// and the ordering precedence between them.
func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		s    RunSignals
		want FailureMode
	}{
		{"pass", RunSignals{Passed: true}, Pass},
		{
			// F1 context-overflow-greeting: ρ≥1, zero tools, zero edits, greeting text.
			"overflow-greeting",
			RunSignals{ToolResultCount: 0, FilesChanged: 0, FirstPromptTokens: 11000, ServerContextWindow: 8192, FinalText: "Hello! How can I help you today?"},
			ContextOverflowGreeting,
		},
		{
			// CJK greeting (the exact failure seen in the gemma 8k baseline).
			"overflow-greeting-cjk",
			RunSignals{FirstPromptTokens: 12000, ServerContextWindow: 8192, FinalText: "您好！请问您需要什么帮助？"},
			ContextOverflowGreeting,
		},
		{
			// Same zero-tool/zero-edit signature but prompt FITS (ρ<1) and text is
			// not a greeting → it's an intent/capability miss, NOT overflow.
			"no-tool-call-when-fits",
			RunSignals{ToolResultCount: 0, FilesChanged: 0, FirstPromptTokens: 4000, ServerContextWindow: 16384, FinalText: "The fix is to add a nil check before the map write."},
			NoToolCall,
		},
		{
			// Timeout takes precedence over the no-edit shape (it ran out of wall time).
			"timeout",
			RunSignals{StopReason: "timeout", ToolResultCount: 0, FilesChanged: 0},
			TurnTimeout,
		},
		{
			"harness-break-configure",
			RunSignals{RunErr: "configure: provider x: bad key"},
			HarnessBreak,
		},
		{"empty-stream", RunSignals{StopReason: "empty_stream"}, EmptyStream},
		{"empty-final", RunSignals{StopReason: "empty_final"}, EmptyFinal},
		{"max-steps", RunSignals{StopReason: "max_steps", ToolResultCount: 7}, MaxSteps},
		{"test-tamper", RunSignals{TestTampered: true, FilesChanged: 1}, TestTampered},
		{"malformed-args", RunSignals{MalformedArgs: true, ToolResultCount: 1}, MalformedArgs},
		{
			// Edited a real file but the test still fails → genuine capability miss.
			"wrong-edit",
			RunSignals{ToolResultCount: 4, FilesChanged: 1, StopReason: "finished"},
			WrongEdit,
		},
	}
	for _, c := range cases {
		if got := Classify(c.s); got != c.want {
			t.Errorf("%s: Classify = %q, want %q", c.name, got, c.want)
		}
	}
}

// ρ helper must guard divide-by-zero (unknown server window).
func TestRho(t *testing.T) {
	if r := (RunSignals{FirstPromptTokens: 8000, ServerContextWindow: 16384}).Rho(); r < 0.48 || r > 0.49 {
		t.Errorf("Rho = %v, want ~0.488", r)
	}
	if r := (RunSignals{FirstPromptTokens: 8000, ServerContextWindow: 0}).Rho(); r != 0 {
		t.Errorf("Rho with unknown window = %v, want 0 (guarded)", r)
	}
}
