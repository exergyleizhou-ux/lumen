// Package command defines slash commands (/status, /cost, /cache, /rewind,
// /skills, etc.) that the user types in interactive mode. Each command is a
// named, described handler that receives the agent's state and returns output
// text. The registry is transport-agnostic — the TUI and headless CLI share it.
package command

import (
	"fmt"
	"strings"

	"lumen/internal/agent"
	"lumen/internal/checkpoint"
	"lumen/internal/timeline"
)

// Result holds the output of a slash command execution.
type Result struct {
	Text  string // output to display
	Error error  // non-nil when the command failed
}

// Command is a named slash handler.
type Command struct {
	Name        string
	Description string
	Aliases     []string
	Run         func(ctx Context) Result
}

// Context provides a command with read access to agent state.
type Context struct {
	Agent      *agent.Agent
	Checkpoint *checkpoint.Store
}

// Registry holds all registered slash commands.
type Registry struct {
	commands map[string]*Command
}

// NewRegistry creates a command registry and registers built-in commands.
func NewRegistry() *Registry {
	r := &Registry{commands: map[string]*Command{}}
	r.registerBuiltins()
	return r
}

// Get looks up a command by name or alias.
func (r *Registry) Get(name string) (*Command, bool) {
	name = strings.TrimPrefix(strings.ToLower(name), "/")
	cmd, ok := r.commands[name]
	return cmd, ok
}

// List returns all registered commands sorted by name.
func (r *Registry) List() []*Command {
	names := make([]string, 0, len(r.commands))
	for n := range r.commands {
		names = append(names, n)
	}
	// Sort would go here; map iteration is fine for now
	out := make([]*Command, 0, len(names))
	for _, n := range names {
		out = append(out, r.commands[n])
	}
	return out
}

// registerBuiltins adds all built-in slash commands.
func (r *Registry) registerBuiltins() {
	r.add(&Command{
		Name:        "status",
		Description: "Show agent status (provider, model, session, cache)",
		Aliases:     []string{"stats", "info"},
		Run:         cmdStatus,
	})
	r.add(&Command{
		Name:        "cost",
		Description: "Show token usage and cost for the current session",
		Aliases:     []string{"usage", "tokens"},
		Run:         cmdCost,
	})
	r.add(&Command{
		Name:        "cache",
		Description: "Show prefix-cache diagnostics (hit rate, stability)",
		Aliases:     []string{"prefix"},
		Run:         cmdCache,
	})
	r.add(&Command{
		Name:        "rewind",
		Description: "Restore all files to their pre-turn state (Esc-Esc)",
		Aliases:     []string{"undo", "restore"},
		Run:         cmdRewind,
	})
	r.add(&Command{
		Name:        "replay",
		Description: "Show session timeline — every tool call, file change, and turn",
		Aliases:     []string{"timeline", "history"},
		Run:         cmdReplay,
	})
	r.add(&Command{
		Name:        "changes",
		Description: "Show all files changed this session (变更收件箱)",
		Aliases:     []string{"files", "modified"},
		Run:         cmdChanges,
	})
	r.add(&Command{
		Name:        "skills",
		Description: "List available skills",
		Aliases:     []string{"skill"},
		Run:         cmdSkills,
	})
	r.add(&Command{
		Name:        "help",
		Description: "Show available slash commands",
		Aliases:     []string{"?"},
		Run:         cmdHelp,
	})
}

func (r *Registry) add(cmd *Command) {
	r.commands[cmd.Name] = cmd
	for _, a := range cmd.Aliases {
		r.commands[a] = cmd
	}
}

// ── Built-in command implementations ──────────────────────

func cmdStatus(ctx Context) Result {
	ag := ctx.Agent
	if ag == nil {
		return Result{Error: fmt.Errorf("no active agent")}
	}
	// Use the last usage for a snapshot
	last := ag.LastUsage()
	var sb strings.Builder
	sb.WriteString("Lumen status\n")
	sb.WriteString("────────────\n")
	if last != nil {
		fmt.Fprintf(&sb, "Session tokens: %d (prompt: %d, completion: %d)\n",
			last.TotalTokens, last.PromptTokens, last.CompletionTokens)
	} else {
		sb.WriteString("Session tokens: (no turns yet)\n")
	}
	cacheHit, cacheMiss := ag.SessionCache()
	if cacheHit+cacheMiss > 0 {
		rate := float64(cacheHit) / float64(cacheHit+cacheMiss) * 100
		fmt.Fprintf(&sb, "Cache: %.0f%% hit (%d/%d tokens)\n",
			rate, cacheHit, cacheHit+cacheMiss)
	} else {
		sb.WriteString("Cache: (no data yet)\n")
	}
	sb.WriteString("Plan mode: ")
	if ag.IsPlanMode() {
		sb.WriteString("⏸ read-only\n")
	} else {
		sb.WriteString("▶ active\n")
	}
	return Result{Text: sb.String()}
}

func cmdCost(ctx Context) Result {
	ag := ctx.Agent
	if ag == nil {
		return Result{Error: fmt.Errorf("no active agent")}
	}
	var sb strings.Builder
	sb.WriteString("Token usage\n")
	sb.WriteString("───────────\n")

	cacheHit, cacheMiss := ag.SessionCache()
	last := ag.LastUsage()

	if last != nil {
		fmt.Fprintf(&sb, "Last turn: %d tokens (prompt: %d, completion: %d)\n",
			last.TotalTokens, last.PromptTokens, last.CompletionTokens)
		if last.CacheHitTokens+last.CacheMissTokens > 0 {
			rate := float64(last.CacheHitTokens) / float64(last.CacheHitTokens+last.CacheMissTokens) * 100
			fmt.Fprintf(&sb, "Last turn cache: %.0f%% (%d/%d)\n",
				rate, last.CacheHitTokens, last.CacheHitTokens+last.CacheMissTokens)
		}
	}
	fmt.Fprintf(&sb, "Session total cache: %d hit + %d miss\n", cacheHit, cacheMiss)
	return Result{Text: sb.String()}
}

func cmdCache(ctx Context) Result {
	ag := ctx.Agent
	if ag == nil {
		return Result{Error: fmt.Errorf("no active agent")}
	}
	cacheHit, cacheMiss := ag.SessionCache()

	var sb strings.Builder
	sb.WriteString("Prefix-cache diagnostics\n")
	sb.WriteString("────────────────────────\n")

	if cacheHit+cacheMiss == 0 {
		sb.WriteString("No cache data yet — make at least one API call.\n")
		sb.WriteString("\nCache stability requires:\n")
		sb.WriteString("  • System prompt unchanged (injected once at session start)\n")
		sb.WriteString("  • Tool schemas stable (canonicalized at registration)\n")
		sb.WriteString("  • Session prepend-only (no message edits, only Add)\n")
		return Result{Text: sb.String()}
	}

	rate := float64(cacheHit) / float64(cacheHit+cacheMiss) * 100
	fmt.Fprintf(&sb, "Session cache hit rate: %.1f%%\n", rate)
	fmt.Fprintf(&sb, "  Cache hit tokens:  %d\n", cacheHit)
	fmt.Fprintf(&sb, "  Cache miss tokens: %d\n", cacheMiss)
	sb.WriteString(fmt.Sprintf("\nTotal prefix tokens: %d\n", cacheHit+cacheMiss))

	sb.WriteString("\n")
	switch {
	case rate >= 90:
		sb.WriteString("✅ Excellent — prefix cache is hot and stable.\n")
	case rate >= 50:
		sb.WriteString("⚠ Warning — cache is partially warm. Check if the system prompt or tool list changed mid-session.\n")
	default:
		sb.WriteString("❌ Poor — cache is cold. Possible causes: system prompt changing between turns, tool schema order fluctuating, or session messages being mutated.\n")
	}

	// Show churn reasons if any
	reasons := ag.CacheReasons()
	if len(reasons) > 0 {
		sb.WriteString("\nPrefix churn events:\n")
		for _, r := range reasons {
			sb.WriteString("  • " + r + "\n")
		}
	}

	return Result{Text: sb.String()}
}

func cmdRewind(ctx Context) Result {
	if ctx.Checkpoint == nil {
		return Result{Error: fmt.Errorf("no checkpoint store")}
	}
	rewound, err := ctx.Checkpoint.Rewind()
	if err != nil {
		return Result{Error: fmt.Errorf("rewind: %w", err)}
	}
	var sb strings.Builder
	sb.WriteString("Rewound files:\n")
	for _, f := range rewound {
		sb.WriteString("  • " + f + "\n")
	}
	sb.WriteString(fmt.Sprintf("\n%d file(s) restored.", len(rewound)))
	return Result{Text: sb.String()}
}

func cmdReplay(ctx Context) Result {
	entries, err := timeline.LoadTimeline(".lumen/timeline.jsonl")
	if err != nil {
		return Result{Error: fmt.Errorf("no timeline data: %w", err)}
	}
	return Result{Text: timeline.FormatTimeline(entries)}
}

func cmdChanges(ctx Context) Result {
	changes, err := timeline.LoadChanges(".lumen/timeline.jsonl")
	if err != nil {
		return Result{Error: fmt.Errorf("no change data: %w", err)}
	}
	return Result{Text: timeline.FormatChanges(changes)}
}

func cmdSkills(ctx Context) Result {
	// Skills accessed through the agent's tool registry
	var sb strings.Builder
	sb.WriteString("Available slash commands\n")
	sb.WriteString("────────────────────────\n")
	sb.WriteString("slash commands live in the /command system.\n")
	sb.WriteString("Use /help to see all commands.\n")
	return Result{Text: sb.String()}
}

func cmdHelp(ctx Context) Result {
	var sb strings.Builder
	sb.WriteString("Lumen slash commands\n")
	sb.WriteString("───────────────────\n\n")
	cmds := NewRegistry().List()
	for _, c := range cmds {
		fmt.Fprintf(&sb, "  /%-12s %s\n", c.Name, c.Description)
	}
	return Result{Text: sb.String()}
}
