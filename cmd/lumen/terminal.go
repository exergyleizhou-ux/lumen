package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/permission"
	"lumen/internal/telemetry"
)

// ── live stats ────────────────────────────────────────────

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

const ansiReset  = "\033[0m"
const ansiBold   = "\033[1m"
const ansiDim    = "\033[2m"
const ansiCyan   = "\033[36m"
const ansiGreen  = "\033[32m"
const ansiRed    = "\033[31m"
const ansiWhite  = "\033[97m"

func a(code, s string) string { return code + s + ansiReset }

// ── Sink: Reasonix-style output + auto-telemetry ──────────

func termSink() event.Sink {
	thinking := false
	textStarted := false
	textLen := 0
	truncated := false
	const maxOutput = 32 * 1024
	tel := telemetry.NewCollector()

	return event.FuncSink(func(e event.Event) {
		switch e.Kind {

		case event.TurnStarted:
			thinking = true
			textStarted = false
			textLen = 0
			truncated = false
			stats.step.Store(0)
			tel.Record(telemetry.EventSessionStart, map[string]any{})

		case event.Reasoning:
			if thinking && !textStarted {
				fmt.Fprintf(os.Stdout, "%s%s", a(ansiDim, "thinking... "), a(ansiDim, stripMD(e.Text)))
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
				fmt.Fprintf(os.Stdout, "\n%s\n", a(ansiDim, "... output truncated"))
				return
			}
			fmt.Fprint(os.Stdout, cleaned)

		case event.ToolDispatch:
			thinking = false
			textStarted = true
			sn := stats.step.Add(1)
			tel.Record(telemetry.EventToolCall, map[string]any{"name": e.Tool.Name, "step": sn})
			fmt.Fprintf(os.Stdout, "\n  %s  %s ",
				a(ansiCyan, fmt.Sprintf("[%d]", sn)),
				e.Tool.Name)

		case event.ToolResult:
			if e.Tool.Err != "" {
				tel.Record(telemetry.EventToolError, map[string]any{"name": e.Tool.Name, "error": e.Tool.Err})
				fmt.Fprintf(os.Stdout, "  %s\n", a(ansiRed, "x "+e.Tool.Err))
			} else if e.Tool.Blocked {
				fmt.Fprintf(os.Stdout, "  %s\n", a(ansiDim, "blocked"))
			} else {
				fmt.Fprint(os.Stdout, a(ansiGreen, "  ok")+"\n")
			}

		case event.UsageKind:
			if e.Usage != nil {
				tel.Record(telemetry.EventModelCall, map[string]any{
					"tokens_in":  e.Usage.PromptTokens,
					"tokens_out": e.Usage.CompletionTokens,
					"total":      e.Usage.TotalTokens,
				})
				stats.tkIn.Store(int64(e.Usage.PromptTokens))
				stats.tkOut.Store(int64(e.Usage.CompletionTokens))
				stats.tkCache.Store(int64(e.Usage.CacheHitTokens))
				stats.addCost(float64(e.Usage.PromptTokens)*0.14/1e6 +
					float64(e.Usage.CompletionTokens)*0.28/1e6)
			}

		case event.FilePreview:
			fmt.Fprintf(os.Stdout, "\n%s\n", a(ansiCyan, "--- diff ---"))
			fmt.Fprint(os.Stdout, e.DiffText)
			fmt.Fprint(os.Stdout, a(ansiCyan, "---")+"\n")

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
	if steps == 0 {
		steps = 1
	}

	pct := 0
	if ti > 0 {
		pct = int(float64(tc) / float64(ti) * 100)
	}
	tkm := fmt.Sprintf("%.0fk", float64(ti+to)/1000)

	fmt.Fprintf(os.Stdout, "\n%s %s  %s  %s  %s\n",
		a(ansiDim, "---"),
		a(ansiCyan, tkm+" tokens"),
		a(ansiDim, fmt.Sprintf("cache %d%%", pct)),
		a(ansiDim, fmt.Sprintf("$%.4f", cost)),
		a(ansiDim, fmt.Sprintf("%d steps", steps)))
}

// ── Chat loop ─────────────────────────────────────────────

func runChatUI(ctrl *control.Controller, modeOverride string) error {
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		return err
	}
	if modeOverride != "" {
		ctrl.SetPermissionMode(permission.ParseMode(modeOverride))
	}

	// Header
	fmt.Printf("\n%s %s\n\n",
		a(ansiBold+ansiWhite, "lumen"),
		a(ansiDim, ctrl.ProviderName()+"/"+ctrl.ModelName()))

	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%s ", a(ansiCyan+ansiBold, ">"))
		if !sc.Scan() {
			onChatExit()
			break
		}
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}

		// Commands
		switch {
		case text == "/exit" || text == "/quit":
			onChatExit()
			return nil
		case text == "/help":
			fmt.Printf("\n%s\n\n", a(ansiDim, "  /exit  /help  /mode  /models  /model <name>  /feedback  /analytics"))
			continue
		case text == "/mode":
			fmt.Printf("\n%s\n\n", a(ansiDim, "  bypass  plan  default  accept-edits"))
			continue
		case strings.HasPrefix(text, "/mode "):
			m := permission.ParseMode(strings.TrimPrefix(text, "/mode "))
			ctrl.SetPermissionMode(m)
			fmt.Printf("\n  %s\n\n", a(ansiBold+ansiCyan, "["+string(m)+"]"))
			continue
		case text == "/models":
			drawModelList()
			continue
		case strings.HasPrefix(text, "/model "):
			name := strings.TrimPrefix(text, "/model ")
			newName, err := ctrl.SwitchModel(name)
			if err != nil {
				fmt.Printf("\n  %s\n\n", a(ansiRed, err.Error()))
			} else {
				fmt.Printf("\n  %s\n\n", a(ansiBold+ansiGreen, "switched to "+newName))
			}
			continue
		case text == "/feedback" || strings.HasPrefix(text, "/feedback "):
			parts := strings.Fields(text)
			msg := ""
			if len(parts) > 1 {
				msg = strings.Join(parts[1:], " ")
			}
			fs := telemetry.NewFeedbackStore()
			fs.Submit("text", msg, "chat: "+text, "")
			fmt.Printf("\n  %s\n\n", a(ansiGreen, "feedback recorded. thank you!"))
			continue
		case text == "/share":
			c := telemetry.NewCollector()
			bundle := c.Export()
			report := telemetry.FormatExport(bundle)
			shareFile := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "share_report.txt")
			os.WriteFile(shareFile, []byte(report), 0600)
			fmt.Printf("\n  %s\n", a(ansiBold, "Report saved to:"))
			fmt.Printf("  %s\n\n", a(ansiCyan, shareFile))
			fmt.Printf("  %s\n", a(ansiDim, "Send this file to the Lumen team — no personal data inside."))
			fmt.Printf("  %s\n\n", a(ansiDim, "You can also paste it into a GitHub Issue or email."))
			continue
		case text == "/uplink" || strings.HasPrefix(text, "/uplink "):
			cfg := telemetry.LoadUploadConfig()
			parts := strings.Fields(text)
			if len(parts) == 1 {
				status := "OFF"
				if cfg.Enabled { status = "ON" }
				fmt.Printf("\n  %s\n", a(ansiBold, "Uplink: "+status))
				fmt.Printf("  %s\n", a(ansiDim, "Auto-sends usage reports to GitHub Issues on exit."))
				fmt.Printf("  %s\n", a(ansiDim, "/uplink on  — enable auto-upload"))
				fmt.Printf("  %s\n", a(ansiDim, "/uplink off — disable"))
				fmt.Printf("  %s\n\n", a(ansiDim, "Requires: export GITHUB_TOKEN=ghp_..."))
				continue
			}
			switch parts[1] {
			case "on":
				cfg.Enabled = true
				telemetry.SaveUploadConfig(cfg)
				fmt.Printf("\n  %s\n", a(ansiGreen, "Uplink ON — reports will be sent to GitHub on exit."))
				fmt.Printf("  %s\n\n", a(ansiDim, "Make sure GITHUB_TOKEN is set in your environment."))
			case "off":
				cfg.Enabled = false
				telemetry.SaveUploadConfig(cfg)
				fmt.Printf("\n  %s\n\n", a(ansiDim, "Uplink OFF — no reports will be sent."))
			default:
				fmt.Printf("\n  %s\n\n", a(ansiDim, "Usage: /uplink on | /uplink off"))
			}
			continue
		case text == "/analytics":
			a := telemetry.NewAnalyzer()
			report := a.Analyze("week")
			fmt.Printf("\n%s\n", telemetry.FormatReport(report))
			continue
		}

		// Echo + run
		fmt.Printf("\n%s\n", a(ansiBold+ansiCyan, text))
		ctrl.Run(context.Background(), text)
		fmt.Print("\n")
	}
	return nil
}

func drawModelList() {
	presets := config.ModelPresets()
	fmt.Println()
	lastProvider := ""
	for _, pr := range presets {
		if pr.Provider != lastProvider {
			fmt.Printf("  %s\n", a(ansiBold+ansiWhite, pr.Provider))
			lastProvider = pr.Provider
		}
		fmt.Printf("    %s  %s\n",
			a(ansiCyan, pr.Name),
			a(ansiDim, pr.Model))
	}
	fmt.Printf("\n%s\n\n", a(ansiDim, "  /model <name> to switch"))
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

// ── Auto-upload on exit ──────────────────────────────────

func onChatExit() {
	url, err := telemetry.MaybeUpload()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  %s: %v\n\n", a(ansiDim, "upload skipped"), err)
		return
	}
	if url != "" {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n\n", a(ansiGreen, "report sent"), a(ansiCyan, url))
	}
}
