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
	"bufio"
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
	switch os.Args[1] {
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
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Lumen — 「你是我绿洲里的光」

Usage:
  lumen chat [--mode M] [--plan]
  lumen run "prompt"
  lumen run --plan "..."
  lumen run --mode M "..."
  lumen doctor
  lumen setup
  lumen version

Modes: bypass (default) | plan | default | accept-edits
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

func lumenSink() event.Sink {
	thinking := true
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted:
			thinking = true
			fmt.Fprint(os.Stderr, "\033[2m…\033[0m")

		case event.Text:
			if thinking {
				fmt.Fprint(os.Stderr, "\r\033[2K")
				thinking = false
			}
			fmt.Print(e.Text)

		case event.Reasoning:
			// silent

		case event.ToolDispatch:
			fmt.Fprintf(os.Stderr, "\r\033[2K\033[2m  %s\033[0m", e.Tool.Name)

		case event.ToolResult:
			if e.Tool.Err != "" {
				fmt.Fprintf(os.Stderr, "\r\033[2K\033[31m  ✗ %s\033[0m\n", e.Tool.Err)
			} else {
				fmt.Fprintf(os.Stderr, "\r\033[2K")
			}

		case event.UsageKind:
			if e.Usage != nil {
				fmt.Fprintf(os.Stderr, "\r\033[2K\033[90m  %d tokens\033[0m\n",
					e.Usage.TotalTokens)
			}

		case event.TurnDone:
			fmt.Fprint(os.Stderr, "\r\033[2K")

		case event.Notice:
			if e.Level == event.LevelErr {
				fmt.Fprintf(os.Stderr, "\r\033[2K\033[31m  %s\033[0m\n", e.Text)
			}
		}
	})
}

// ── Setup ──────────────────────────────────────────────────

func runSetup() {
	ctrl := control.New()
	if err := ctrl.Configure(lumenSink(), nil, ""); err != nil {
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

	ctrl, err := makeController(lumenSink(), modeOverride)
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

// ── Interactive chat ───────────────────────────────────────

func runChat(args []string) {
	modeOverride := parseModeFlag(args)

	ctrl, err := makeController(lumenSink(), modeOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("lumen · %s/%s · /exit\n\n",
		ctrl.ProviderName(), ctrl.ModelName())

	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !sc.Scan() { break }
		text := strings.TrimSpace(sc.Text())
		if text == "" { continue }
		if text == "/exit" || text == "/quit" { return }
		if text == "/help" {
			fmt.Println("/exit /mode /mode bypass|plan")
			continue
		}
		if text == "/mode" {
			fmt.Println("bypass | plan | default | accept-edits")
			continue
		}
		if strings.HasPrefix(text, "/mode ") {
			m := permission.ParseMode(strings.TrimPrefix(text, "/mode "))
			ctrl.SetPermissionMode(m)
			fmt.Printf("[%s]\n", m)
			continue
		}

		ctrl.Run(context.Background(), text)
		fmt.Println()
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
