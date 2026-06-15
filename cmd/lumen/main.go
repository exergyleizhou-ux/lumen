// Lumen — 「你是我绿洲里的光」
// A multi-model coding agent for your terminal. Built in Go, single binary.
//
// Usage:
//
//	lumen chat              Start interactive chat (TUI)
//	lumen run "prompt"      Run a one-shot task
//	lumen run --plan "..."  Plan mode (read-only, produces a plan for approval)
//	lumen run --mode M "..." Permission mode: default | accept-edits | bypass | plan
//	lumen setup             Run config wizard
//	lumen version           Print version info
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"


	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/doctor"
	"lumen/internal/event"
	"lumen/internal/permission"

	// Ensure openai provider is registered
	_ "lumen/internal/provider/openai"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "chat":
		runChat(os.Args[2:])
	case "run":
		runOneShot(os.Args[2:])
	case "setup":
		runSetup()
	case "doctor":
		runDoctor()
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
  lumen chat [--mode M] [--plan]    Interactive chat (plan = read-only)
  lumen run "prompt"                Run a one-shot task
  lumen run --plan "..."            Plan mode (read-only)
  lumen run --mode M "..."          Permission mode: default | accept-edits | bypass | plan
  lumen doctor                      Run health checks
  lumen setup                       Run config wizard
  lumen version                     Print version

Permission modes:
  default       Safe tools auto-allowed, writes ask (default)
  accept-edits  All tools allowed except dangerous ones
  bypass        All tools bypass the gate
  plan          Read-only: all writes blocked`)
}

// ── Setup ─────────────────────────────────────────────────

func runSetup() {
	fmt.Println("Lumen setup")
	fmt.Println("Create a lumen.toml file in your project root with your provider config.")
	fmt.Println("See .env.example for environment variable configuration.")
	fmt.Println()

	ctrl := control.New()
	if err := ctrl.Configure(headlessSink(), nil, ""); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Default model: %s/%s\n", ctrl.ProviderName(), ctrl.ModelName())
	fmt.Printf("Permission mode: %s\n", ctrl.PermissionMode())
	fmt.Printf("Skills loaded: %d\n", len(ctrl.Skills().List()))
}

// ── Health check ────────────────────────────────────────

func runDoctor() {
	fmt.Println("Lumen doctor — checking configuration...")
	fmt.Println()
	cfg, err := config.Load(config.FindConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	report := doctor.Run(cfg)
	fmt.Print(report.Print())
}

// ── One-shot run ──────────────────────────────────────────

func parseRunArgs(args []string) (planMode bool, mode string, prompt string) {
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--plan":
			planMode = true
		case "--mode":
			if i+1 < len(args) {
				mode = args[i+1]
				i++
			}
		default:
			rest = append(rest, args[i])
		}
	}
	return planMode, mode, strings.Join(rest, " ")
}

func runOneShot(args []string) {
	planMode, modeOverride, prompt := parseRunArgs(args)
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "error: prompt is required")
		os.Exit(1)
	}

	ctrl := control.New()
	if err := ctrl.Configure(headlessSink(), nil, ""); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	// Headless mode: auto-approve tools that would otherwise require a human.
	// Plan mode stays read-only via the gate; default/accept-edits get auto-approve.
	headlessApprove := func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }
	mode := ctrl.PermissionMode()
	if modeOverride != "" {
		mode = permission.ParseMode(modeOverride)
		ctrl.SetPermissionMode(mode)
	}
	ctrl.Agent().SetGate(permission.NewGate(mode, headlessApprove))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	var err error
	if planMode {
		fmt.Fprintf(os.Stderr, "⚡ Plan mode — read-only tools only. The agent will produce a plan.\n\n")
		err = ctrl.Plan(ctx, prompt)
	} else {
		fmt.Fprintf(os.Stderr, "⚡ Running with %s/%s (permissions: %s)...\n\n",
			ctrl.ProviderName(), ctrl.ModelName(), ctrl.PermissionMode())
		err = ctrl.Run(ctx, prompt)
	}

	if err != nil {
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
	ctrl := control.New()
	mode := parseChatArgs(args)
	if err := runTUIChat(ctrl, mode); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}

// parseChatArgs returns a mode override, or empty string.
func parseChatArgs(args []string) string {
	mode := ""
	plan := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mode":
			i++
			if i < len(args) { mode = args[i] }
		case "--plan":
			plan = true
		}
	}
	if plan { return "plan" }
	return mode
}

// chatSink is a clean chat sink: only text output, no tool noise.
func chatSink() event.Sink {
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Text:
			renderClean(e.Text)
		case event.ToolResult:
			if e.Tool.Err != "" && e.Tool.Name != "" {
				// Minimal: just a one-line tool status
				short := e.Tool.Name
				if len(short) > 25 { short = short[:22] + "..." }
				fmt.Fprintf(os.Stderr, "  \033[2m%s ❌\033[0m\n", short)
			}
		case event.UsageKind:
			flushBuffer()
			if e.Usage != nil {
				fmt.Fprintf(os.Stderr, "\n\033[2m── %d tokens\033[0m\n", e.Usage.TotalTokens)
			}
		}
	})
}


// ── Headless sink ─────────────────────────────────────────

func headlessSink() event.Sink {
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Text:
			renderClean(e.Text)
		case event.Reasoning:
			fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m", e.Text)
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
			flushBuffer()
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
