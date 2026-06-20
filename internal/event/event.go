// Package event defines the typed event stream the agent emits. Frontends
// (TUI, HTTP/SSE, headless) consume these to render the agent's activity.
package event

import (
	"sync"
	"time"
)

// Kind identifies the type of event.
type Kind string

const (
	TurnStarted  Kind = "turn_started"
	TurnDone     Kind = "turn_done"
	Phase        Kind = "phase"
	Text         Kind = "text"
	Reasoning    Kind = "reasoning"
	ToolDispatch Kind = "tool_dispatch"
	ToolResult   Kind = "tool_result"
	ToolProgress Kind = "tool_progress"
	FilePreview  Kind = "file_preview"
	UsageKind    Kind = "usage"
	Notice       Kind = "notice"
	Ask          Kind = "ask"
	PlanApproval Kind = "plan_approval"

	// Verify events bracket the verify-after-edit loop. VerifyStarted fires
	// before running build/vet/test on a writer batch; VerifyResult reports the
	// outcome (Text = summary, Level = info on success / warn on failure).
	VerifyStarted Kind = "verify_started"
	VerifyResult  Kind = "verify_result"

	// PerfKind reports per-turn performance: time-to-first-token, decode
	// throughput, and turn wall-clock. On local inference latency is the real
	// bottleneck (cost ≈ $0), so these are first-class.
	PerfKind Kind = "perf"
)

// Perf carries per-turn performance metrics for the perf HUD.
type Perf struct {
	TTFTMs           int64   `json:"ttft_ms"`           // time to first token (stream start → first chunk)
	TurnMs           int64   `json:"turn_ms"`           // stream start → stream done (wall clock)
	TokensPerSec     float64 `json:"tokens_per_sec"`    // decode throughput (completion tokens / generation window)
	CompletionTokens int     `json:"completion_tokens"` // real usage when reported, else 0
}

// Level is the severity of a Notice event.
type Level string

const (
	LevelInfo Level = "info"
	LevelWarn Level = "warn"
	LevelErr  Level = "error"
)

// Tool carries tool-call identity and display fields for ToolDispatch/ToolResult.
type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Args        string `json:"args,omitempty"`
	Output      string `json:"output,omitempty"`
	ReadOnly    bool   `json:"read_only"`
	Err         string `json:"err,omitempty"`
	Blocked     bool   `json:"blocked,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	ParentID    string `json:"parent_id,omitempty"` // for nested subagent calls
}

// Usage mirrors provider.Usage for event emission.
type Usage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	CacheHitTokens   int    `json:"cache_hit_tokens"`
	CacheMissTokens  int    `json:"cache_miss_tokens"`
	FinishReason     string `json:"finish_reason,omitempty"`
}

// Profile describes a model/effort selection for display.
type Profile struct {
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// AskQuestion is one question for the user (the ask tool).
type AskQuestion struct {
	Header      string      `json:"header"`
	Question    string      `json:"question"`
	Options     []AskOption `json:"options"`
	MultiSelect bool        `json:"multi_select"`
}

// AskOption is one choice in an AskQuestion.
type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AskAnswer is the user's response to one AskQuestion.
type AskAnswer struct {
	Header  string   `json:"header"`
	Answers []string `json:"answers"`
}

// Event is one typed event from the agent run loop.
type Event struct {
	Kind      Kind          `json:"kind"`
	Text      string        `json:"text,omitempty"`
	Tool      Tool          `json:"tool,omitempty"`
	Usage     *Usage        `json:"usage,omitempty"`
	Level     Level         `json:"level,omitempty"`
	Profile   *Profile      `json:"profile,omitempty"`
	Perf      *Perf         `json:"perf,omitempty"`
	Questions []AskQuestion `json:"questions,omitempty"`
	DiffText  string        `json:"diff,omitempty"`
	// StopReason is set on a TurnDone event to record WHY the turn ended:
	// "finished" | "max_steps" | "empty_stream" | "empty_final". (A turn-timeout
	// cancels the context before a clean TurnDone, so it is detected from Run's
	// returned error instead.) Consumed by the eval harness to classify failures.
	StopReason string    `json:"stop_reason,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// Sink is a receiver of agent events. The agent no longer formats output itself;
// it emits typed events and the frontend decides how to render them.
type Sink interface {
	Emit(e Event)
}

// FuncSink wraps a function as a Sink.
type FuncSink func(e Event)

func (f FuncSink) Emit(e Event) { f(e) }

// Discard is a sink that drops all events (headless runs, tests).
var Discard Sink = FuncSink(func(e Event) {})

// syncSink serializes Emit so an unsynchronized inner sink (e.g. a terminal/TUI/
// SSE closure with non-atomic state) is safe when emitted from multiple
// goroutines — the agent's foreground turn and a background run_in_background
// sub-agent both emit into the same sink concurrently.
type syncSink struct {
	mu    sync.Mutex
	inner Sink
}

func (s *syncSink) Emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner.Emit(e)
}

// NewSyncSink returns a goroutine-safe wrapper around inner. nil → Discard; an
// already-synchronized sink is returned unchanged (idempotent).
func NewSyncSink(inner Sink) Sink {
	if inner == nil {
		return Discard
	}
	if _, ok := inner.(*syncSink); ok {
		return inner
	}
	return &syncSink{inner: inner}
}
