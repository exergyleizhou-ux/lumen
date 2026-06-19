package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"lumen/internal/config"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/hermes"
	"lumen/internal/lineedit"
	"lumen/internal/permission"
	"lumen/internal/provider"
	"lumen/internal/telemetry"
	"lumen/internal/timeline"
	"lumen/internal/tui"
)

// ── live stats ────────────────────────────────────────────

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

// deepseekCost estimates the USD cost of one model call. Cache-hit input tokens
// bill at ~1/10 the miss rate on DeepSeek, so charging the whole prompt at the
// miss rate overstated the cost — badly on cached runs (often >90% hits).
func deepseekCost(promptTokens, cacheHitTokens, completionTokens int) float64 {
	const (
		missRate = 0.14 / 1e6  // input, cache miss
		hitRate  = 0.014 / 1e6 // input, cache hit (~10x cheaper)
		outRate  = 0.28 / 1e6  // output
	)
	miss := promptTokens - cacheHitTokens
	if miss < 0 {
		miss = 0
	}
	return float64(miss)*missRate + float64(cacheHitTokens)*hitRate + float64(completionTokens)*outRate
}

// usageCost returns the spend for a usage record using the active provider's
// configured pricing when set, and the built-in DeepSeek default rate otherwise
// (so a DeepSeek user with no [providers.pricing] block is unaffected, while any
// other provider's cost can be made accurate via config).
func usageCost(p *provider.Pricing, u *event.Usage) float64 {
	if p != nil {
		return (float64(u.CacheHitTokens)*p.CacheHit +
			float64(u.CacheMissTokens)*p.Input +
			float64(u.CompletionTokens)*p.Output) / 1e6
	}
	return deepseekCost(u.PromptTokens, u.CacheHitTokens, u.CompletionTokens)
}

// activePricing reports the active provider's configured rates, if any.
func activePricing() *provider.Pricing {
	if currentCtrl == nil {
		return nil
	}
	return currentCtrl.Pricing()
}

var st = &liveStats{}
var currentCtrl *control.Controller

// ── color / display helpers ───────────────────────────────

const R = "\033[0m"; const B = "\033[1m"; const D = "\033[2m"
const C = "\033[36m"; const G = "\033[32m"; const Rd = "\033[31m"
const W = "\033[97m"; const Y = "\033[33m"; const M = "\033[35m"

func fg(code, s string) string { return code + s + R }

// ── Sink: emoji-rich streaming output ──────────────────────

// verifyResultLine renders a verify-after-edit outcome for the non-interactive
// sink. The self-repair loop is Lumen's differentiator, but its result was
// previously dropped here (the case was a no-op), so a `lumen run` showed
// "verifying..." and then nothing. Success → green "✓ verified"; a failure or
// timeout → its text (which already carries the ✗ / "timed out" marker) in
// yellow, the attention-but-not-fatal color (a failure starts a repair cycle).
func verifyResultLine(level event.Level, text string) string {
	if level == event.LevelInfo {
		return "  " + G + "✓ verified" + R + "\n"
	}
	return "  " + Y + text + R + "\n"
}

func termSink() event.Sink {
	thinking := false
	textStarted := false
	textLen := 0
	truncated := false
	hadReasoning := false
	const maxOut = 48 * 1024
	tel := telemetry.NewCollector()
	steps := newToolStepRenderer()

	return event.FuncSink(func(e event.Event) {
		w := func(s string) {
			os.Stdout.WriteString(s)
			os.Stdout.Sync()
		}
		c := func(code, s string) string { return code + s + R }

		switch e.Kind {

		case event.TurnStarted:
			thinking = true; textStarted = false; textLen = 0; truncated = false; hadReasoning = false
			steps.reset()
			st.step.Store(0); st.turn.Add(1)
			tel.Record(telemetry.EventSessionStart, map[string]any{})
			w("  " + c(D, "⏵ Thinking…")) // show immediately so user knows it's working

		case event.Reasoning:
			if thinking && !textStarted {
				if rt := stripMD(e.Text); rt != "" {
					if !hadReasoning {
						// first reasoning: replace "Thinking…" with real thought
						w(c(D, rt))
						hadReasoning = true
					} else {
						w(c(D, rt)) // continue streaming reasoning
					}
				}
			}

		case event.Text:
			if thinking && !textStarted {
				thinking = false; textStarted = true
				if hadReasoning {
					w("\n") // extra blank line: reasoning → answer gap
				}
				w("\n" + c(C, "  ⏵ "))
			}
			if truncated { return }
			t := stripMD(e.Text); textLen += len(t)
			if textLen > maxOut { truncated = true; w("\n  ... too long\n"); return }
			w(t)

		case event.ToolDispatch:
			thinking = false; textStarted = true
			sn := st.step.Add(1)
			tel.Record(telemetry.EventToolCall, map[string]any{"name": e.Tool.Name, "step": sn})
			w(steps.dispatch(e.Tool.ID, e.Tool.Name, e.Tool.ReadOnly, int(sn)))

		case event.ToolResult:
			if e.Tool.Err != "" {
				tel.Record(telemetry.EventToolError, map[string]any{"name": e.Tool.Name, "error": e.Tool.Err})
			}
			w(steps.result(e.Tool.ID, e.Tool.Name, e.Tool.Err, e.Tool.Blocked))

		case event.UsageKind:
			if e.Usage != nil {
				tel.Record(telemetry.EventModelCall, map[string]any{
					"tokens_in": e.Usage.PromptTokens, "tokens_out": e.Usage.CompletionTokens, "total": e.Usage.TotalTokens,
				})
				// Accumulate (not Store) so the token counts share the cumulative
				// basis of the cost below — otherwise the footer mixed last-call
				// tokens with session-total cost across a multi-step turn.
				st.tkIn.Add(int64(e.Usage.PromptTokens)); st.tkOut.Add(int64(e.Usage.CompletionTokens))
				st.tkCache.Add(int64(e.Usage.CacheHitTokens))
				st.addCost(usageCost(activePricing(), e.Usage))
			}

		case event.FilePreview:
			w("\n  diff\n")
			for _, line := range strings.Split(e.DiffText, "\n") {
				w("  " + line + "\n")
			}

		case event.Notice:
			if e.Level == event.LevelWarn || e.Level == event.LevelErr {
				w("\n  " + e.Text + "\n")
			}

		case event.VerifyStarted:
			w("\n  verifying...\n")

		case event.VerifyResult:
			w(verifyResultLine(e.Level, e.Text))

		case event.TurnDone:
			thinking = false; textStarted = false; st.step.Store(0)
		}
	})
}

func toolIcon(name string) string {
	switch {
	case strings.HasPrefix(name, "read") || strings.HasPrefix(name, "lsp_") || name == "grep" || name == "glob": return "📖"
	case strings.HasPrefix(name, "write") || strings.HasPrefix(name, "edit") || name == "multi_edit" || name == "notebook_edit": return "✏️"
	case name == "bash": return "⚡"
	case strings.HasPrefix(name, "github"): return "🐙"
	case strings.HasPrefix(name, "llm") || strings.HasPrefix(name, "model"): return "🤖"
	case strings.HasPrefix(name, "screen") || strings.HasPrefix(name, "click") || strings.HasPrefix(name, "type") || strings.HasPrefix(name, "key") || strings.HasPrefix(name, "open") || strings.HasPrefix(name, "ui_"): return "🖥"
	case name == "workspace": return "🗂️"
	case strings.HasPrefix(name, "seal") || strings.HasPrefix(name, "sign") || strings.HasPrefix(name, "verify") || strings.HasPrefix(name, "audit"): return "🔐"
	case strings.HasPrefix(name, "graph") || strings.HasPrefix(name, "topology") || strings.HasPrefix(name, "detect") || strings.HasPrefix(name, "critical"): return "🕸️"
	case name == "run_mapreduce" || name == "stream_metrics": return "📊"
	case name == "ask" || name == "todo_write" || name == "complete_step": return "📋"
	case strings.HasPrefix(name, "mcp"): return "🔌"
	default: return "🔧"
	}
}

// ── Footer ─────────────────────────────────────────────────

func drawStatusLine(ctrl *control.Controller) {
	ti := st.tkIn.Load(); to := st.tkOut.Load(); tc := st.tkCache.Load()
	cost := st.cost(); steps := st.step.Load(); turns := st.turn.Load()
	if steps == 0 { steps = 1 }
	pct := 0; if ti > 0 { pct = int(float64(tc) / float64(ti) * 100) }

	fmt.Printf("\n  %s %s%s  [%.0fk tokens · ♻ %d%% · $%.4f · turn #%d]\n\n",
		fg(G, ctrl.ProviderName()), fg(C, ctrl.ModelName()),
		iconForMode(ctrl.PermissionMode()),
		float64(ti+to)/1000, pct, cost, turns)
}

func drawFooter() {
	ti := st.tkIn.Load(); to := st.tkOut.Load(); tc := st.tkCache.Load()
	cost := st.cost(); steps := st.step.Load(); turns := st.turn.Load()
	if steps == 0 { steps = 1 }
	pct := 0; if ti > 0 { pct = int(float64(tc) / float64(ti) * 100) }

	fmt.Printf("\n%s %s %s %s\n",
		fg(D, "  ["),
		fg(C, fmt.Sprintf("%.0fk tokens", float64(ti+to)/1000)),
		fg(D, fmt.Sprintf("· ♻ %s · %s · %s",
			fg(G, fmt.Sprintf("%d%%", pct)),
			fg(Y, fmt.Sprintf("$%.4f", cost)),
			fg(M, fmt.Sprintf("turn #%d", turns)))),
		fg(D, "]"))
}

// ── Chat loop ──────────────────────────────────────────────

func runChatUI(ctrl *control.Controller, modeOverride string) error {
	if err := ctrl.Configure(termSink(), newLineAsker(), ""); err != nil {
		return err
	}
	currentCtrl = ctrl
	defer func() { currentCtrl = nil }()
	if modeOverride != "" { ctrl.SetPermissionMode(permission.ParseMode(modeOverride)) }

	drawBanner(ctrl)

	memories := loadMemories()
	sess := ctrl.Session()
	if len(memories) > 0 || (sess != nil && sess.Len() > 0) {
		var parts []string
		if len(memories) > 0 { parts = append(parts, fmt.Sprintf("🧠 %d memories", len(memories))) }
		if sess != nil && sess.Len() > 0 { parts = append(parts, fmt.Sprintf("📂 %d msgs", sess.Len())) }
		fmt.Printf("  %s\n", fg(D, strings.Join(parts, " · ")))
	}

	// ── lineedit: full cursor movement, insert anywhere, ↑↓ history ──
	histPath := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "input_history")
	cwd, _ := os.Getwd()
	ed := lineedit.NewEditor("▸ ", histPath, cwd)
	// Enable bracketed paste once for the whole session so a paste that lands
	// while a turn is running is still wrapped as one block (not re-split into
	// line-by-line submits). Disabled on exit so the shell doesn't inherit it.
	ed.EnableBracketedPaste()
	defer ed.DisableBracketedPaste()

	history := make([]string, 0, 100)
	var lastPrompt string // for /retry after Ctrl+C interruption
	for {
	

		line, err := ed.ReadLine()
		if err != nil { onChatExit(); break }
		text := strings.TrimSpace(line)
		if text == "" { continue }

		if len(history) == 0 || history[len(history)-1] != text {
			history = append(history, text)
			if len(history) > 100 { history = history[1:] }
		}

		switch {
		case text == "/exit" || text == "/quit": onChatExit(); return nil
		case text == "/help": drawHelp(); continue
		case text == "/mode": fmt.Printf("\n  %s\n\n", fg(D, "🔓 bypass  🔒 plan  🛡 default  ✅ accept-edits")); continue
		case strings.HasPrefix(text, "/mode "):
			m := permission.ParseMode(strings.TrimPrefix(text, "/mode "))
			ctrl.SetPermissionMode(m)
			fmt.Printf("\n  %s\n", fg(B+C, iconForMode(m)+" mode = "+string(m)))
			continue
		case text == "/models": drawModels(); continue
		case strings.HasPrefix(text, "/model "):
			name := strings.TrimPrefix(text, "/model ")
			n, err := ctrl.SwitchModel(name)
			if err != nil { fmt.Printf("\n  %s\n", fg(Rd, "✗ "+err.Error())) } else { fmt.Printf("\n  %s\n", fg(G+B, "🔄 model = "+n)) }
			continue
		case text == "/feedback" || strings.HasPrefix(text, "/feedback "):
			parts := strings.Fields(text); msg := ""
			if len(parts) > 1 { msg = strings.Join(parts[1:], " ") }
			if _, err := telemetry.NewFeedbackStore().Submit("text", msg, "chat: "+text, ""); err != nil {
				fmt.Printf("\n  %s\n", fg(Rd, "✗ feedback not saved: "+err.Error()))
			} else {
				fmt.Printf("\n  %s\n", fg(G, "💬 feedback recorded — thank you!"))
			}
			continue
		case text == "/share":
			f, err := telemetry.ShareReport()
			if err != nil { fmt.Printf("\n  %s\n", fg(Rd, "✗ "+err.Error())) } else { fmt.Printf("\n  %s  %s\n", fg(B, "📊 report →"), fg(C, f)) }
			continue
		case text == "/uplink" || strings.HasPrefix(text, "/uplink "): drawUplink(text); continue
		case text == "/analytics":
			fmt.Printf("\n%s\n", telemetry.FormatReport(telemetry.NewAnalyzer().Analyze("week")))
			continue
		}

		if strings.HasPrefix(text, "/workflow ") {
			runWorkflow(ctrl, strings.TrimPrefix(text, "/workflow ")); continue
		}
		if strings.HasPrefix(text, "/ultra ") {
			runUltra(ctrl, strings.TrimPrefix(text, "/ultra ")); continue
		}
		if text == "/undo" {
			rewound, err := ctrl.Rewind()
			if err != nil { fmt.Printf("\n  %s\n", fg(Rd, "✗ "+err.Error())) } else { fmt.Printf("\n  %s\n", formatRewound(rewound)) }
			continue
		}
		if text == "/status" { drawStatusLine(ctrl); continue }
		if text == "/cost" { drawCost(); continue }
		if text == "/cache" { drawCache(); continue }
		if text == "/rewind" { drawRewind(); continue }
		if text == "/replay" { drawReplay(); continue }
		if text == "/changes" { drawChanges(); continue }
		if text == "/retry" {
			if lastPrompt == "" { fmt.Printf("\n  %s\n", fg(D, "no previous task to retry")); continue }
			text = lastPrompt
		}
		if text == "/wizard" { runWizard(ctrl); continue }
		if strings.HasPrefix(text, "/goal ") {
			runGoalMode(ctrl, strings.TrimPrefix(text, "/goal "))
			continue
		}
		if text == "/evolve" {
			runEvolve(); continue
		}
		if text == "/execute" && planReady {
			fmt.Printf("\n  %s\n", fg(B, "🚀 Executing Plan"))
			ctrl.Agent().SetPlanMode(false); ctrl.SetPermissionMode(permission.ModeBypass)
			ctrl.Run(context.Background(), lastPlan); planReady = false
			fmt.Printf("\n  %s\n", fg(G, "✅ workflow complete"))
			continue
		}
		if text == "/reject" && planReady {
			fmt.Printf("\n  %s\n", fg(D, "✗ plan rejected")); planReady = false; continue
		}
		if text == "/execute" && !planReady { fmt.Printf("\n  %s\n", fg(D, "no plan ready — use /workflow <task> first")); continue }
		if text == "/history" {
			fmt.Printf("\n  %s\n", fg(D, "📜 recent:")); start := 0
			if len(history) > 20 { start = len(history) - 20 }
			for i := start; i < len(history); i++ { fmt.Printf("    %s\n", fg(D, history[i])) }; fmt.Println(); continue
		}
		if strings.HasPrefix(text, "/") && !strings.Contains(text, " ") {
			if dispatchSkill(ctrl, strings.TrimPrefix(text, "/"), "") { continue }
			fmt.Printf("\n  %s\n", fg(D, "unknown command · /help for help · /models for models"))
			continue
		}
		lastPrompt = text // save for /retry after interruption
		costBefore := st.cost()
		turnCtx, turnCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT)
		// sigDone lets the watcher goroutine exit when the turn ends. Without it
		// the goroutine blocks on <-sigCh forever (signal.Stop doesn't close the
		// channel), leaking one goroutine per turn over a long session.
		sigDone := make(chan struct{})
		go func() {
			select {
			case <-sigCh:
				turnCancel()
			case <-sigDone:
			}
		}()
		err = ctrl.Run(turnCtx, text)
		turnCancel(); signal.Stop(sigCh); close(sigDone)
		// Guard against silent turns. A real completion always bills output
		// tokens, so the (cumulative) cost strictly increases. If it didn't move
		// and the agent surfaced no error, the model produced nothing — most
		// often because the context grew too long. Tell the user how to recover
		// instead of leaving a frozen "⏵ Thinking…" on screen.
		if err == nil && st.cost() == costBefore {
			os.Stdout.WriteString("\n  ⚡ no model response — the context may be too long. Try /undo to trim history, /model to switch provider, or restart the chat.\n")
		}
		drawFooter()
		fmt.Print("\n")
	}
	return nil
}

// ── Drawing ────────────────────────────────────────────────

func drawBanner(ctrl *control.Controller) {
	cwd, _ := os.Getwd()
	if home, _ := os.UserHomeDir(); home != "" {
		cwd = strings.Replace(cwd, home, "~", 1)
	}
	fmt.Printf("\n%s  %s\n",
		fg(C+B, "●") + " " + fg(B+W, "LUMEN"),
		fg(D, fmt.Sprintf("%s/%s · %s · %s",
			fg(G, ctrl.ProviderName()), fg(C, ctrl.ModelName()),
			string(ctrl.PermissionMode()), cwd)))
}

func iconForMode(m permission.Mode) string {
	switch m { case permission.ModeBypass: return "🔓"; case permission.ModePlan: return "🔒"; case permission.ModeDefault: return "🛡"; case permission.ModeAcceptEdits: return "✅"; default: return "❓" }
}

func drawHelp() {
	fmt.Printf("\n%s\n", fg(B, "  Commands"))
	fmt.Printf("  %s  %s\n", fg(C, "/wizard"),    fg(D, "AI interviews you, then builds"))
	fmt.Printf("  %s  %s\n", fg(C, "/goal <t>"),   fg(D, "autonomous goal execution"))
	fmt.Printf("  %s  %s\n", fg(C, "/workflow"),   fg(D, "plan → review → execute"))
	fmt.Printf("  %s  %s\n", fg(C, "/ultra"),      fg(D, "plan → auto-execute"))
	fmt.Printf("  %s  %s\n", fg(C, "/models"),     fg(D, "list available models"))
	fmt.Printf("  %s  %s\n", fg(C, "/model <n>"),  fg(D, "switch model"))
	fmt.Printf("  %s  %s\n", fg(C, "/mode"),       fg(D, "🔓 bypass  🔒 plan  🛡 default  ✅ accept-edits"))
	fmt.Printf("  %s  %s\n", fg(C, "/undo"),       fg(D, "undo last file edits"))
	fmt.Printf("  %s  %s\n", fg(C, "/retry"),      fg(D, "retry last task after Ctrl+C"))
	fmt.Printf("  %s  %s\n", fg(C, "/status"),     fg(D, "agent stats"))
	fmt.Printf("  %s  %s\n", fg(C, "/feedback"),   fg(D, "submit feedback"))
	fmt.Printf("  %s  %s\n", fg(C, "/history"),    fg(D, "recent messages"))
	fmt.Printf("  %s  %s\n\n", fg(C, "/<skill>"),  fg(D, "invoke skill"))
}

func drawModels() {
	presets := config.ModelPresets()
	fmt.Println()
	last := ""
	nameW, modelW := 0, 0
	for _, pr := range presets {
		if len(pr.Name) > nameW { nameW = len(pr.Name) }
		if len(pr.Model) > modelW { modelW = len(pr.Model) }
	}
	for _, pr := range presets {
		if pr.Provider != last {
			if last != "" { fmt.Println() }
			fmt.Printf("  %s %s\n", fg(B+W, providerIcon(pr.Provider)), fg(B+C, pr.Provider))
			last = pr.Provider
		}
		fmt.Printf("    %s %s%s  %s%s\n",
			fg(G, "▸"),
			fg(C, pr.Name), strings.Repeat(" ", nameW-len(pr.Name)+2),
			fg(D, pr.Model), strings.Repeat(" ", modelW-len(pr.Model)))
	}
	fmt.Printf("\n  %s\n\n", fg(D, "▸ /model <name> to switch"))
}

func providerIcon(p string) string {
	switch p { case "openai": return "🧠"; case "anthropic": return "🏛"; case "deepseek": return "🔍"; case "xai": return "⚡"; case "moonshot": return "🌙"; case "qwen": return "🐉"; case "zhipu": return "🔥"; case "mimo": return "🤖"; case "google": return "💎"; default: return "🤖" }
}

func drawUplink(text string) {
	cfg := telemetry.LoadUploadConfig(); parts := strings.Fields(text)
	if len(parts) == 1 {
		s := "OFF"; if cfg.Enabled { s = "ON" }
		fmt.Printf("\n  %s\n  %s\n  %s\n  %s\n", fg(B, "☁️  uplink = "+s), fg(D, "/uplink on/off"), fg(D, "auto-sends reports to GitHub Issues"), fg(D, "requires: export GITHUB_TOKEN=ghp_..."))
		return
	}
	switch parts[1] {
	case "on":
		cfg.Enabled = true
		if err := telemetry.SaveUploadConfig(cfg); err != nil {
			fmt.Printf("\n  %s\n", fg(Rd, "✗ uplink not saved: "+err.Error()))
		} else {
			fmt.Printf("\n  %s\n", fg(G+B, "☁️ uplink = ON"))
		}
	case "off":
		cfg.Enabled = false
		if err := telemetry.SaveUploadConfig(cfg); err != nil {
			fmt.Printf("\n  %s\n", fg(Rd, "✗ uplink not saved: "+err.Error()))
		} else {
			fmt.Printf("\n  %s\n", fg(D, "☁️ uplink = OFF"))
		}
	}
}

func onChatExit() {
	// Save session mark for next startup resume
	if ctrl := currentCtrl; ctrl != nil {
		ctrl.SaveMark()
	}
	url, err := telemetry.MaybeUpload()
	if err != nil {
		// Suppress non-actionable errors — user hasn't configured upload
		if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
			fmt.Fprintf(os.Stderr, "\n  %s\n", fg(D, "☁️ upload: "+err.Error()))
		}
	} else if url != "" {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n", fg(G, "☁️ report sent"), fg(C, url))
	}
}


// ── Workflow / Ultra ──────────────────────────────────────

func runWorkflow(ctrl *control.Controller, prompt string) {
	fmt.Printf("\n  %s\n  %s\n\n", fg(B, "📋 Plan Phase"), fg(D, "producing plan in read-only mode…"))
	ctrl.SetPermissionMode(permission.ModePlan); ctx := context.Background()
	if err := ctrl.Plan(ctx, prompt); err != nil { return } // error shown via sink
	fmt.Printf("\n  %s\n  %s\n  %s  %s\n", fg(B, "👀 Review"), fg(C, "plan above — review carefully."), fg(C, "/execute"), fg(D, "/reject"))
	lastPlan = prompt; planReady = true
}

func runUltra(ctrl *control.Controller, prompt string) {
	fmt.Printf("\n  %s\n  %s\n\n", fg(B, "⚡ Ultra Mode"), fg(D, "plan → auto-approve → execute"))
	ctrl.SetPermissionMode(permission.ModePlan); ctx := context.Background()
	if err := ctrl.Plan(ctx, prompt); err != nil { return } // error shown via sink
	ctrl.Agent().SetPlanMode(false); ctrl.SetPermissionMode(permission.ModeBypass)
	fmt.Printf("\n  %s\n", fg(B, "🚀 Executing"))
	if err := ctrl.Run(ctx, lastPlan); err != nil { return } // error shown via sink
	fmt.Printf("\n  %s\n", fg(G, "✅ ultra workflow complete"))
}

func runEvolve() {
	fmt.Printf("\n  %s\n", fg(B, "🧬 Evolving Knowledge Base"))
	pb := hermes.NewKnowledgeBase(); before := len(pb.Patterns)
	telemDir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "telemetry")
	entries, _ := os.ReadDir(telemDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") { continue }
		data, _ := os.ReadFile(filepath.Join(telemDir, e.Name()))
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "\"tool_error\"") { continue }
			parts := strings.SplitN(line, "\"error\":\"", 2)
			if len(parts) > 1 {
				errPart := strings.SplitN(parts[1], "\"", 2)[0]
				pb.Learn("fix", truncProb(errPart, 30), "Auto-detected: "+truncProb(errPart, 50), "telemetry")
			}
		}
	}
	after := len(pb.Patterns)
	if after == before { fmt.Printf("  %s\n\n", fg(D, "no new patterns — keep using lumen!")) } else {
		fmt.Printf("  %s\n  %s  %s\n\n", fg(G, fmt.Sprintf("🧬 %d new patterns learned", after-before)), fg(D, "knowledge base:"), fg(C, fmt.Sprintf("%d patterns", after)))
	}
}

// ── Helpers ────────────────────────────────────────────────

var lastPlan string; var planReady bool


func loadMemories() []string {
	wd, _ := os.Getwd(); root := wd
	for dir := wd; dir != "/" && dir != "."; dir = filepath.Dir(dir) { if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil { root = dir; break } }
	var loaded []string
	for _, name := range []string{"CLAUDE.md", "AGENTS.md", "REASONIX.md", "LUMEN.md", "README.md"} { if _, err := os.Stat(filepath.Join(root, name)); err == nil { loaded = append(loaded, name) } }
	return loaded
}

func dispatchSkill(ctrl *control.Controller, name, rest string) bool {
	skills := ctrl.Skills(); if skills == nil { return false }
	for _, sk := range skills.List() {
		if strings.EqualFold(sk.Name, name) {
			fmt.Printf("\n  %s  %s\n", fg(C, "🎯 skill:"), fg(B, sk.Name))
			// Tell the LLM to invoke the skill — includes the skill name so it knows
			// exactly which run_skill tool call to make.
			prompt := fmt.Sprintf("run the %s skill", sk.Name)
			if rest != "" {
				prompt = fmt.Sprintf("run the %s skill with arguments: %s", sk.Name, rest)
			}
			ctrl.Run(context.Background(), prompt); fmt.Print("\n"); return true
		}
	}
	return false
}

func runGoalMode(ctrl *control.Controller, prompt string) {
	fmt.Printf("\n%s\n", fg(B, "🎯 goal: ")+fg(C, prompt))
	kb := hermes.NewKnowledgeBase(); patterns := kb.Match(prompt)
	if len(patterns) > 0 {
		fmt.Printf("  %s\n", fg(D, fmt.Sprintf("🧠 %d knowledge patterns matched", len(patterns))))
		for _, p := range patterns { fmt.Printf("    %s  %s → %s\n", fg(C, "["+p.Category+"]"), fg(D, p.Condition), fg(D, p.Action)) }
	}
	fmt.Printf("  %s  %s\n\n", fg(B, "🤖 Autonomous Execution"), fg(D, "running to completion…"))
	// Actually run the agent to completion. Previously this printed the banner
	// but never invoked the model — a no-op that claimed success.
	if err := ctrl.Run(context.Background(), prompt); err != nil {
		return // error already surfaced via the event sink
	}
	fmt.Printf("\n  %s\n", fg(G, "✅ goal complete"))
}

func stripMD(s string) string {
	s = strings.ReplaceAll(s, "**", ""); s = strings.ReplaceAll(s, "__", ""); s = strings.ReplaceAll(s, "`", ""); s = strings.ReplaceAll(s, "####", ""); s = strings.ReplaceAll(s, "###", ""); s = strings.ReplaceAll(s, "##", ""); s = strings.ReplaceAll(s, "*", ""); s = strings.ReplaceAll(s, "|---", ""); s = strings.ReplaceAll(s, "| ", "  ")
	return s
}
func truncProb(s string, n int) string { if len(s) <= n { return s }; return s[:n-3] + "..." }

// ── slash command helpers ─────────────────────────────────

func drawCost() {
	ag := currentCtrl.Agent()
	if ag == nil { fmt.Printf("\n  %s\n", fg(Rd, "no agent")); return }
	var sb strings.Builder
	sb.WriteString("\n  Token usage\n  ───────────\n")
	cacheHit, cacheMiss := ag.SessionCache()
	last := ag.LastUsage()
	if last != nil {
		fmt.Fprintf(&sb, "  Last turn: %d tokens\n", last.TotalTokens)
		if last.CacheHitTokens+last.CacheMissTokens > 0 {
			rate := float64(last.CacheHitTokens) / float64(last.CacheHitTokens+last.CacheMissTokens) * 100
			fmt.Fprintf(&sb, "  Cache: %.0f%% (%d hit / %d miss)\n", rate, last.CacheHitTokens, last.CacheHitTokens+last.CacheMissTokens)
		}
	}
	fmt.Fprintf(&sb, "  Session: %d hit + %d miss\n", cacheHit, cacheMiss)
	ti := st.tkIn.Load(); to := st.tkOut.Load()
	fmt.Fprintf(&sb, "  Total: %.0fk tokens  ·  $%.4f\n", float64(ti+to)/1000, st.cost())
	fmt.Print(sb.String())
}

func drawCache() {
	ti, tc := st.tkIn.Load(), st.tkCache.Load()
	pct := 0; if ti > 0 { pct = int(float64(tc) / float64(ti) * 100) }
	fmt.Printf("\n  Cache efficiency\n  ────────────────\n")
	fmt.Printf("  Input tokens:  %d\n", ti)
	fmt.Printf("  Cache hits:    %d (♻ %d%%)\n", tc, pct)
	fmt.Printf("  Cache misses:  %d\n\n", ti-tc)
}

func drawRewind() {
	rewound, err := currentCtrl.Rewind()
	if err != nil { fmt.Printf("\n  %s\n", fg(Rd, "✗ "+err.Error())); return }
	fmt.Printf("\n  %s\n", formatRewound(rewound))
}

// formatRewound renders the rewound-file list for humans instead of dumping a
// raw Go slice ("[a.go b.go]").
func formatRewound(rewound []string) string {
	if len(rewound) == 0 {
		return fg(D, "↩ nothing to undo")
	}
	return fmt.Sprintf("%s: %s", fg(G, fmt.Sprintf("↩ rewound %d file(s)", len(rewound))), strings.Join(rewound, ", "))
}

func drawReplay() {
	entries, err := timeline.LoadTimeline(".lumen/timeline.jsonl")
	if err != nil || len(entries) == 0 { fmt.Printf("\n  %s\n", fg(D, "no timeline yet")); return }
	fmt.Printf("\n%s\n", timeline.FormatTimeline(entries))
}

func drawChanges() {
	changes, err := timeline.LoadChanges(".lumen/timeline.jsonl")
	if err != nil || len(changes) == 0 { fmt.Printf("\n  %s\n", fg(D, "no changes yet")); return }
	fmt.Printf("\n%s\n", timeline.FormatChanges(changes))
}

// ── session helpers ───────────────────────────────────────

func loadLastSession(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".last_session"))
	if err != nil { return "" }
	name := strings.TrimSpace(string(data))
	if _, err := os.Stat(filepath.Join(dir, name)); err == nil { return name }
	return ""
}

func saveLastSession(dir, filename string) {
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, ".last_session"), []byte(filename), 0600)
}

// ── TUI sink bridge ───────────────────────────────────────

func tuiSink(model *tui.Model) event.Sink {
	textBuf := strings.Builder{}
	step := int64(0)
	stepByID := map[string]int{} // tool call ID → its dispatch step (parallel-safe)

	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted:
			textBuf.Reset(); step = 0
			model.Send(tui.StatusMsg{State: "thinking"})
			model.Send(tui.VerifyMsg{State: ""}) // clear last turn's verify indicator

		case event.VerifyStarted:
			model.Send(tui.VerifyMsg{State: "running"})

		case event.VerifyResult:
			vs := "ok"
			if e.Level == event.LevelWarn || e.Level == event.LevelErr {
				vs = "fail"
			}
			// A timeout is inconclusive, not a failure — show it neutrally.
			if strings.HasPrefix(e.Text, "verify timed out") {
				vs = "skip"
			}
			model.Send(tui.VerifyMsg{State: vs, Detail: e.Text})

		case event.Text:
			textBuf.WriteString(e.Text)

		case event.ToolDispatch:
			step++
			stepByID[e.Tool.ID] = int(step)
			st := "running"
			if e.Tool.ReadOnly {
				st = "done"
			}
			model.Send(tui.TuiMsg{
				Role: "tool",
				ToolCalls: []tui.ToolCall{{
					Name:   e.Tool.Name,
					Input:  e.Tool.Args,
					Status: st,
					Step:   int(step),
				}},
			})

		case event.ToolResult:
			status := "done"
			if e.Tool.Err != "" {
				status = "error"
			}
			if e.Tool.Blocked {
				status = "blocked"
			}
			// Use THIS tool's dispatch step (parallel batches share the counter),
			// so the result coalesces onto its own row, not the latest dispatch's.
			s := stepByID[e.Tool.ID]
			delete(stepByID, e.Tool.ID)
			model.Send(tui.TuiMsg{
				Role: "tool",
				ToolCalls: []tui.ToolCall{{
					Name:   e.Tool.Name,
					Output: e.Tool.Output,
					Error:  e.Tool.Err,
					Status: status,
					Step:   s,
				}},
			})

		case event.UsageKind:
			if e.Usage != nil {
				// Accumulate to share the cumulative basis of the cost (see termSink).
				st.tkIn.Add(int64(e.Usage.PromptTokens))
				st.tkOut.Add(int64(e.Usage.CompletionTokens))
				st.tkCache.Add(int64(e.Usage.CacheHitTokens))
				st.addCost(usageCost(activePricing(), e.Usage))
			}

		case event.TurnDone:
			content := textBuf.String()
			if content != "" {
				model.Send(tui.TuiMsg{Role: "assistant", Content: content})
			}
			model.Send(tui.StatusMsg{
				Model:    "", // preserve existing model display
				Provider: "",
				TokensIn: st.tkIn.Load(), TokensOut: st.tkOut.Load(),
				CacheHit: st.tkCache.Load(),
				Cost:     st.cost(),
				Steps:    step,
				State:    "idle",
			})
		}
	})
}
