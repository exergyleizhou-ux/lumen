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
	step    atomic.Int64
	costU   atomic.Int64
}

func (s *liveStats) addCost(v float64) { s.costU.Add(int64(v * 1_000_000)) }
func (s *liveStats) cost() float64     { return float64(s.costU.Load()) / 1_000_000 }

var stats = &liveStats{}

// ── ANSI ──────────────────────────────────────────────────

const (
	r = "\033[0m"
	b = "\033[1m"
	d = "\033[2m"
	i = "\033[3m"
	C = "\033[36m" // Cyan
	G = "\033[32m" // Green
	R = "\033[31m" // Red
	Y = "\033[33m" // Yellow
	W = "\033[97m" // White
)

func clr(s, code string) string { return code + s + r }

// ── Sink ──────────────────────────────────────────────────

func termSink() event.Sink {
	thinking := false
	textStarted := false
	textLen := 0
	truncated := false
	const maxOutput = 24 * 1024

	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted:
			thinking = true
			textStarted = false
			textLen = 0
			truncated = false
			stats.step.Store(0)

		case event.Reasoning:
			if thinking && !textStarted {
				fmt.Fprint(os.Stdout, clr(stripMD(e.Text), d))
			}

		case event.Text:
			if thinking && !textStarted {
				thinking = false
				textStarted = true
				fmt.Print("\n")
			}
			if truncated {
				return
			}
			cleaned := stripMD(e.Text)
			textLen += len(cleaned)
			if textLen > maxOutput {
				truncated = true
				fmt.Fprintf(os.Stdout, "\n%s\n", clr("... output truncated", d))
				return
			}
			fmt.Fprint(os.Stdout, cleaned)

		case event.ToolDispatch:
			thinking = false
			textStarted = true
			sn := stats.step.Add(1)
			fmt.Fprintf(os.Stdout, "\n  %s %s ", clr(fmt.Sprintf("[%d]", sn), C), clr(e.Tool.Name, d+Y))

		case event.ToolResult:
			if e.Tool.Err != "" {
				fmt.Fprintf(os.Stdout, "%s\n", clr("x "+e.Tool.Err, R))
			} else if e.Tool.Blocked {
				fmt.Fprintf(os.Stdout, "%s\n", clr("blocked", d))
			} else {
				fmt.Fprint(os.Stdout, clr("ok", G)+"\n")
			}

		case event.UsageKind:
			if e.Usage != nil {
				stats.tkIn.Store(int64(e.Usage.PromptTokens))
				stats.tkOut.Store(int64(e.Usage.CompletionTokens))
				stats.tkCache.Store(int64(e.Usage.CacheHitTokens))
				stats.addCost(float64(e.Usage.PromptTokens)*0.14/1e6 +
					float64(e.Usage.CompletionTokens)*0.28/1e6)
			}

		case event.FilePreview:
			fmt.Fprintf(os.Stdout, "\n%s\n", clr("--- diff ---", C))
			fmt.Fprint(os.Stdout, e.DiffText)
			fmt.Fprint(os.Stdout, clr("---", C)+"\n")

		case event.TurnDone:
			drawFooter()
			thinking = false
			textStarted = false
			stats.step.Store(0)
		}
	})
}

// ── Footer ────────────────────────────────────────────────

func drawFooter() {
	ti := stats.tkIn.Load()
	to := stats.tkOut.Load()
	tc := stats.tkCache.Load()
	cost := stats.cost()
	steps := stats.step.Load()

	cachePct := 0
	if ti > 0 {
		cachePct = int(float64(tc) / float64(ti) * 100)
	}
	fmt.Fprintf(os.Stdout, "\n%s %s:%s tokens  cache %d%%  $%.4f  %d steps%s\n",
		clr("", d),
		clr(fmt.Sprint(ti/1000), C),
		clr(fmt.Sprint(to/1000), G),
		cachePct, cost, steps,
		clr("", r))
}

// ── Chat loop ─────────────────────────────────────────────

func runChatUI(ctrl *control.Controller, modeOverride string) error {
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		return err
	}
	if modeOverride != "" {
		ctrl.SetPermissionMode(permission.ParseMode(modeOverride))
	}

	fmt.Printf("\n%s %s\n\n",
		clr("lumen", b+W),
		clr(ctrl.ProviderName()+"/"+ctrl.ModelName(), d))

	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(clr("> ", C))
		if !sc.Scan() {
			break
		}
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		if text == "/exit" || text == "/quit" {
			return nil
		}
		if text == "/help" {
			fmt.Printf("%s\n\n", clr("  /exit  /mode  /mode bypass|plan|default", d))
			continue
		}
		if text == "/mode" {
			fmt.Printf("%s\n\n", clr("  bypass  plan  default  accept-edits", d))
			continue
		}
		if strings.HasPrefix(text, "/mode ") {
			m := permission.ParseMode(strings.TrimPrefix(text, "/mode "))
			ctrl.SetPermissionMode(m)
			fmt.Printf("  %s\n\n", clr("["+string(m)+"]", b+C))
			continue
		}

		fmt.Printf("\n%s\n", clr(text, b+C))
		ctrl.Run(context.Background(), text)
		fmt.Print("\n")
	}
	return nil
}

// ── Markdown stripper ─────────────────────────────────────

func stripMD(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "####", "")
	s = strings.ReplaceAll(s, "###", "")
	s = strings.ReplaceAll(s, "##", "")
	lines := strings.Split(s, "\n")
	var clean []string
	for _, l := range lines {
		for strings.HasPrefix(l, "#") {
			l = strings.TrimPrefix(l, "#")
			l = strings.TrimPrefix(l, " ")
		}
		clean = append(clean, l)
	}
	s = strings.Join(clean, "\n")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "|---", "")
	s = strings.ReplaceAll(s, "| ", "  ")
	return s
}
