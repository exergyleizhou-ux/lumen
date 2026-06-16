// Package control provides a transport-agnostic Controller that sits between
// every frontend (CLI chat, one-shot run, HTTP/SSE serve, Wails desktop) and
// the agent. Frontends call Configure() once then Run()/Chat()/Plan(); the
// controller owns provider resolution, tool/skill wiring, session lifecycle,
// and permission gating so no frontend duplicates that logic.
package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"lumen/internal/agent"
	"lumen/internal/checkpoint"
	"lumen/internal/config"
	"lumen/internal/event"
	"lumen/internal/jobs"
	"lumen/internal/permission"
	"lumen/internal/provider"
	"lumen/internal/skill"
	"lumen/internal/timeline"
	"lumen/internal/tool"

	// Side-effect: register builtin tools and providers
	_ "lumen/internal/provider/openai"
	_ "lumen/internal/tool/builtin"
)

// Mode selects the agent run mode.
type Mode string

const (
	ModeRun  Mode = "run"  // one-shot execution
	ModePlan Mode = "plan" // plan-only (read-only tools)
	ModeChat Mode = "chat" // interactive REPL
)

// Controller owns the full agent lifecycle for one session.
// Create with New, call Configure, then Run/Chat/Plan.
type Controller struct {
	// Configuration (set by Configure)
	cfg        *config.File
	provCfg    *config.ProviderConfig
	prov       provider.Provider
	fallbacks  []provider.Provider // for failover
	reg        *tool.Registry
	skillStore *skill.Store
	permMode    permission.Mode
	sink        event.Sink
	asker       agent.Asker
	autoApprove func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) // terminal auto-approve

	// Agent (created by Configure)
	ag   *agent.Agent
	sess *agent.Session
	cp   *checkpoint.Store
	jm   *jobs.Manager
	tl   *timeline.Recorder // session timeline for replay + change inbox

	// Sub-agent deps (shared by run_skill / task tools)
	subDeps agent.SubagentDeps
}

// New creates an unconfigured Controller. Call Configure() before use.
func New() *Controller {
	return &Controller{}
}

// Configure resolves config, providers, tools, skills, permissions, and
// creates the agent. Sink receives all agent events; Asker handles interactive
// questions (nil for headless runs). Call once, before Run/Chat/Plan.
func (c *Controller) Configure(sink event.Sink, asker agent.Asker, cfgPath string) error {
	if sink == nil {
		sink = event.Discard
	}
	c.sink = sink
	c.asker = asker

	// 1. Load config
	path := cfgPath
	if path == "" {
		path = config.FindConfig()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	c.cfg = cfg

	// 2. Resolve default provider
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("no providers configured — add one in lumen.toml or run 'lumen setup'")
	}
	var provCfg *config.ProviderConfig
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == cfg.DefaultModel {
			provCfg = &cfg.Providers[i]
			break
		}
	}
	if provCfg == nil {
		provCfg = &cfg.Providers[0]
	}
	c.provCfg = provCfg

	prov, err := provider.New(provCfg.Kind, provider.Config{
		Name:    provCfg.Name,
		BaseURL: provCfg.BaseURL,
		Model:   provCfg.Model,
		APIKey:  provCfg.APIKey,
	})
	if err != nil {
		return fmt.Errorf("provider %s: %w", provCfg.Name, err)
	}
	c.prov = prov

	// 2b. Build fallback providers from remaining configs
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == provCfg.Name {
			continue
		}
		fb, err := provider.New(cfg.Providers[i].Kind, provider.Config{
			Name:    cfg.Providers[i].Name,
			BaseURL: cfg.Providers[i].BaseURL,
			Model:   cfg.Providers[i].Model,
			APIKey:  cfg.Providers[i].APIKey,
		})
		if err == nil {
			c.fallbacks = append(c.fallbacks, fb)
		}
	}

	// 3. Build tool registry
	reg := tool.NewRegistry()
	for _, t := range tool.Builtins() {
		reg.Add(t)
	}

	// 4. Build skill store
	wd, _ := os.Getwd()
	c.skillStore = skill.New(skill.Options{ProjectRoot: wd})
	skills := c.skillStore.List()
	_ = skills
	// 5. Resolve permission mode
	c.permMode = permission.ParseMode(cfg.Permissions.Mode)

	// 6. Build permission gate (auto-approve in terminal mode — guard.CheckBash
	// still blocks known-dangerous patterns regardless)
	autoApprove := func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		return true, nil
	}
	gate := permission.NewGate(c.permMode, autoApprove)
	c.autoApprove = autoApprove

	// 7. Wire sub-agent dependencies (shared by run_skill and task tools)
	c.subDeps = agent.SubagentDeps{
		Prov:          prov,
		ParentReg:     reg,
		MaxSteps:      cfg.Agent.MaxSteps,
		ContextWindow: cfg.Agent.ContextWindow,
		Temperature:   cfg.Agent.Temperature,
		Gate:          gate,
	}
	reg.Add(agent.NewSkillTool(c.skillStore, c.subDeps))
	reg.Add(agent.NewTaskTool(prov, nil, reg,
		cfg.Agent.MaxSteps, cfg.Agent.ContextWindow,
		cfg.Agent.Temperature, "", gate, "", "", nil))
	c.reg = reg

	// 8. Init timeline recorder (session replay + change inbox)
	tl, err := timeline.NewRecorder(".lumen/timeline.jsonl")
	if err != nil {
		c.logf("timeline: %v (replay disabled)", err)
	} else {
		c.tl = tl
		// Wrap sink to auto-record events
		tlSink := &timelineSink{inner: sink, tl: tl}
		sink = tlSink
	}

	// 9. Create session
	c.sess = agent.NewSession("")

	// 10. Create agent
	c.ag = agent.New(prov, reg, c.sess, agent.Options{
		MaxSteps:      cfg.Agent.MaxSteps,
		Temperature:   cfg.Agent.Temperature,
		ContextWindow: cfg.Agent.ContextWindow,
		Sink:          sink,
		Gate:          gate,
		Asker:         asker,
	})

	// 11. Wire infrastructure
	c.cp = checkpoint.New()
	c.ag.SetCheckpoint(c.cp)
	c.jm = jobs.NewManager()
	c.ag.SetJobs(c.jm)

	return nil
}

// Run executes a one-shot task and returns the agent's final answer.
// On failure, automatically tries fallback providers if configured.
func (c *Controller) Run(ctx context.Context, prompt string) error {
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.prov.Name() + " · executing"})
	err := c.ag.Run(ctx, prompt)
	if err != nil && len(c.fallbacks) > 0 {
		original := c.prov.Name()
		for _, fb := range c.fallbacks {
			c.sink.Emit(event.Event{
				Kind: event.Notice, Level: event.LevelWarn,
				Text: fmt.Sprintf(original + " failed — switching to " + fb.Name()),
			})
			c.ag.SetProvider(fb)
			err2 := c.ag.Run(ctx, prompt)
			if err2 == nil {
				return nil
			}
		}
		c.ag.SetProvider(c.prov) // restore
	}
	return err
}

// Plan runs in read-only mode and returns the agent's plan.
func (c *Controller) Plan(ctx context.Context, prompt string) error {
	c.ag.SetPlanMode(true)
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.prov.Name() + " · planning (read-only)"})
	return c.ag.Run(ctx, prompt)
}

// Chat runs an interactive session. (TUI placeholder — falls back to Run)
func (c *Controller) Chat(ctx context.Context, prompt string) error {
	return c.Run(ctx, prompt)
}

// ── Accessors ──────────────────────────────────────────────

// Agent returns the underlying agent (for direct access when needed).
func (c *Controller) Agent() *agent.Agent { return c.ag }

// Session returns the agent's session.
func (c *Controller) Session() *agent.Session { return c.sess }

// SetSink replaces the event sink at runtime (used by SSE/TUI to redirect events).
func (c *Controller) SetSink(s event.Sink) {
	c.sink = s
	if c.ag != nil {
		c.ag.SetSink(s)
	}
}

// ProviderName returns the active provider instance name.
func (c *Controller) ProviderName() string { return c.provCfg.Name }

// ModelName returns the active model ID.
func (c *Controller) ModelName() string { return c.provCfg.Model }

// PermissionMode returns the resolved permission mode.
func (c *Controller) PermissionMode() permission.Mode { return c.permMode }

// Checkpoint returns the turn's checkpoint store.
func (c *Controller) Checkpoint() *checkpoint.Store { return c.cp }

// Skills returns the skill store.
func (c *Controller) Skills() *skill.Store { return c.skillStore }

// SetPermissionMode overrides the permission mode at runtime.
func (c *Controller) SetPermissionMode(m permission.Mode) {
	c.permMode = m
	if c.ag != nil {
		c.ag.SetGate(permission.NewGate(m, c.autoApprove))
	}
}

// SwitchModel hot-swaps the active provider at runtime.
func (c *Controller) SwitchModel(name string) (string, error) {
	preset := config.FindPreset(name)
	if preset == nil {
		return "", fmt.Errorf("model %q not found — use /models to list", name)
	}

	// Try env var first, fall back to current provider's key
	apiKey := os.Getenv(strings.ToUpper(strings.ReplaceAll(preset.Provider, "-", "_")) + "_API_KEY")
	if apiKey == "" {
		apiKey = c.provCfg.APIKey
	}

	newProv, err := provider.New(preset.Kind, provider.Config{
		Name:    preset.Name,
		BaseURL: preset.BaseURL,
		Model:   preset.Model,
		APIKey:  apiKey,
	})
	if err != nil {
		return "", fmt.Errorf("create provider: %w", err)
	}

	c.provCfg = &config.ProviderConfig{
		Name:    preset.Name,
		Kind:    preset.Kind,
		BaseURL: preset.BaseURL,
		Model:   preset.Model,
		APIKey:  apiKey,
	}
	c.prov = newProv
	if c.ag != nil {
		c.ag.SetProvider(newProv)
	}
	return preset.Name, nil
}

// SetAsker installs an interactive asker (for TUI mode).
func (c *Controller) SetAsker(asker agent.Asker) {
	c.asker = asker
	if c.ag != nil {
		c.ag.SetAsker(asker)
	}
}

// Rewind restores all files to their pre-turn state.
func (c *Controller) Rewind() ([]string, error) {
	if c.cp == nil {
		return nil, fmt.Errorf("no checkpoint store")
	}
	return c.cp.Rewind()
}

// logf prints a diagnostic line to stderr (like Reasonix's bootstrap output).
func (c *Controller) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// TimelinePath returns the path to the session timeline file, or "".
func (c *Controller) TimelinePath() string {
	if c.tl == nil {
		return ""
	}
	return ".lumen/timeline.jsonl"
}

// Close shuts down infrastructure (timeline, jobs, etc).
func (c *Controller) Close() {
	if c.tl != nil {
		c.tl.Close()
	}
}

// ── timelineSink wraps event.Sink to auto-record timeline entries ─

type timelineSink struct {
	inner event.Sink
	tl    *timeline.Recorder
}

func (s *timelineSink) Emit(e event.Event) {
	s.tl.RecordEvent(e)
	s.inner.Emit(e)
}

var _ event.Sink = (*timelineSink)(nil)
