// Lumen — 「你是我绿洲里的光」
// A multi-model coding agent for your terminal. Built in Go, single binary.
//
// Usage:
//
//	agent chat              Start interactive chat (TUI)
//	agent run "prompt"      Run a one-shot task
//	agent run --plan "..."  Plan mode (read-only, produces a plan for approval)
//	agent setup             Run config wizard
//	agent version           Print version info
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"lumen/internal/agent"
	"lumen/internal/checkpoint"
	"lumen/internal/config"
	"lumen/internal/event"
	"lumen/internal/jobs"
	"lumen/internal/permission"
	"lumen/internal/provider"
	"lumen/internal/provider/openai"
	"lumen/internal/skill"
	"lumen/internal/tool"

	// Import builtin tools for side-effect registration
	_ "lumen/internal/tool/builtin"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Fill unset provider keys from a local .env before resolving config.
	_ = config.LoadDotEnv(".env")

	cmd := os.Args[1]
	switch cmd {
	case "chat":
		runChat(os.Args[2:])
	case "run":
		runOneShot(os.Args[2:])
	case "setup":
		runSetup()
	case "version":
		fmt.Println("Lumen v0.1.0")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Lumen — 「你是我绿洲里的光」

A multi-model coding agent for your terminal. Built in Go, single binary.

Usage:
  agent chat              Start interactive chat
  agent run "prompt"      Run a one-shot task
  agent run --plan "..."  Plan mode (read-only)
  agent setup             Run config wizard
  agent version           Print version`)
}

// ── Setup ─────────────────────────────────────────────────

func runSetup() {
	fmt.Println("Lumen setup")
	fmt.Println("Create a lumen.toml file in your project root with your provider config.")
	fmt.Println("See .env.example for environment variable configuration.")
	fmt.Println()
	cfg, err := config.Load(config.FindConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Default model: %s\n", cfg.DefaultModel)
	fmt.Printf("Providers configured: %d\n", len(cfg.Providers))
	for _, p := range cfg.Providers {
		keyStatus := "❌ not set"
		if p.APIKey != "" {
			keyStatus = "✅ set"
		}
		fmt.Printf("  %s (%s/%s) → %s %s\n", p.Name, p.Kind, p.Model, p.BaseURL, keyStatus)
	}
}

// ── One-shot run ──────────────────────────────────────────

func runOneShot(args []string) {
	planMode := false
	var prompt string

	if len(args) > 0 && args[0] == "--plan" {
		planMode = true
		prompt = strings.Join(args[1:], " ")
	} else {
		prompt = strings.Join(args, " ")
	}

	if prompt == "" {
		fmt.Fprintln(os.Stderr, "error: prompt is required")
		os.Exit(1)
	}

	// Load config
	cfg, err := config.Load(config.FindConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	// Find the default provider
	if len(cfg.Providers) == 0 {
		fmt.Fprintln(os.Stderr, "error: no providers configured. Run 'agent setup' first.")
		os.Exit(1)
	}

	var providerCfg *config.ProviderConfig
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == cfg.DefaultModel {
			providerCfg = &cfg.Providers[i]
			break
		}
	}
	if providerCfg == nil {
		providerCfg = &cfg.Providers[0]
	}

	prov, err := provider.New(providerCfg.Kind, provider.Config{
		Name:    providerCfg.Name,
		BaseURL: providerCfg.BaseURL,
		Model:   providerCfg.Model,
		APIKey:  providerCfg.APIKey,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider: %v\n", err)
		os.Exit(1)
	}

	// Build tool registry
	reg := tool.NewRegistry()
	for _, t := range tool.Builtins() {
		reg.Add(t)
	}

	// Build skill store
	wd, _ := os.Getwd()
	skillStore := skill.New(skill.Options{
		ProjectRoot: wd,
	})

	// List skills for the model to see (call once, reuse slice)
	skills := skillStore.List()
	fmt.Fprintf(os.Stderr, "Skills loaded: %d\n", len(skills))
	for _, sk := range skills {
		fmt.Fprintf(os.Stderr, "  %s — %s\n", sk.Name, sk.Description)
	}

	// Create session
	sess := agent.NewSession("")

	// Create agent
	ag := agent.New(prov, reg, sess, agent.Options{
		MaxSteps:      cfg.Agent.MaxSteps,
		Temperature:   cfg.Agent.Temperature,
		ContextWindow: cfg.Agent.ContextWindow,
		Sink:          headlessSink(),
		Gate:          permission.NewGate(permission.ModeBypass, nil),
	})
	// Wire checkpoint for pre-edit snapshots
	ag.SetCheckpoint(checkpoint.New())
	// Wire background job manager
	ag.SetJobs(jobs.NewManager())

	// Skills loaded via skills/ directory (project scope wins over builtins)
	_ = skillStore

	// Plan mode
	if planMode {
		ag.SetPlanMode(true)
		fmt.Fprintf(os.Stderr, "⚡ Plan mode — read-only tools only. The agent will produce a plan.\n\n")
	} else {
		fmt.Fprintf(os.Stderr, "⚡ Running with %s/%s...\n\n", providerCfg.Name, providerCfg.Model)
	}

	// Run
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := ag.Run(ctx, prompt); err != nil {
		if ctx.Err() != nil {
			fmt.Fprintf(os.Stderr, "\n⏹ cancelled\n")
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}

// ── Interactive chat ──────────────────────────────────────

func runChat(args []string) {
	fmt.Println("⚡ Lumen chat — interactive mode")
	fmt.Println("(TUI not yet implemented — falling back to one-shot mode)")
	fmt.Println("Use: agent run \"your prompt\"")
	fmt.Println()
	runOneShot(args)
}

// ── Headless sink ─────────────────────────────────────────

func headlessSink() event.Sink {
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Text:
			fmt.Print(e.Text)
		case event.Reasoning:
			fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m", e.Text) // dim
		case event.ToolDispatch:
			fmt.Fprintf(os.Stderr, "\n⚙ %s", e.Tool.Name)
			if e.Tool.Description != "" {
				fmt.Fprintf(os.Stderr, " — %s", e.Tool.Description)
			}
			fmt.Fprintln(os.Stderr)
		case event.ToolResult:
			if e.Tool.Err != "" {
				fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", e.Tool.Name, e.Tool.Err)
			} else if e.Tool.Blocked {
				fmt.Fprintf(os.Stderr, "  ⊘ %s blocked\n", e.Tool.Name)
			} else {
				fmt.Fprintf(os.Stderr, "  ✓ %s done\n", e.Tool.Name)
			}
		case event.UsageKind:
			if e.Usage != nil {
				cacheRate := 0.0
				total := e.Usage.CacheHitTokens + e.Usage.CacheMissTokens
				if total > 0 {
					cacheRate = float64(e.Usage.CacheHitTokens) / float64(total) * 100
				}
				fmt.Fprintf(os.Stderr, "\n📊 %d tokens (cache: %.0f%%)", e.Usage.TotalTokens, cacheRate)
			}
		case event.Notice:
			switch e.Level {
			case event.LevelWarn:
				fmt.Fprintf(os.Stderr, "\n⚠ %s", e.Text)
			case event.LevelErr:
				fmt.Fprintf(os.Stderr, "\n❌ %s", e.Text)
			default:
				fmt.Fprintf(os.Stderr, "\nℹ %s", e.Text)
			}
		case event.Phase:
			fmt.Fprintf(os.Stderr, "\n● %s\n", e.Text)
		case event.TurnDone:
			fmt.Println()
		}
	})
}

// Ensure providers are imported
var _ = openai.New
