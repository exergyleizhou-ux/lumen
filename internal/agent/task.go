package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lumen/internal/event"
	"lumen/internal/jobs"
	"lumen/internal/provider"
	"lumen/internal/tool"
)

// DefaultTaskSystemPrompt steers a sub-agent toward focused, terse delivery.
const DefaultTaskSystemPrompt = `You are a sub-agent invoked by a parent coding agent to carry out one focused task.
Use the provided tools to investigate or act. Return a single final answer that is concise
and self-contained — the parent will see only that answer, not your tool calls or reasoning.
If you need to ask for clarification, fail with a precise question instead of guessing.`

// SubagentMetaTools are tools that spawned agents should not inherit from the
// parent registry (to prevent recursive agent nesting).
var SubagentMetaTools = []string{
	"task",
	"run_skill",
	"install_skill",
}

// TaskTool spawns a sub-agent in its own session for a focused sub-task.
type TaskTool struct {
	prov            provider.Provider
	pricing         *provider.Pricing
	parentReg       *tool.Registry
	maxSteps        int
	contextWindow   int
	temperature     float64
	sysPrompt       string
	gate            Gate
	subagentModel   string
	subagentEffort  string
	resolveProvider func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
}

// NewTaskTool wires a task tool to the parent agent's environment.
func NewTaskTool(
	prov provider.Provider,
	pricing *provider.Pricing,
	parentReg *tool.Registry,
	maxSteps, contextWindow int,
	temperature float64,
	sysPrompt string,
	gate Gate,
	subagentModel, subagentEffort string,
	resolveProvider func(string, string) (provider.Provider, *provider.Pricing, int, error),
) *TaskTool {
	if sysPrompt == "" {
		sysPrompt = DefaultTaskSystemPrompt
	}
	return &TaskTool{
		prov:            prov,
		pricing:         pricing,
		parentReg:       parentReg,
		maxSteps:        maxSteps,
		contextWindow:   contextWindow,
		temperature:     temperature,
		sysPrompt:       sysPrompt,
		gate:            gate,
		subagentModel:   subagentModel,
		subagentEffort:  subagentEffort,
		resolveProvider: resolveProvider,
	}
}

func (t *TaskTool) Name() string   { return "task" }
func (t *TaskTool) ReadOnly() bool { return false }

func (t *TaskTool) Description() string {
	return "Spawn a sub-agent for a focused sub-task. The sub-agent runs in its own session with the same provider and a filtered tool list. Only its final answer is returned. Use for (a) keeping long exploration sequences out of the parent's context budget, or (b) delegating self-contained work like 'find every place that calls X and summarise the patterns'."
}

func (t *TaskTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "prompt":{"type":"string","description":"What the sub-agent should accomplish. Be specific — the sub-agent does not see this conversation."},
  "description":{"type":"string","description":"Short label (3-7 words). Surfaced in the dispatch line so the user sees what's running."},
  "tools":{"type":"array","items":{"type":"string"},"description":"Optional tool whitelist. Subagent/skill meta-tools are still excluded."},
  "max_steps":{"type":"integer","description":"Optional cap on tool-call rounds (default: half the parent's cap, min 5).","minimum":1},
  "run_in_background":{"type":"boolean","description":"Run the sub-agent asynchronously: returns a job id immediately, collect with wait."},
  "model":{"type":"string","description":"Optional model override for the sub-agent."},
  "effort":{"type":"string","description":"Optional reasoning effort (e.g. high, max)."}
},
"required":["prompt"]
}`)
}

func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Prompt          string   `json:"prompt"`
		Description     string   `json:"description"`
		Tools           []string `json:"tools"`
		MaxSteps        int      `json:"max_steps"`
		RunInBackground bool     `json:"run_in_background"`
		Model           string   `json:"model"`
		Effort          string   `json:"effort"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	// Resolve model/effort
	modelRef := strings.TrimSpace(p.Model)
	effortRef := strings.TrimSpace(p.Effort)
	if modelRef == "" {
		modelRef = t.subagentModel
	}
	if effortRef == "" {
		effortRef = t.subagentEffort
	}

	// Resolve provider
	prov, pricing, ctxWin := t.prov, t.pricing, t.contextWindow
	if t.resolveProvider != nil && (modelRef != "" || effortRef != "") {
		var err error
		prov, pricing, ctxWin, err = t.resolveProvider(modelRef, effortRef)
		if err != nil {
			return "", fmt.Errorf("sub-agent profile: %w", err)
		}
	}

	// Build tool registry
	subReg := filterRegistry(t.parentReg, p.Tools, SubagentMetaTools...)

	// Max steps
	maxSteps := p.MaxSteps
	if maxSteps <= 0 {
		maxSteps = t.maxSteps / 2
		if maxSteps < 5 {
			maxSteps = 5
		}
	}

	// Create sub-agent session with system prompt
	subSess := NewSession("")
	subSess.Add(provider.Message{Role: provider.RoleSystem, Content: t.sysPrompt})

	// Get parent call context for event nesting
	parentID, parentSink, _, _ := CallContext(ctx)

	// Build sink that nests events under the parent call
	sink := event.Discard
	if parentSink != nil {
		sink = subSinkFor(parentID, parentSink)
	}

	// ── Background mode: hand off to jobs manager ──
	if p.RunInBackground {
		jm := jobs.FromContext(ctx)
		if jm == nil {
			return "", fmt.Errorf("run_in_background: no jobs manager available")
		}
		label := p.Description
		if label == "" {
			label = p.Prompt
		}
		if len(label) > 60 {
			label = label[:57] + "..."
		}
		// Capture locals
		prov2, reg2, sess2 := prov, subReg, subSess
		opts := Options{
			MaxSteps: maxSteps, Temperature: t.temperature,
			Pricing: pricing, ContextWindow: ctxWin,
			Gate: t.gate, Sink: sink,
		}
		prompt := p.Prompt
		jm.Start("task", label, func(bgCtx context.Context) (string, error) {
			subAgent := New(prov2, reg2, sess2, opts)
			if err := subAgent.Run(bgCtx, prompt); err != nil {
				return "", fmt.Errorf("sub-agent: %w", err)
			}
			// Extract final answer
			for i := len(sess2.Messages) - 1; i >= 0; i-- {
				m := sess2.Messages[i]
				if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
					return m.Content, nil
				}
			}
			return "(sub-agent completed, no assistant message)", nil
		})
		return fmt.Sprintf("started background task agent"), nil
	}

	// ── Synchronous mode ──

	// Run the sub-agent
	subAgent := New(prov, subReg, subSess, Options{
		MaxSteps:      maxSteps,
		Temperature:   t.temperature,
		Pricing:       pricing,
		ContextWindow: ctxWin,
		Gate:          t.gate,
		Sink:          sink,
	})

	if err := subAgent.Run(ctx, p.Prompt); err != nil {
		return "", fmt.Errorf("sub-agent: %w", err)
	}

	// Extract final answer: last assistant message with content
	for i := len(subSess.Messages) - 1; i >= 0; i-- {
		m := subSess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("sub-agent finished without producing a final answer")
}

// ── Filter registry ────────────────────────────────────────

func filterRegistry(parent *tool.Registry, names []string, exclude ...string) *tool.Registry {
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	src := names
	if len(src) == 0 {
		src = parent.Names()
	}
	for _, name := range src {
		if ex[name] {
			continue
		}
		if tl, ok := parent.Get(name); ok {
			sub.Add(tl)
		}
	}
	return sub
}

// ── Event nesting helpers ──────────────────────────────────

// CallContext extracts the parent call context from ctx (set by the agent's executeOne).
func CallContext(ctx context.Context) (parentID string, sink event.Sink, asker Asker, ok bool) {
	cc, ok := ctx.Value(callContextKey{}).(callContext)
	if !ok {
		return "", nil, nil, false
	}
	return cc.parentID, cc.sink, cc.asker, true
}

// callContextKey / callContext are used to stamp tool call identity onto the context.
type callContextKey struct{}
type callContext struct {
	parentID string
	sink     event.Sink
	asker    Asker
	planMode bool
}

// withCallContext stamps the parent call's identity and sink onto ctx so that
// sub-agents spawned by a tool (task / run_skill) can nest their tool events
// under the parent call instead of discarding them.
func withCallContext(ctx context.Context, parentID string, sink event.Sink, asker Asker, planMode bool) context.Context {
	return context.WithValue(ctx, callContextKey{}, callContext{
		parentID: parentID,
		sink:     sink,
		asker:    asker,
		planMode: planMode,
	})
}

// subSinkFor builds a nesting sink that forwards sub-agent tool events under a parent call.
func subSinkFor(parentID string, parent event.Sink) event.Sink {
	if parent == nil {
		return event.Discard
	}
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ToolDispatch, event.ToolResult:
			e.Tool.ParentID = parentID
			e.Tool.ID = parentID + "/" + e.Tool.ID
			parent.Emit(e)
		}
	})
}
