package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/permission"
	"lumen/internal/telemetry"
)

type liveStats struct {
	tkIn    atomic.Int64
	tkOut   atomic.Int64
	tkCache atomic.Int64
	step    atomic.Int64
	costU   atomic.Int64
	turn    atomic.Int64
}

func (s *liveStats) addCost(v float64) { s.costU.Add(int64(v * 1_000_000)) }
func (s *liveStats) cost() float64     { return float64(s.costU.Load()) / 1_000_000 }

var st = &liveStats{}

// ── color palette ──────────────────────────────────────────

const R = "\033[0m"
const B = "\033[1m"
const D = "\033[2m"
const C = "\033[36m"
const G = "\033[32m"
const Rd = "\033[31m"
const W = "\033[97m"

func fg(c, s string) string { return c + s + R }

// ── Sink ───────────────────────────────────────────────────

func termSink() event.Sink {
	thinking := false
	textStarted := false
	textLen := 0
	truncated := false
	const maxOut = 32 * 1024
	tel := telemetry.NewCollector()

	return event.FuncSink(func(e event.Event) {
		switch e.Kind {

		case event.TurnStarted:
			thinking = true
			textStarted = false
			textLen = 0
			truncated = false
			st.step.Store(0)
			st.turn.Add(1)
			tel.Record(telemetry.EventSessionStart, map[string]any{})

		case event.Reasoning:
			if thinking && !textStarted {
				fmt.Fprintf(os.Stdout, "%s%s", fg(D, "  "), fg(D, stripMD(e.Text)))
			}

		case event.Text:
			if thinking && !textStarted {
				thinking = false
				textStarted = true
				fmt.Print("\n\n  ")
			}
			if truncated {
				return
			}
			t := stripMD(e.Text)
			textLen += len(t)
			if textLen > maxOut {
				truncated = true
				fmt.Fprintf(os.Stdout, "\n%s\n", fg(D, "  ... output truncated"))
				return
			}
			fmt.Fprint(os.Stdout, t)

		case event.ToolDispatch:
			thinking = false
			textStarted = true
			sn := st.step.Add(1)
			tel.Record(telemetry.EventToolCall, map[string]any{"name": e.Tool.Name, "step": sn})
			fmt.Fprintf(os.Stdout, "\n\n  %s %s",
				fg(C+B, fmt.Sprintf("[%d]", sn)),
				fg(D, e.Tool.Name))

		case event.ToolResult:
			if e.Tool.Err != "" {
				tel.Record(telemetry.EventToolError, map[string]any{"name": e.Tool.Name, "error": e.Tool.Err})
				fmt.Fprintf(os.Stdout, " %s\n", fg(Rd, "x "+e.Tool.Err))
			} else if e.Tool.Blocked {
				fmt.Fprint(os.Stdout, fg(D, " blocked")+"\n")
			} else {
				fmt.Fprint(os.Stdout, " "+fg(G, "ok")+"\n")
			}

		case event.UsageKind:
			if e.Usage != nil {
				tel.Record(telemetry.EventModelCall, map[string]any{
					"tokens_in":  e.Usage.PromptTokens,
					"tokens_out": e.Usage.CompletionTokens,
					"total":      e.Usage.TotalTokens,
				})
				st.tkIn.Store(int64(e.Usage.PromptTokens))
				st.tkOut.Store(int64(e.Usage.CompletionTokens))
				st.tkCache.Store(int64(e.Usage.CacheHitTokens))
				st.addCost(float64(e.Usage.PromptTokens)*0.14/1e6 + float64(e.Usage.CompletionTokens)*0.28/1e6)
			}

		case event.FilePreview:
			fmt.Fprintf(os.Stdout, "\n  %s\n", fg(C, "── diff ──"))
			for _, line := range strings.Split(e.DiffText, "\n") {
				fmt.Fprintf(os.Stdout, "  %s\n", line)
			}

		case event.TurnDone:
			drawFooter()
			thinking = false
			textStarted = false
			st.step.Store(0)
		}
	})
}

// ── Footer ─────────────────────────────────────────────────

func drawFooter() {
	ti := st.tkIn.Load()
	to := st.tkOut.Load()
	tc := st.tkCache.Load()
	cost := st.cost()
	steps := st.step.Load()
	turns := st.turn.Load()
	if steps == 0 { steps = 1 }
	pct := 0
	if ti > 0 { pct = int(float64(tc) / float64(ti) * 100) }

	fmt.Fprintf(os.Stdout, "\n\n%s %s  %s  %s  %s\n\n",
		fg(D, "  ──"),
		fg(C, fmt.Sprintf("%.0fk tk", float64(ti+to)/1000)),
		fg(D, fmt.Sprintf("cache %d%%", pct)),
		fg(D, fmt.Sprintf("$%.4f", cost)),
		fg(D, fmt.Sprintf("%d steps  ·  turn #%d", steps, turns)))
}

// ── Chat loop ──────────────────────────────────────────────

func runChatUI(ctrl *control.Controller, modeOverride string) error {
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		return err
	}
	if modeOverride != "" {
		ctrl.SetPermissionMode(permission.ParseMode(modeOverride))
	}

	drawBanner(ctrl)

	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\n%s ", fg(C+B, "▸"))
		if !sc.Scan() { onChatExit(); break }
		text := strings.TrimSpace(sc.Text())
		if text == "" { continue }

		switch {
		case text == "/exit" || text == "/quit":
			onChatExit()
			return nil
		case text == "/help":
			drawHelp()
			continue
		case text == "/mode":
			fmt.Printf("\n  %s\n\n", fg(D, "bypass  plan  default  accept-edits"))
			continue
		case strings.HasPrefix(text, "/mode "):
			m := permission.ParseMode(strings.TrimPrefix(text, "/mode "))
			ctrl.SetPermissionMode(m)
			fmt.Printf("\n  %s\n", fg(B+C, "mode = "+string(m)))
			continue
		case text == "/models":
			drawModels()
			continue
		case strings.HasPrefix(text, "/model "):
			name := strings.TrimPrefix(text, "/model ")
			n, err := ctrl.SwitchModel(name)
			if err != nil {
				fmt.Printf("\n  %s\n", fg(Rd, err.Error()))
			} else {
				fmt.Printf("\n  %s\n", fg(G+B, "model = "+n))
			}
			continue
		case text == "/feedback" || strings.HasPrefix(text, "/feedback "):
			parts := strings.Fields(text)
			msg := ""
			if len(parts) > 1 { msg = strings.Join(parts[1:], " ") }
			telemetry.NewFeedbackStore().Submit("text", msg, "chat: "+text, "")
			fmt.Printf("\n  %s\n", fg(G, "feedback recorded. thank you."))
			continue
		case text == "/share":
			f, err := telemetry.ShareReport()
			if err != nil {
				fmt.Printf("\n  %s\n", fg(Rd, "error: "+err.Error()))
			} else {
				fmt.Printf("\n  %s  %s\n", fg(B, "report →"), fg(C, f))
			}
			continue
		case text == "/uplink" || strings.HasPrefix(text, "/uplink "):
			drawUplink(text)
			continue
		case text == "/analytics":
			fmt.Printf("\n%s\n", telemetry.FormatReport(telemetry.NewAnalyzer().Analyze("week")))
			continue
		}

		fmt.Printf("\n%s\n", fg(B+C, text))
		ctrl.Run(context.Background(), text)
		fmt.Print("\n")
	}
	return nil
}

// ── Drawing ────────────────────────────────────────────────

func drawBanner(ctrl *control.Controller) {
	header := fmt.Sprintf("  LUMEN  ·  %s/%s  ·  mode: %s",
		ctrl.ProviderName(), ctrl.ModelName(), ctrl.PermissionMode())
	fmt.Printf("\n%s\n\n", fg(C+B, "╭─"+strings.Repeat("─", len(stripANSII(header))+2)+"─╮"))
	fmt.Printf("%s\n", fg(C+B, "│")+"  "+header+"  "+fg(C+B, "│"))
	fmt.Printf("%s\n", fg(C+B, "╰─"+strings.Repeat("─", len(stripANSII(header))+2)+"─╯"))
}

func stripANSII(s string) string {
	out := s
	for strings.Contains(out, "\033") {
		i := strings.Index(out, "\033")
		j := strings.Index(out[i:], "m")
		if j >= 0 { out = out[:i] + out[i+j+1:] } else { break }
	}
	return out
}

func drawHelp() {
	fmt.Printf("\n  %s\n", fg(B, "commands"))
	fmt.Printf("  %s  quit\n", fg(C, "/exit"))
	fmt.Printf("  %s  list all 25 models\n", fg(C, "/models"))
	fmt.Printf("  %s  switch model\n", fg(C, "/model <name>"))
	fmt.Printf("  %s  permission mode (bypass/plan/default/accept-edits)\n", fg(C, "/mode"))
	fmt.Printf("  %s  submit feedback\n", fg(C, "/feedback"))
	fmt.Printf("  %s  generate usage report\n", fg(C, "/share"))
	fmt.Printf("  %s  view analytics\n", fg(C, "/analytics"))
	fmt.Printf("  %s  auto-upload on/off\n\n", fg(C, "/uplink"))
}

func drawModels() {
	presets := config.ModelPresets()
	fmt.Println()
	last := ""
	for _, pr := range presets {
		if pr.Provider != last {
			fmt.Printf("  %s\n", fg(B+W, pr.Provider))
			last = pr.Provider
		}
		fmt.Printf("    %s%s  %s\n", fg(C, ""), pr.Name, fg(D, pr.Model))
	}
	fmt.Printf("\n  %s\n\n", fg(D, "/model <name> to switch"))
}

func drawUplink(text string) {
	cfg := telemetry.LoadUploadConfig()
	parts := strings.Fields(text)
	if len(parts) == 1 {
		s := "OFF"
		if cfg.Enabled { s = "ON" }
		fmt.Printf("\n  %s  %s\n", fg(B, "uplink ="), fg(C, s))
		fmt.Printf("  %s\n", fg(D, "/uplink on  —  enable auto-upload"))
		fmt.Printf("  %s\n", fg(D, "/uplink off —  disable"))
		fmt.Printf("  %s\n", fg(D, "requires: export GITHUB_TOKEN=ghp_..."))
		return
	}
	switch parts[1] {
	case "on":
		cfg.Enabled = true
		telemetry.SaveUploadConfig(cfg)
		fmt.Printf("\n  %s\n", fg(G+B, "uplink = ON"))
	case "off":
		cfg.Enabled = false
		telemetry.SaveUploadConfig(cfg)
		fmt.Printf("\n  %s\n", fg(D, "uplink = OFF"))
	default:
		fmt.Printf("\n  %s\n", fg(D, "/uplink on | /uplink off"))
	}
}

func onChatExit() {
	url, err := telemetry.MaybeUpload()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  %s: %v\n", fg(D, "upload skipped"), err)
		return
	}
	if url != "" {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n", fg(G, "report sent"), fg(C, url))
	}
}

// ── Markdown stripper ──────────────────────────────────────

func stripMD(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "####", "")
	s = strings.ReplaceAll(s, "###", "")
	s = strings.ReplaceAll(s, "##", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "|---", "")
	s = strings.ReplaceAll(s, "| ", "  ")
	return s
}
