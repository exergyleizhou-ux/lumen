package eval

import (
	"regexp"
	"strings"
)

// FailureMode is one bucket of the pre-registered failure taxonomy (research
// design §3). Buckets are mutually exclusive; a run lands in exactly one.
type FailureMode string

const (
	Pass                    FailureMode = "pass"
	ContextOverflowGreeting FailureMode = "context_overflow_greeting" // F1 (headline)
	NoToolCall              FailureMode = "no_tool_call"              // F2
	MalformedArgs           FailureMode = "malformed_tool_args"       // F3
	WrongEdit               FailureMode = "wrong_edit"                // F4
	MaxSteps                FailureMode = "max_steps_exhaustion"      // F5
	TurnTimeout             FailureMode = "turn_timeout"              // F6
	EmptyStream             FailureMode = "empty_stream"              // F7
	EmptyFinal              FailureMode = "empty_final"               // F8
	TestTampered            FailureMode = "test_tampered"             // F9
	HarnessBreak            FailureMode = "harness_break"             // F10 (not a model failure)
	Unknown                 FailureMode = "unknown"
)

// RunSignals is everything the harness records about one task run that the
// classifier needs. All fields are computed deterministically from the eval/
// agent event stream + the post-run workspace diff (no model involved), so
// Classify is a pure function and can be κ-validated against hand labels.
type RunSignals struct {
	Passed              bool
	RunErr              string // Result.Err: "configure: …"/"copy: …"/non-model "run: …"
	StopReason          string // from the agent: finished/max_steps/timeout/empty_stream/empty_final
	ToolResultCount     int    // count of event.ToolResult (NOT ToolDispatch — finalized-only calls skip Dispatch)
	FilesChanged        int    // non-test source files changed (workspace pre/post diff)
	TestTampered        bool   // a protected *_test.go was modified (anti-cheat)
	MalformedArgs       bool   // a tool call's args failed to parse
	FirstPromptTokens   int    // provider-reported PromptTokens of the FIRST turn (overflow ground truth)
	ServerContextWindow int    // the LM Studio `-c` load value (operator-asserted axis); 0 = unknown
	FinalText           string // the model's final assistant text (for greeting detection)
}

// Rho is the dimensionless overflow ratio ρ = first_turn_prompt_tokens /
// server_context_window — the harness-invariant independent variable. Returns 0
// when the server window is unknown (guard against divide-by-zero).
func (s RunSignals) Rho() float64 {
	if s.ServerContextWindow <= 0 {
		return 0
	}
	return float64(s.FirstPromptTokens) / float64(s.ServerContextWindow)
}

// greeting matches a generic, task-unaware opening — the tell of a model that
// lost the system prompt to a slid context window and "restarted" the chat.
// Covers EN and the CJK form observed in the gemma 8k baseline.
var greeting = regexp.MustCompile(`(?i)^[\s>*_-]*(hi\b|hello|hey\b|how can i (help|assist)|what would you like|what can i (do|help)|i'?m ready|您好|你好|请问|有什么(可以)?(帮|能))`)

// Classify applies the ordered, first-match-wins taxonomy (§3). The order encodes
// precedence: an overflow is an overflow even though it also has zero tool calls;
// a timeout is a timeout even though it also made no edit.
func Classify(s RunSignals) FailureMode {
	if s.Passed {
		return Pass
	}
	// F1 — context-overflow-greeting (headline): prompt overflowed the real
	// window (ρ≥1), the model never acted (0 tools, 0 edits), and the final text
	// is a generic greeting rather than task work.
	if s.ToolResultCount == 0 && s.FilesChanged == 0 &&
		s.ServerContextWindow > 0 && s.FirstPromptTokens >= s.ServerContextWindow &&
		greeting.MatchString(s.FinalText) {
		return ContextOverflowGreeting
	}
	// F6 — turn timeout (ran out of wall time; precedence over no-edit shapes).
	if s.StopReason == "timeout" {
		return TurnTimeout
	}
	// F10 — harness/config break (not a model failure): the run never executed.
	if strings.HasPrefix(s.RunErr, "configure:") || strings.HasPrefix(s.RunErr, "copy:") {
		return HarnessBreak
	}
	// F7/F8 — provider/stream degeneracies.
	if s.StopReason == "empty_stream" {
		return EmptyStream
	}
	if s.StopReason == "empty_final" {
		return EmptyFinal
	}
	// F5 — exhausted the step budget without a final answer.
	if s.StopReason == "max_steps" {
		return MaxSteps
	}
	// F9 — passed the check by tampering with protected tests (anti-cheat).
	if s.TestTampered {
		return TestTampered
	}
	// F3 — emitted a tool call with unparseable arguments.
	if s.MalformedArgs {
		return MalformedArgs
	}
	// F4 — edited real source but the test still fails (genuine capability miss).
	if s.FilesChanged > 0 {
		return WrongEdit
	}
	// F2 — never called a tool, prompt fit, not a greeting (intent/capability miss).
	if s.ToolResultCount == 0 {
		return NoToolCall
	}
	return Unknown
}
