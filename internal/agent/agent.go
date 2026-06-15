// Package agent drives the core coding-agent loop: prompt → stream → tool calls
// → execute → repeat. It supports plan mode (read-only gating without cache
// invalidation), parallel read-only tool dispatch, storm-breaker dead-loop
// detection, and automatic context compaction for long sessions.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"lumen/internal/checkpoint"
	"lumen/internal/diff"
	"lumen/internal/event"
	"lumen/internal/evidence"
	"lumen/internal/guard"
	"lumen/internal/jobs"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

// DefaultSystemPrompt is the base system message sent to every model.
const DefaultSystemPrompt = `You are Lumen, a coding agent. You run on various LLM backends (the model you are currently using is listed in the opening banner). This is a terminal-based coding assistant.
Use the provided tools to read and write files and run shell commands.
Principles: understand the request before acting; verify with tools instead of guessing; keep changes minimal and correct; briefly summarize what you did.
When the request leaves a real choice to the user, use the ask tool to offer 2-4 concrete options. For multi-step work, track progress with todo_write, and sign off each step with complete_step.

IMPORTANT: If asked what model you are, say "I'm Lumen running on the model shown in the top bar (e.g., DeepSeek, OpenAI, Anthropic). I don't hardcode my backend model identity — check the banner above ✨."`

// ── Constants ─────────────────────────────────────────────

const (
	maxToolOutputBytes      = 32 * 1024
	stormBreakThreshold     = 3
	maxStreamRecoveries     = 1
	maxEmptyFinalBlocks     = 3
	maxFinalReadinessBlocks = 3
)

// ── Renderer / Asker / Gate interfaces ────────────────────

// Renderer redraws the assistant's final-answer text as styled output.
type Renderer interface {
	Render(text string) string
}

// Asker puts structured multiple-choice questions to the user.
type Asker interface {
	Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error)
}

// Gate decides, per tool call, whether it may run.
type Gate interface {
	Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (allow bool, reason string, err error)
}

// ── Agent ──────────────────────────────────────────────────

// Agent drives a single task: a Provider, a tool Registry, and a Session wired
// into the main loop.
type Agent struct {
	prov     provider.Provider
	tools    *tool.Registry
	session  *Session
	sessMu   sync.Mutex
	maxSteps int

	temperature float64
	pricing     *provider.Pricing
	sink        event.Sink

	lastUsage atomic.Pointer[provider.Usage]

	sessCacheHit  atomic.Int64
	sessCacheMiss atomic.Int64

	// planMode, when true, refuses any non-ReadOnly tool call at execute time.
	// The system prompt and tool list never change, so the prefix cache stays hot.
	planMode atomic.Bool

	gate  Gate
	asker Asker

	// onPreEdit fires before a writer tool runs to capture a pre-edit snapshot.
	onPreEdit func(diff.Change)

	// checkpoint holds pre-edit file snapshots for this turn so the user can
	// rewind (Esc-Esc). Set via SetCheckpoint; the agent feeds it via onPreEdit.
	checkpoint *checkpoint.Store

	// cache tracks prefix-cache stability across turns. It computes the
	// prefix shape before each API call and detects churn.
	cache *cacheTracker

	// jobs is the session's background job manager. executeOne stamps it onto
	// each tool call's context so bash/task run_in_background tools can access it.
	jobs *jobs.Manager

	// cachedSchemas is the stable tool schema list, computed once at registration
	// and reused every turn. It must stay byte-identical across turns for the
	// DeepSeek prefix cache to stay hot.
	cachedSchemas []provider.ToolSchema

	// stormSig / stormCount track consecutive identical failures (see storm breaker).
	stormSig   string
	stormCount int

	// repeatSuccessCounts tracks write-like calls that keep succeeding identically.
	repeatSuccessCounts map[string]int

	// emptyFinalCount tracks consecutive empty final answers (reset on non-empty).
	emptyFinalCount int
	// streamRecoveryCount limits stream-interrupted recovery attempts per turn.
	streamRecoveryCount int

	// evidence is a per-user-turn ledger of host-observed tool receipts. It lets
	// complete_step validate that cited evidence happened before the claim.
	evidence *evidence.Ledger

	// Context management
	contextWindow       int
	softCompactRatio    float64
	compactRatio        float64
	compactForceRatio   float64
	recentKeep          int
	softCompactNoticed  bool
	compactStuck        bool
	compactProvider     provider.Provider // model-based compaction (nil = sliding window)
	consecutiveCompacts int
}

// Options configures a new Agent.
type Options struct {
	MaxSteps          int
	Temperature       float64
	Pricing           *provider.Pricing
	ContextWindow     int
	SoftCompactRatio  float64
	CompactRatio      float64
	CompactForceRatio float64
	RecentKeep        int
	Sink              event.Sink
	Gate              Gate
	Asker             Asker
}

// New creates an Agent.
func New(prov provider.Provider, tools *tool.Registry, session *Session, opts Options) *Agent {
	if opts.RecentKeep <= 0 {
		opts.RecentKeep = 3
	}
	if opts.ContextWindow <= 0 {
		opts.ContextWindow = 128000
	}
	if opts.SoftCompactRatio <= 0 {
		opts.SoftCompactRatio = 0.5
	}
	if opts.CompactRatio <= 0 {
		opts.CompactRatio = 0.8
	}
	if opts.CompactForceRatio <= 0 {
		opts.CompactForceRatio = 1.0
	}
	return &Agent{
		prov:              prov,
		tools:             tools,
		session:           session,
		maxSteps:          opts.MaxSteps,
		temperature:       opts.Temperature,
		pricing:           opts.Pricing,
		sink:              opts.Sink,
		gate:              opts.Gate,
		asker:             opts.Asker,
		contextWindow:     opts.ContextWindow,
		softCompactRatio:  opts.SoftCompactRatio,
		compactRatio:      opts.CompactRatio,
		compactForceRatio: opts.CompactForceRatio,
		recentKeep:        opts.RecentKeep,
		cache:             newCacheTracker(),
	}
}

// SetPlanMode flips the read-only gate. While true, executeOne refuses any
// non-ReadOnly tool. Cache-friendly — nothing changes in the prompt.
func (a *Agent) SetPlanMode(v bool) { a.planMode.Store(v) }

// IsPlanMode reports whether the agent is currently in plan mode.
func (a *Agent) IsPlanMode() bool { return a.planMode.Load() }

// SetGate installs the per-call permission gate.
func (a *Agent) SetGate(g Gate) { a.gate = g }

// SetAsker installs the asker for the `ask` tool.
func (a *Agent) SetAsker(as Asker) { a.asker = as }

// SetPreEditHook installs the pre-edit snapshot hook.
func (a *Agent) SetPreEditHook(fn func(diff.Change)) { a.onPreEdit = fn }

// SetCheckpoint installs a checkpoint store and wires onPreEdit to feed it.
// Call before Run(); a nil store disables checkpointing.
func (a *Agent) SetCheckpoint(s *checkpoint.Store) {
	a.checkpoint = s
	if s != nil {
		a.onPreEdit = func(ch diff.Change) { s.SaveFromChange(ch) }
	} else {
		a.onPreEdit = nil
	}
}

// Checkpoint returns the current turn's checkpoint store, or nil.
func (a *Agent) Checkpoint() *checkpoint.Store { return a.checkpoint }

// LastUsage returns the most recent per-turn token telemetry, or nil.
func (a *Agent) LastUsage() *provider.Usage { return a.lastUsage.Load() }

// SessionCache returns the aggregate cache-hit and cache-miss tokens for the session.
func (a *Agent) SessionCache() (hit, miss int64) {
	return a.sessCacheHit.Load(), a.sessCacheMiss.Load()
}

// CacheReasons returns the recorded prefix-churn reasons for diagnostics.
func (a *Agent) CacheReasons() []string {
	if a.cache == nil {
		return nil
	}
	return a.cache.reasons()
}

// InvalidateSchemaCache discards the cached tool schemas so the next
// API call picks up newly registered tools (e.g. after MCP server connect).
func (a *Agent) InvalidateSchemaCache() { a.cachedSchemas = nil }

// SetJobs installs the session's background job manager.
func (a *Agent) SetJobs(jm *jobs.Manager) { a.jobs = jm }

// SetCompactProvider installs a compact model for context summarization.
// When nil (default), the agent falls back to sliding-window compaction.
func (a *Agent) SetCompactProvider(prov provider.Provider) { a.compactProvider = prov }

// SetSink replaces the event sink (used by TUI to redirect output mid-session).
func (a *Agent) SetSink(s event.Sink) { a.sink = s }

// Session returns the agent's current session.
func (a *Agent) Session() *Session {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	return a.session
}

// SetSession replaces the agent's session (used for resume).
func (a *Agent) SetSession(s *Session) {
	a.sessMu.Lock()
	a.session = s
	a.sessMu.Unlock()
}

// ── Run: the main loop ────────────────────────────────────

// Run executes one user turn. It streams a completion, executes any tool calls,
// feeds results back, and repeats until the model produces a final answer or
// maxSteps is exhausted.
func (a *Agent) Run(ctx context.Context, input string) error {
	// Strip hidden/invisible Unicode characters (indirect injection defense)
	input = guard.StripHiddenChars(input)
	if a.sink == nil {
		a.sink = event.Discard
	}
	// Fresh evidence ledger for this user turn
	a.evidence = evidence.NewLedger()
	a.repeatSuccessCounts = nil
	a.emptyFinalCount = 0
	a.streamRecoveryCount = 0
	a.sink.Emit(event.Event{Kind: event.TurnStarted, Timestamp: time.Now()})

	// Ensure the session starts with a system prompt (cache-stable prefix).
	// Only add it once — the session may already have one from a resume.
	if a.session.Len() == 0 {
		a.session.Add(provider.Message{Role: provider.RoleSystem, Content: DefaultSystemPrompt})
	}
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: input})

	for step := 0; step < a.maxSteps; step++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 1. Auto-compact before the prompt nears the context window
		a.autoCompact()

		// 2. Build request — sanitize messages to satisfy tool-call pairing contract.
		// Only sanitize when the last assistant message had tool calls (the common
		// case for unpaired calls); otherwise the snapshot is already clean.
		snapshot := a.session.Snapshot()
		needsRepair := len(snapshot) > 0 && snapshot[len(snapshot)-1].Role == provider.RoleTool
		if needsRepair {
			snapshot = provider.SanitizeToolPairing(snapshot)
		}

		// 3. Cache schemas — compute once per agent lifetime, reuse every turn
		if a.cachedSchemas == nil {
			a.cachedSchemas = a.tools.Schemas()
		}

		// 4. Check prefix-cache stability before the API call
		if a.cache != nil {
			firstUser := ""
			for _, m := range snapshot {
				if m.Role == provider.RoleUser || m.Role == provider.RoleSystem {
					firstUser += m.Content
					break
				}
			}
			_, churn := a.cache.check(DefaultSystemPrompt, a.cachedSchemas, firstUser, false)
			if churn {
				a.sink.Emit(event.Event{
					Kind: event.Notice, Level: event.LevelInfo,
					Text: "prefix cache churn detected — next turn may have reduced cache hit rate",
				})
			}
		}

		req := provider.Request{
			Messages:    snapshot,
			Tools:       a.cachedSchemas,
			Temperature: a.temperature,
		}

		// 5. Stream the completion
		ch, err := a.prov.Stream(ctx, req)
		if err != nil {
			// Recovery: interrupted stream after some output already delivered
			if a.handleStreamRecovery(err) {
				continue
			}
			return fmt.Errorf("stream: %w", err)
		}

		// 4. Collect text and tool calls
		var (
			textBuf   strings.Builder
			toolCalls []provider.ToolCall
			usage     *provider.Usage
			reasonBuf strings.Builder
		)

		for chunk := range ch {
			switch chunk.Type {
			case provider.ChunkText:
				textBuf.WriteString(chunk.Text)
				a.sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text, Timestamp: time.Now()})
			case provider.ChunkReasoning:
				reasonBuf.WriteString(chunk.Text)
				a.sink.Emit(event.Event{Kind: event.Reasoning, Text: chunk.Text, Timestamp: time.Now()})
			case provider.ChunkToolCall:
				toolCalls = append(toolCalls, *chunk.ToolCall)
				// ToolCall delivers the complete call — dispatch once with full args.
				// (ChunkToolCallStart was already dispatched without args; skip re-dispatch.)
			case provider.ChunkToolCallStart:
				// Dispatch the start of a tool call (ID + Name, no args yet)
				a.sink.Emit(event.Event{
					Kind: event.ToolDispatch,
					Tool: event.Tool{
						ID:       chunk.ToolCall.ID,
						Name:     chunk.ToolCall.Name,
						ReadOnly: a.toolReadOnly(chunk.ToolCall.Name),
					},
					Timestamp: time.Now(),
				})
			case provider.ChunkUsage:
				usage = chunk.Usage
			case provider.ChunkError:
				return chunk.Err
			}
		}

		// Track usage
		if usage != nil {
			a.lastUsage.Store(usage)
			a.sessCacheHit.Add(int64(usage.CacheHitTokens))
			a.sessCacheMiss.Add(int64(usage.CacheMissTokens))
			a.sink.Emit(event.Event{Kind: event.UsageKind, Usage: convertUsage(usage), Timestamp: time.Now()})
		}

		text := textBuf.String()
		reasoning := reasonBuf.String()

		// 5. If no tool calls → check readiness, then final answer
		if len(toolCalls) == 0 {
			// 5a. Empty final guard — model produced no text at all
			if a.handleEmptyFinal(text) {
				continue // retry with a nudge
			}
			// 5b. Check whether the model has actually finished its work
			if !a.finalAnswerReady(text) {
				continue // retry with a prompt to finish
			}
			a.session.Add(provider.Message{
				Role:             provider.RoleAssistant,
				Content:          text,
				ReasoningContent: reasoning,
			})
			a.sink.Emit(event.Event{Kind: event.TurnDone, Timestamp: time.Now()})
			return nil
		}

		// 6. Record assistant message with tool calls
		a.session.Add(provider.Message{
			Role:             provider.RoleAssistant,
			Content:          text,
			ReasoningContent: reasoning,
			ToolCalls:        toolCalls,
		})

		// 7. Execute tool calls (partitioned: read-only in parallel, writers serial)
		batches := partitionToolCalls(a.tools, toolCalls)
		for _, batch := range batches {
			results := make([]toolOutcome, len(batch.calls))

			if batch.parallel {
				a.executeParallel(ctx, batch.calls, results)
			} else {
				for i, tc := range batch.calls {
					results[i] = a.executeOne(ctx, tc)
				}
			}

			// Storm breaker: detect identical repeated failures
			a.applyStormBreaker(batch.calls, results)

			// Emit results and add to session
			for i, outcome := range results {
				tc := batch.calls[i]
				ev := event.Event{
					Kind: event.ToolResult,
					Tool: event.Tool{
						ID:        tc.ID,
						Name:      tc.Name,
						Output:    outcome.output,
						Err:       outcome.errMsg,
						Blocked:   outcome.blocked,
						Truncated: outcome.truncated,
						ReadOnly:  a.toolReadOnly(tc.Name),
					},
					Timestamp: time.Now(),
				}
				a.sink.Emit(ev)

				a.session.Add(provider.Message{
					Role:       provider.RoleTool,
					Content:    outcome.output,
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
			}
		}
	}

	// Max steps exhausted
	a.sink.Emit(event.Event{
		Kind:      event.Notice,
		Level:     event.LevelWarn,
		Text:      fmt.Sprintf("max steps (%d) reached — stopping", a.maxSteps),
		Timestamp: time.Now(),
	})
	a.sink.Emit(event.Event{Kind: event.TurnDone, Timestamp: time.Now()})
	return nil
}

// ── Tool execution ────────────────────────────────────────

type toolCallBatch struct {
	calls    []provider.ToolCall
	parallel bool
}

type toolOutcome struct {
	output    string
	blocked   bool
	errMsg    string
	truncated bool
}

// executeOne runs a single tool call through the full gate→pre-edit→execute→post pipeline.
func (a *Agent) executeOne(ctx context.Context, call provider.ToolCall) toolOutcome {
	// Stamp evidence ledger onto context so complete_step can validate
	if a.evidence != nil {
		ctx = evidence.WithLedger(ctx, a.evidence)
	}
	// Stamp jobs manager onto context so bash/task background tools can access it
	if a.jobs != nil {
		ctx = jobs.WithManager(ctx, a.jobs)
	}
	// Stamp this call's identity + sink so sub-agent-spawning tools (task,
	// run_skill) nest their child events under this call instead of discarding.
	ctx = withCallContext(ctx, call.ID, a.sink, a.asker, a.planMode.Load())

	t, ok := a.tools.Get(call.Name)
	if !ok {
		return toolOutcome{
			output: fmt.Sprintf("error: unknown tool %q", call.Name),
			errMsg: fmt.Sprintf("unknown tool %q", call.Name),
		}
	}

	// Plan mode gate: refuse writer tools without changing the prompt
	if a.planMode.Load() && !t.ReadOnly() {
		return toolOutcome{
			output: fmt.Sprintf(
				"blocked: %q is a writer tool and plan mode is read-only. "+
					"Keep exploring with read-only tools, then write your plan as your reply — "+
					"the user will be asked to approve it before any changes are made.",
				call.Name),
			blocked: true,
			errMsg:  "blocked: plan mode is read-only",
		}
	}

	// Permission gate
	if a.gate != nil {
		allow, reason, err := a.gate.Check(ctx, call.Name, json.RawMessage(call.Arguments), t.ReadOnly())
		if err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: %s (%v)", reason, err),
				blocked: true,
				errMsg:  fmt.Sprintf("blocked: %v", err),
			}
		}
		if !allow {
			return toolOutcome{
				output:  "blocked: " + reason,
				blocked: true,
				errMsg:  "blocked by permission policy",
			}
		}
	}

	// Pre-edit snapshot
	if a.onPreEdit != nil && !t.ReadOnly() {
		if pv, ok := t.(tool.Previewer); ok {
			if change, err := pv.Preview(json.RawMessage(call.Arguments)); err == nil {
				a.onPreEdit(change)
			}
		}
	}

	result, err := t.Execute(ctx, json.RawMessage(call.Arguments))
	// Record evidence receipt for host-observable validation
	if a.evidence != nil {
		rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, t.ReadOnly())
		a.evidence.Record(rec)
	}
	if err != nil {
		detail := result
		if !json.Valid([]byte(call.Arguments)) {
			detail = strings.TrimRight(detail, "\n") + "\nThe arguments were not valid JSON. Re-emit them per this schema:\n" + string(t.Schema())
		}
		body, truncMsg := truncateToolOutput(fmt.Sprintf("error: %v\n%s", err, detail))
		return toolOutcome{output: body, errMsg: firstLine(err.Error()), truncated: truncMsg != ""}
	}

	a.recordRepeatSuccess(call, t)
	body, truncMsg := truncateToolOutput(result)
	return toolOutcome{output: body, truncated: truncMsg != ""}
}

func (a *Agent) executeParallel(ctx context.Context, calls []provider.ToolCall, results []toolOutcome) {
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i := range calls {
		i := i
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = a.executeOne(ctx, calls[i])
		}()
	}
	wg.Wait()
}

func (a *Agent) toolReadOnly(name string) bool {
	t, ok := a.tools.Get(name)
	return ok && t.ReadOnly()
}

// ── Tool call partitioning ─────────────────────────────────

func partitionToolCalls(r *tool.Registry, calls []provider.ToolCall) []toolCallBatch {
	var batches []toolCallBatch
	for i := 0; i < len(calls); {
		if parallelisable(r, calls[i].Name) {
			start := i
			i++
			for i < len(calls) && parallelisable(r, calls[i].Name) {
				i++
			}
			batches = append(batches, toolCallBatch{calls: calls[start:i], parallel: true})
			continue
		}
		batches = append(batches, toolCallBatch{calls: calls[i : i+1]})
		i++
	}
	return batches
}

func parallelisable(r *tool.Registry, name string) bool {
	if name == "complete_step" || name == "todo_write" {
		return false
	}
	t, ok := r.Get(name)
	return ok && t.ReadOnly()
}

// ── Storm breaker (death-spiral detection) ─────────────────

func (a *Agent) applyStormBreaker(calls []provider.ToolCall, outcomes []toolOutcome) {
	sig, ok := batchStormSignature(calls, outcomes)
	if !ok {
		a.stormSig, a.stormCount = "", 0
		return
	}
	if sig != a.stormSig {
		a.stormSig, a.stormCount = sig, 1
		return
	}
	a.stormCount++
	if a.stormCount < stormBreakThreshold {
		return
	}
	subject := fmt.Sprintf("%q", calls[0].Name)
	if len(calls) > 1 {
		subject = fmt.Sprintf("this batch of %d tool calls", len(calls))
	}
	outcomes[0].output += fmt.Sprintf(
		"\n\n[loop guard] %s has now failed %d times in a row with the same error. "+
			"Change approach: fix the arguments, use a different tool, or explain the blocker in your final answer.",
		subject, a.stormCount)
}

func batchStormSignature(calls []provider.ToolCall, outcomes []toolOutcome) (string, bool) {
	if len(calls) == 0 {
		return "", false
	}
	var sb strings.Builder
	for i := range calls {
		if outcomes[i].errMsg == "" || outcomes[i].blocked {
			return "", false
		}
		sb.WriteString(calls[i].Name)
		sb.WriteByte(0)
		sb.WriteString(outcomes[i].errMsg)
		sb.WriteByte(0)
	}
	return sb.String(), true
}

// ── Repeat success guard ───────────────────────────────────

func (a *Agent) recordRepeatSuccess(call provider.ToolCall, t tool.Tool) {
	if t.ReadOnly() {
		return
	}
	sig := repeatSuccessSignature(call, t)
	if sig == "" {
		return
	}
	if a.repeatSuccessCounts == nil {
		a.repeatSuccessCounts = make(map[string]int)
	}
	a.repeatSuccessCounts[sig]++
}

func repeatSuccessSignature(call provider.ToolCall, t tool.Tool) string {
	if t.ReadOnly() {
		return ""
	}
	switch call.Name {
	case "write_file", "edit_file":
		return call.Name + "\x00" + call.Arguments
	default:
		return ""
	}
}

// ── Auto-compaction ────────────────────────────────────────

func (a *Agent) autoCompact() {
	if a.compactStuck || a.contextWindow <= 0 {
		return
	}
	// Estimate tokens from character count (~4 chars/token for English,
	// ~2 chars/token for CJK). Under-estimate slightly to compact before
	// the actual limit is hit.
	msgs := a.session.Snapshot()
	totalChars := 0
	for _, m := range msgs {
		totalChars += len(m.Content) + len(m.ReasoningContent)
		for _, tc := range m.ToolCalls {
			totalChars += len(tc.Arguments)
		}
	}
	estimatedTokens := totalChars / 3 // conservative for mixed content

	hardLimit := int(float64(a.contextWindow) * a.compactRatio)
	softLimit := int(float64(a.contextWindow) * a.softCompactRatio)

	if a.recentKeep > 0 && estimatedTokens > hardLimit {
		// Prefer model-based compaction when a compact provider is available
		if a.compactProvider != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := a.CompactWithModel(ctx, a.compactProvider, a.recentKeep, a.recentKeep); err != nil {
				a.sink.Emit(event.Event{
					Kind: event.Notice, Level: event.LevelWarn,
					Text: fmt.Sprintf("model compaction failed, falling back to sliding window: %v", err),
				})
				a.session.Compact(a.recentKeep, a.recentKeep,
					fmt.Sprintf("[auto-compacted at ~%d tokens: session exceeded the %d-token threshold. "+
						"Earlier messages were dropped (sliding window) to fit the context window; "+
						"the opening and most recent messages are preserved verbatim. "+
						"If you need omitted detail, re-read the relevant files.]",
						estimatedTokens, hardLimit))
			}
			a.consecutiveCompacts++
			return
		}
		// Fallback: pure sliding window
		a.session.Compact(a.recentKeep, a.recentKeep,
			fmt.Sprintf("[auto-compacted at ~%d tokens: session exceeded the %d-token threshold. "+
				"Earlier messages were dropped (sliding window) to fit the context window; "+
				"the opening and most recent messages are preserved verbatim. "+
				"If you need omitted detail, re-read the relevant files.]",
				estimatedTokens, hardLimit))
	}
	if estimatedTokens > softLimit && !a.softCompactNoticed {
		a.softCompactNoticed = true
		a.sink.Emit(event.Event{
			Kind:  event.Notice,
			Level: event.LevelInfo,
			Text:  fmt.Sprintf("context approaching limit (~%d / %d tokens)", estimatedTokens, a.contextWindow),
		})
	}
}

// ── Final-answer readiness guards ──────────────────────────

// finalAnswerReady checks whether the model's final answer actually concludes
// the work. Short text without a done marker suggests the model stopped
// prematurely. Returns false to force another loop iteration with a nudge.
func (a *Agent) finalAnswerReady(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}

	// Any non-empty answer at least one character long is accepted.
	// The empty-final guard (handleEmptyFinal) already catches the truly
	// empty case, and the storm-breaker prevents infinite loops.
	// We rely on the model's judgment — our job is to keep it from
	// silently producing nothing, not to second-guess short answers.
	return true
}

// handleEmptyFinal detects a completely empty final answer and nudges the
// model to produce a real response. Returns true when it nudged.
func (a *Agent) handleEmptyFinal(text string) bool {
	if strings.TrimSpace(text) != "" {
		return false
	}
	a.emptyFinalCount++
	if a.emptyFinalCount > maxEmptyFinalBlocks {
		a.session.Add(provider.Message{
			Role:    provider.RoleUser,
			Content: "[system: you have produced multiple empty responses. Write a brief message explaining what you were trying to do, then end.]",
		})
		return false
	}
	a.session.Add(provider.Message{
		Role:    provider.RoleUser,
		Content: "[system: your response was empty. If you are finished, say so. Otherwise continue.]",
	})
	a.sink.Emit(event.Event{
		Kind: event.Notice, Level: event.LevelWarn,
		Text: "empty assistant response — nudging model",
	})
	return true
}

// handleStreamRecovery attempts to recover from a stream interruption by
// appending a recovery prompt. Returns true when a recovery was attempted.
func (a *Agent) handleStreamRecovery(err error) bool {
	if err == nil || !provider.IsStreamInterrupted(err) {
		return false
	}
	if a.streamRecoveryCount >= maxStreamRecoveries {
		return false
	}
	a.streamRecoveryCount++
	a.session.Add(provider.Message{
		Role:    provider.RoleUser,
		Content: "[system: the previous response was interrupted mid-stream. Continue from where you left off without repeating completed work. Re-state the last complete sentence, then proceed.]",
	})
	a.sink.Emit(event.Event{
		Kind: event.Notice, Level: event.LevelWarn,
		Text: "stream interrupted — recovering once",
	})
	return true
}

// ── Helpers ────────────────────────────────────────────────

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func truncateToolOutput(s string) (string, string) {
	if len(s) <= maxToolOutputBytes {
		return s, ""
	}
	keep := maxToolOutputBytes / 2
	head := snapToRuneBoundary(s, 0, keep)
	tail := snapToRuneBoundary(s, len(s)-keep, len(s))
	omitted := len(s) - len(head) - len(tail)
	body := head + fmt.Sprintf(
		"\n\n…[truncated %d of %d bytes — rerun with narrower args to see the middle]…\n\n",
		omitted, len(s)) + tail
	notice := fmt.Sprintf("tool output truncated: %d of %d bytes elided", omitted, len(s))
	return body, notice
}

func snapToRuneBoundary(s string, lo, hi int) string {
	for lo > 0 && !utf8.RuneStart(s[lo]) {
		lo--
	}
	for hi < len(s) && !utf8.RuneStart(s[hi]) {
		hi++
	}
	return s[lo:hi]
}

func convertUsage(u *provider.Usage) *event.Usage {
	if u == nil {
		return nil
	}
	return &event.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CacheHitTokens:   u.CacheHitTokens,
		CacheMissTokens:  u.CacheMissTokens,
		FinishReason:     u.FinishReason,
	}
}
