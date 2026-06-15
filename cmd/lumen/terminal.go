// terminal.go — Reasonix-quality terminal UI.
// Clean output, thinking inline, status footer after each turn.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/permission"
)

type liveStats struct {
	tkIn    atomic.Int64
	tkOut   atomic.Int64
	tkCache atomic.Int64
	tools   atomic.Int64
	costU   atomic.Int64 // micros
}

func (s *liveStats) addCost(v float64) {
	s.costU.Add(int64(v * 1_000_000))
}
func (s *liveStats) cost() float64 {
	return float64(s.costU.Load()) / 1_000_000
}

var stats = &liveStats{}

// ── Sink: Reasonix-style output ────────────────────────────
//
// Strategy:
//   - Agent text → stdout (streaming, the main content)
//   - Thinking → stdout dimmed (Reasonix shows thinking inline)
//   - Tool calls → stdout dimmed on their own line
//   - Token bar → stderr at end of turn

func termSink() event.Sink {
	thinking := false
	textStarted := false

	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted:
			thinking = true
			textStarted = false

		case event.Reasoning:
			if thinking && !textStarted {
				fmt.Fprint(os.Stdout, dim(e.Text))
			}

		case event.Text:
			if thinking && !textStarted {
				// First text chunk: clear thinking, print a thin separator
				thinking = false
				textStarted = true
				fmt.Print("\n\n")
			}
			fmt.Fprint(os.Stdout, e.Text)

		case event.ToolDispatch:
			thinking = false
			textStarted = true
			stats.tools.Add(1)
			fmt.Printf("\n%s", dim("  ⚙ "+e.Tool.Name))

		case event.ToolResult:
			if e.Tool.Err != "" {
				fmt.Printf(" %s\n", red("✗"))
			} else if e.Tool.Blocked {
				fmt.Printf(" %s\n", dim("⊘ blocked"))
			} else {
				fmt.Printf(" %s\n", green("✓"))
			}

		case event.UsageKind:
			if e.Usage != nil {
				stats.tkIn.Store(int64(e.Usage.PromptTokens))
				stats.tkOut.Store(int64(e.Usage.CompletionTokens))
				stats.tkCache.Store(int64(e.Usage.CacheHitTokens))
				stats.addCost(float64(e.Usage.PromptTokens)*0.14/1e6 +
					float64(e.Usage.CompletionTokens)*0.28/1e6)
			}

		case event.TurnDone:
			drawFooter()
			thinking = false
			textStarted = false
		}
	})
}

// ── Footer ─────────────────────────────────────────────────

func drawFooter() {
	ti := stats.tkIn.Load()
	to := stats.tkOut.Load()
	tc := stats.tkCache.Load()
	cost := stats.cost()
	tools := stats.tools.Load()

	cachePct := 0
	if ti > 0 {
		cachePct = int(float64(tc) / float64(ti) * 100)
	}

	fmt.Fprintf(os.Stderr, "%s  %d∶%d tokens  ·  cache %d%%  ·  $%.4f  ·  %d tools  %s\n",
		dim(""),
		ti/1000, to/1000,
		cachePct,
		cost,
		tools,
		dim(""))
}

// ── Chat loop ──────────────────────────────────────────────

func runChatUI(ctrl *control.Controller, modeOverride string) error {
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		return err
	}
	if modeOverride != "" {
		ctrl.SetPermissionMode(permission.ParseMode(modeOverride))
	}

	fmt.Printf("\n%s  %s\n\n",
		bold("lumen"), dim(ctrl.ProviderName()+"/"+ctrl.ModelName()))

	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(cyan("▸ "))
		if !sc.Scan() { break }
		text := strings.TrimSpace(sc.Text())

		if text == "" { continue }
		switch {
		case text == "/exit" || text == "/quit":
			return nil
		case text == "/help":
			fmt.Printf("  /exit /mode /mode bypass|plan|default\n\n")
			continue
		case text == "/mode":
			fmt.Printf("  bypass | plan | default | accept-edits\n\n")
			continue
		case strings.HasPrefix(text, "/mode "):
			m := permission.ParseMode(strings.TrimPrefix(text, "/mode "))
			ctrl.SetPermissionMode(m)
			fmt.Printf("  %s\n\n", dim("["+string(m)+"]"))
			continue
		}

		ctrl.Run(context.Background(), text)
		fmt.Print("\n\n")
	}
	return nil
}

// ── ANSI helpers ───────────────────────────────────────────

func bold(s string) string  { return "\033[1m" + s + "\033[0m" }
func dim(s string) string   { return "\033[2m" + s + "\033[0m" }
func cyan(s string) string  { return "\033[36m" + s + "\033[0m" }
func green(s string) string { return "\033[32m" + s + "\033[0m" }
func red(s string) string   { return "\033[31m" + s + "\033[0m" }
