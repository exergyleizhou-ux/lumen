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
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"lumen/internal/agent"
	"lumen/internal/checkpoint"
	"lumen/internal/config"
	"lumen/internal/editverify"
	"lumen/internal/event"
	"lumen/internal/jobs"
	"lumen/internal/memory"
	"lumen/internal/modelpool"
	"lumen/internal/permission"
	"lumen/internal/provider"
	"lumen/internal/skill"
	"lumen/internal/timeline"
	"lumen/internal/tool"
	"lumen/internal/tool/builtin"

	// Side-effect: register builtin providers
	_ "lumen/internal/provider/openai"
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
	cfg     *config.File
	provCfg *config.ProviderConfig
	prov    provider.Provider
	// pricing is the active provider's configured rates (nil → use default).
	pricing     *provider.Pricing
	fallbacks   []provider.Provider // for failover
	reg         *tool.Registry
	skillStore  *skill.Store
	permMode    permission.Mode
	sinkRef     atomic.Pointer[event.Sink] // via sink()/store; safe vs a mid-turn redirect
	asker       agent.Asker
	autoApprove func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) // terminal auto-approve

	// Agent (created by Configure)
	ag       *agent.Agent
	sess     *agent.Session
	sessPath string // JSONL path for persistence
	cp       *checkpoint.Store
	jm       *jobs.Manager
	tl       *timeline.Recorder // session timeline for replay + change inbox
	memStore *memory.Store      // persistent user memories

	// Sub-agent deps (shared by run_skill / task tools)
	subDeps agent.SubagentDeps

	persistWarned bool // session-persistence failure surfaced once
}

// New creates an unconfigured Controller. Call Configure() before use.
func New() *Controller {
	return &Controller{}
}

// sink returns the current event sink (never nil).
func (c *Controller) sink() event.Sink {
	if p := c.sinkRef.Load(); p != nil {
		return *p
	}
	return event.Discard
}

// storeSink atomically sets the sink, so a redirect can't race a turn reading it.
func (c *Controller) storeSink(s event.Sink) {
	if s == nil {
		s = event.Discard
	}
	c.sinkRef.Store(&s)
}

// Configure resolves config, providers, tools, skills, permissions, and
// creates the agent. Sink receives all agent events; Asker handles interactive
// questions (nil for headless runs). Call once, before Run/Chat/Plan.
func (c *Controller) Configure(sink event.Sink, asker agent.Asker, cfgPath string) error {
	if sink == nil {
		sink = event.Discard
	}
	// Serialize the sink: the foreground turn and any background run_in_background
	// sub-agent emit into it concurrently. Both c.sink and the agent's sink use
	// this same wrapped value.
	sink = event.NewSyncSink(sink)
	c.storeSink(sink)
	c.asker = asker

	// 1. Load config
	path := cfgPath
	if path == "" {
		path = config.FindConfig()
	}
	cfg, err := config.LoadWithEnv(path, config.FindDotEnv())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	c.cfg = cfg

	// 2. Resolve default provider
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("no providers configured — add one in lumen.toml or run 'lumen setup'")
	}
	provCfg, matched := resolveProvider(cfg.Providers, cfg.DefaultModel)
	if !matched && cfg.DefaultModel != "" {
		sink.Emit(event.Event{
			Kind: event.Notice, Level: event.LevelWarn,
			Text: fmt.Sprintf("default_model %q matched no provider's name or model — falling back to %q", cfg.DefaultModel, provCfg.Name),
		})
	}
	c.provCfg = provCfg
	c.pricing = pricingFromConfig(provCfg.Pricing)

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
	backends := []modelpool.Backend{{Name: provCfg.Name, Provider: prov, IsLocal: isLoopbackURL(provCfg.BaseURL)}}
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
			backends = append(backends, modelpool.Backend{Name: cfg.Providers[i].Name, Provider: fb, IsLocal: isLoopbackURL(cfg.Providers[i].BaseURL)})
		}
	}

	// 2c. With more than one backend, route through a latency-aware, local-first
	// RoutingProvider. Failover then happens at the stream layer WITHOUT replaying
	// produced output — unlike the controller-level retry below, which re-runs the
	// whole prompt. We hand the router to the agent and clear c.fallbacks so the
	// replay path stays a last resort (it won't trigger while the router succeeds).
	if len(backends) > 1 {
		c.prov = modelpool.NewRoutingProvider(backends)
		c.fallbacks = nil
	}

	// 3. Build tool registry — honoring [tools] profile (default "full"; "core"
	// offers only the coding set so the model isn't handed ~116 tools per turn).
	reg := tool.NewRegistry()
	for _, t := range selectTools(tool.Builtins(), cfg.Tools.Profile) {
		reg.Add(t)
	}

	// 4. Build skill store
	wd, _ := os.Getwd()
	c.skillStore = skill.New(skillOptionsFromConfig(cfg, wd))
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

	// 7b. Wire the orchestrator's workflow pool to real sub-agents so
	// create_workflow + run_workflow execute actual work in parallel (the pool
	// was previously empty → every task failed "no agent available"). Each task
	// spawns a one-shot lumen sub-agent; workflow/task meta-tools are excluded so
	// a workflow task can't recursively spawn more workflows.
	spawnCfg := agent.SpawnConfig{
		Provider:      prov,
		ParentReg:     reg,
		MaxSteps:      cfg.Agent.MaxSteps,
		ContextWindow: cfg.Agent.ContextWindow,
		Temperature:   cfg.Agent.Temperature,
		Gate:          gate,
		ExcludeTools:  append([]string{"run_workflow", "create_workflow", "list_workflows"}, agent.SubagentMetaTools...),
	}
	maxParallel := cfg.Agent.MaxSteps
	if maxParallel <= 0 {
		maxParallel = 8
	}
	builtin.SetWorkflowAgent("lumen", func(ctx context.Context, prompt string) (string, error) {
		return agent.Spawn(ctx, spawnCfg, prompt)
	}, maxParallel)

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

	// 9. Create session — auto-resume from last session if it's small enough.
	// Sessions live in ~/.lumen/history/ as JSONL files.
	// Large sessions (>30 messages) are not reused to avoid context window overflow
	// on the first turn of a new run.
	histDir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "history")
	os.MkdirAll(histDir, 0700)
	sessPath := filepath.Join(histDir, time.Now().Format("2006-01-02-150405")+".jsonl")

	lastBytes, _ := os.ReadFile(filepath.Join(histDir, ".last_session"))
	lastName := strings.TrimSpace(string(lastBytes))
	if lastName != "" {
		candidate := filepath.Join(histDir, lastName)
		if info, err := os.Stat(candidate); err == nil && info.Size() < 50*1024 {
			sessPath = candidate // reuse only small sessions
		}
	}
	c.sessPath = sessPath
	c.sess = agent.NewSession(sessPath)

	// 10. Init persistent memory store (~/.lumen/memories/)
	memDir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "memories")
	memStore, err := memory.NewStore(memDir)
	if err != nil {
		c.logf("memory: %v (disabled)", err)
		memStore = nil
	}
	c.memStore = memStore
	builtin.SetMemStore(memStore) // wire tools: remember / forget / memories

	// 11. Create agent
	var memPrompt string
	if memStore != nil {
		memPrompt = memStore.SystemPrompt()
	}
	agOpts := agentOptionsFromConfig(cfg)
	agOpts.Sink = sink
	agOpts.Gate = gate
	agOpts.Asker = asker
	agOpts.MemoryPrompt = memPrompt
	agOpts.Pricing = c.pricing
	// Use c.prov so the main loop runs through the RoutingProvider (latency-aware
	// local-first routing + no-replay failover) when multiple backends exist; it
	// is the same as prov when only one is configured.
	c.ag = agent.New(c.prov, reg, c.sess, agOpts)

	// 12. Wire infrastructure
	c.cp = checkpoint.New()
	c.ag.SetCheckpoint(c.cp)
	c.jm = jobs.NewManager()
	c.ag.SetJobs(c.jm)

	// 13. Verify-after-edit (C-5): after a writer batch the agent auto-runs
	// build/vet/test and feeds failures back for self-repair. Config from
	// lumen.toml [verify]; disabled config leaves the loop fully inert.
	verifyCfg := editverify.DefaultConfig()
	if p := config.FindConfig(); p != "" {
		if raw, err := os.ReadFile(p); err == nil {
			if vc, err := editverify.ConfigFromTOML(raw); err == nil {
				verifyCfg = vc
			}
		}
	}
	c.setupEditVerify(wd, verifyCfg)

	return nil
}

// setupEditVerify installs the verify-after-edit verifier when verification is
// enabled and wd is a recognized project (Go / JS-TS / Python). Detect chooses
// the commands per changed file and the runner skips uninstalled tools, so it
// never misfires `go build` in a non-Go repo nor false-fails on an absent
// linter. Returns whether the loop was activated.
func (c *Controller) setupEditVerify(wd string, cfg editverify.Config) bool {
	if !cfg.Enabled {
		return false
	}
	// Walk up to the project root so verify still activates when lumen runs from
	// a monorepo subdirectory (the go.mod/package.json may be a level or two up).
	root := editverify.FindProjectRoot(wd)
	if root == "" {
		return false
	}
	c.ag.SetVerifier(editverify.New(root, cfg), cfg)
	return true
}

// Run executes a one-shot task and returns the agent's final answer.
// On failure, automatically tries fallback providers if configured.
func (c *Controller) Run(ctx context.Context, prompt string) error {
	c.sink().Emit(event.Event{Kind: event.Phase, Text: c.prov.Name() + " · executing"})
	err := c.ag.Run(ctx, prompt)
	if err != nil && len(c.fallbacks) > 0 {
		original := c.prov.Name()
		for _, fb := range c.fallbacks {
			c.sink().Emit(event.Event{
				Kind: event.Notice, Level: event.LevelWarn,
				Text: fmt.Sprintf(original + " failed — switching to " + fb.Name()),
			})
			c.ag.SetProvider(fb)
			err2 := c.ag.Run(ctx, prompt)
			if err2 == nil {
				return nil
			}
			err = err2 // surface the error from the provider actually tried last
		}
		c.ag.SetProvider(c.prov) // restore
	}
	if err != nil {
		c.emitError(err)
	}
	c.warnIfNotPersisting()
	return err
}

// emitError surfaces a turn-ending error to every front-end via the event sink.
// Without this, callers that ignore Run/Plan's return value (one-shot, the
// interactive loop) would fail silently — the user sees "Thinking…" and nothing
// else when the provider returns e.g. HTTP 402.
// warnIfNotPersisting surfaces, once, a session that has silently stopped saving
// to disk — otherwise the user loses conversation resume with no warning.
func (c *Controller) warnIfNotPersisting() {
	if c.persistWarned || c.sess == nil {
		return
	}
	if pe := c.sess.PersistErr(); pe != nil {
		c.persistWarned = true
		c.sink().Emit(event.Event{
			Kind:      event.Notice,
			Level:     event.LevelWarn,
			Text:      "session not being saved: " + pe.Error() + " (this conversation may not resume)",
			Timestamp: time.Now(),
		})
	}
}

func (c *Controller) emitError(err error) {
	c.sink().Emit(event.Event{
		Kind:      event.Notice,
		Level:     event.LevelErr,
		Text:      err.Error(),
		Timestamp: time.Now(),
	})
}

// Plan runs in read-only mode and returns the agent's plan.
func (c *Controller) Plan(ctx context.Context, prompt string) error {
	c.ag.SetPlanMode(true)
	c.sink().Emit(event.Event{Kind: event.Phase, Text: c.prov.Name() + " · planning (read-only)"})
	err := c.ag.Run(ctx, prompt)
	if err != nil {
		c.emitError(err)
	}
	return err
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
	s = event.NewSyncSink(s) // foreground + background sub-agents emit concurrently
	c.storeSink(s)
	if c.ag != nil {
		c.ag.SetSink(s)
	}
}

// SaveMark writes .last_session so the next run resumes from this conversation.
func (c *Controller) SaveMark() {
	if c.sessPath == "" {
		return
	}
	dir := filepath.Dir(c.sessPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		c.warn("could not save resume marker: " + err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(dir, ".last_session"), []byte(filepath.Base(c.sessPath)), 0600); err != nil {
		c.warn("could not save resume marker: " + err.Error())
	}
}

// warn surfaces a non-fatal warning via the event sink.
func (c *Controller) warn(text string) {
	c.sink().Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text, Timestamp: time.Now()})
}

// ProviderName returns the active provider instance name.
// Pricing returns the active provider's configured rates, or nil when none are
// configured (the cost readout then uses the built-in default).
func (c *Controller) Pricing() *provider.Pricing { return c.pricing }

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
		if err := c.tl.Close(); err != nil {
			c.warn("timeline not fully saved: " + err.Error())
		}
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
