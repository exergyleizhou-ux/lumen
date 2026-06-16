// Lumen — 「你是我绿洲里的光」
// A multi-model coding agent for your terminal. Built in Go, single binary.
//
// Usage:
//   lumen chat [--mode M] [--plan]   Interactive chat
//   lumen run "prompt"                One-shot task
//   lumen run --plan "..."            Plan mode (read-only)
//   lumen run --mode M "..."          Permission mode: default | accept-edits | bypass | plan
//   lumen version                     Print version info
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/doctor"
	"lumen/internal/event"
	"lumen/internal/permission"
	"lumen/internal/tui"

	// Ensure all providers are registered via init()
	_ "lumen/internal/provider/anthro"
	_ "lumen/internal/provider/gemini"
	_ "lumen/internal/provider/openai"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "chat":
		runChat(os.Args[2:])
	case "tui":
		runTUI(os.Args[2:])
	case "wizard":
		runWizardEntry()
	case "run":
		runOneShot(os.Args[2:])
	case "setup":
		runSetup()
	case "doctor":
		runDoctor()
	case "version":
		fmt.Println("Lumen v0.1.0")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Lumen — 「你是我绿洲里的光」

Usage:
  lumen wizard            Active onboarding — AI interviews you, then builds
  lumen chat [--mode M]   Interactive chat (terminal line-mode)
  lumen tui [--mode M]    Multi-panel Bubble Tea TUI
  lumen run "prompt"      One-shot task
  lumen run --plan "..."  Plan mode (read-only)
  lumen run --mode M "..."
  lumen doctor
  lumen setup
  lumen version

Modes: bypass | plan | default | accept-edits
`)
}

// ── Shared ─────────────────────────────────────────────────

func makeController(sink event.Sink, modeOverride string) (*control.Controller, error) {
	ctrl := control.New()
	if err := ctrl.Configure(sink, nil, ""); err != nil {
		return nil, err
	}
	if modeOverride != "" {
		ctrl.SetPermissionMode(permission.ParseMode(modeOverride))
	}
	return ctrl, nil
}

// ── Event sink ─────────────────────────────────────────────
//
// Render strategy: agent text goes straight to stdout with no framing.
// Tool activity shows as a single overwritable status line on stderr so
// the main output stays clean.  Token counts go to stderr.


// ── Setup ──────────────────────────────────────────────────

func runSetup() {
	ctrl := control.New()
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Default model: %s/%s\n", ctrl.ProviderName(), ctrl.ModelName())
	fmt.Printf("Permission mode: %s\n", ctrl.PermissionMode())
	fmt.Printf("Skills loaded: %d\n", len(ctrl.Skills().List()))
}

// ── Doctor ─────────────────────────────────────────────────

func runDoctor() {
	cfg, err := config.Load(config.FindConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	report := doctor.Run(cfg)
	fmt.Print(report.Print())
}

// ── One-shot run ───────────────────────────────────────────

func runOneShot(args []string) {
	planMode, modeOverride, prompt := parseRunArgs(args)
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "error: prompt is required")
		os.Exit(1)
	}

	ctrl, err := makeController(termSink(), modeOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	headlessApprove := func(context.Context, string, json.RawMessage) (bool, error) { return true, nil }
	ctrl.Agent().SetGate(permission.NewGate(ctrl.PermissionMode(), headlessApprove))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	if planMode {
		fmt.Fprintf(os.Stderr, "[plan — read-only]\n\n")
		ctrl.Plan(ctx, prompt)
	} else {
		fmt.Fprintf(os.Stderr, "")
		ctrl.Run(ctx, prompt)
	}
}

func parseRunArgs(args []string) (planMode bool, mode, prompt string) {
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

// ── Wizard entry ────────────────────────────────────────────

func runWizardEntry() {
	ctrl := control.New()
	if err := runWizard(ctrl); err != nil {
		fmt.Fprintf(os.Stderr, "wizard: %v\n", err)
		os.Exit(1)
	}
}

// ── Interactive chat ───────────────────────────────────────

func runChat(args []string) {
	ctrl := control.New()
	mode := parseModeFlag(args)
	if err := runChatUI(ctrl, mode); err != nil {
		fmt.Fprintf(os.Stderr, "chat: %v\n", err)
		os.Exit(1)
	}
}

// ── Bubble Tea TUI ─────────────────────────────────────────

func runTUI(args []string) {
	ctrl, err := makeController(event.Discard, parseModeFlag(args))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	model := tui.NewModel()

	// Build status
	model.Send(tui.StatusMsg{
		Model:    ctrl.ModelName(),
		Provider: ctrl.ProviderName(),
		Mode:     string(ctrl.PermissionMode()),
	})

	// Bridge: agent events → TUI messages
	ctrl.SetSink(tuiSink(model))

	// Goroutine: read TUI input lines, run agent, feed results back
	go func() {
		for {
			line := model.WaitInput()
			if line == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := ctrl.Run(ctx, line); err != nil {
				model.Send(tui.TuiMsg{Role: "system", Content: fmt.Sprintf("error: %v", err)})
			}
			cancel()
		}
	}()

	// Start the TUI (blocks until quit)
	if err := tui.RunTUI(model); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}

func parseModeFlag(args []string) string {
	mode := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mode":
			if i+1 < len(args) {
				mode = args[i+1]
				i++
			}
		case "--plan":
			return "plan"
		}
	}
	return mode
}
