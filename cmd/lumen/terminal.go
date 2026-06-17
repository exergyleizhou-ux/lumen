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
var st = &liveStats{}
var currentCtrl *control.Controller
var outputBuf strings.Builder // accumulates agent output during raw mode

// ── color / display helpers ───────────────────────────────

const R = "\033[0m"; const B = "\033[1m"; const D = "\033[2m"
const C = "\033[36m"; const G = "\033[32m"; const Rd = "\033[31m"
const W = "\033[97m"; const Y = "\033[33m"; const M = "\033[35m"

func fg(code, s string) string { return code + s + R }

// ── Sink: emoji-rich streaming output ──────────────────────

func termSink() event.Sink {
	thinking := false
	textStarted := false
	textLen := 0
	truncated := false
	const maxOut = 48 * 1024
	tel := telemetry.NewCollector()

	return event.FuncSink(func(e event.Event) {
		// Buffer ALL output during the turn. The chat loop flushes
		// the buffer to stdout between ReadLine() calls, when the
		// terminal is briefly outside raw mode.
		w := func(s string) { outputBuf.WriteString(s) }

		switch e.Kind {

		case event.TurnStarted:
			thinking = true; textStarted = false; textLen = 0; truncated = false
			st.step.Store(0); st.turn.Add(1)
			tel.Record(telemetry.EventSessionStart, map[string]any{})
			w("\n  \u23f3\n")

		case event.Reasoning:
			if thinking && !textStarted {
				if rt := stripMD(e.Text); rt != "" {
					w(rt)
				}
			}

		case event.Text:
			if thinking && !textStarted {
				thinking = false; textStarted = true
				w("\n")
			}
			if truncated { return }
			t := stripMD(e.Text); textLen += len(t)
			if textLen > maxOut { truncated = true; w("\n  ... too long\n"); return }
			w(t)

		case event.ToolDispatch:
			thinking = false; textStarted = true
			sn := st.step.Add(1)
			tel.Record(telemetry.EventToolCall, map[string]any{"name": e.Tool.Name, "step": sn})
			w(fmt.Sprintf("\n\n  %2d. %s %s", sn, toolIcon(e.Tool.Name), e.Tool.Name))

		case event.ToolResult:
			if e.Tool.Err != "" {
				tel.Record(telemetry.EventToolError, map[string]any{"name": e.Tool.Name, "error": e.Tool.Err})
				w(fmt.Sprintf("  \u2717 %s\n", e.Tool.Err))
			} else if e.Tool.Blocked {
				w("  \u26d4\n")
			} else {
				suffix := ""
				if out := strings.TrimSpace(e.Tool.Output); out != "" {
					first := strings.SplitN(out, "\n", 2)[0]
					if len(first) > 60 { first = first[:57] + "..." }
					suffix = "  " + first
				}
				w(" \u2713" + suffix + "\n")
			}
			w("  \u23f3\n")

		case event.UsageKind:
			if e.Usage != nil {
				tel.Record(telemetry.EventModelCall, map[string]any{
					"tokens_in": e.Usage.PromptTokens, "tokens_out": e.Usage.CompletionTokens, "total": e.Usage.TotalTokens,
				})
				st.tkIn.Store(int64(e.Usage.PromptTokens)); st.tkOut.Store(int64(e.Usage.CompletionTokens))
				st.tkCache.Store(int64(e.Usage.CacheHitTokens))
				st.addCost(float64(e.Usage.PromptTokens)*0.14/1e6 + float64(e.Usage.CompletionTokens)*0.28/1e6)
			}

		case event.FilePreview:
			w("\n  diff\n")
			for _, line := range strings.Split(e.DiffText, "\n") {
				w("  " + line + "\n")
			}

		case event.Notice:
			if e.Level == event.LevelWarn || e.Level == event.LevelErr {
				w("\n  \u26a1 " + e.Text + "\n")
			} else {
				w("\n  i " + e.Text + "\n")
			}

		case event.VerifyStarted:
			w("\n  verifying...\n")

		case event.VerifyResult:
			// nothing

		case event.TurnDone:
			thinking = false; textStarted = false; st.step.Store(0)
			drawFooter()
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

	fmt.Fprintf(os.Stdout, "\n%s %s  %s%s  %s  %s  %s  %s\n\n",
		fg(D, "  ·"),
		fg(C, fmt.Sprintf("%s/%s", ctrl.ProviderName(), ctrl.ModelName())),
		iconForMode(ctrl.PermissionMode()), fg(D, string(ctrl.PermissionMode())),
		fg(C, fmt.Sprintf("📊 %.0fk", float64(ti+to)/1000)),
		fg(G, fmt.Sprintf("♻ %d%%", pct)),
		fg(Y, fmt.Sprintf("💰 $%.4f", cost)),
		fg(M, fmt.Sprintf("⚙ %dst · #%d", steps, turns)))
}

func drawFooter() {
	// Thin wrapper kept for backwards compat; status line drawn inline.
	ti := st.tkIn.Load(); to := st.tkOut.Load(); tc := st.tkCache.Load()
	cost := st.cost(); steps := st.step.Load(); turns := st.turn.Load()
	if steps == 0 { steps = 1 }
	pct := 0; if ti > 0 { pct = int(float64(tc) / float64(ti) * 100) }

	fmt.Fprintf(os.Stdout, "\n%s %s  %s  %s  %s\n\n",
		fg(D, "  ·"),
		fg(C, fmt.Sprintf("📊 %.0fk", float64(ti+to)/1000)),
		fg(G, fmt.Sprintf("♻ %d%%", pct)),
		fg(Y, fmt.Sprintf("💰 $%.4f", cost)),
		fg(M, fmt.Sprintf("⚙ %dst · turn #%d", steps, turns)))
}

// ── Chat loop ──────────────────────────────────────────────

func runChatUI(ctrl *control.Controller, modeOverride string) error {
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		return err
	}
	currentCtrl = ctrl
	defer func() { currentCtrl = nil }()
	if modeOverride != "" { ctrl.SetPermissionMode(permission.ParseMode(modeOverride)) }

	drawBanner(ctrl)

	memories := loadMemories()
	if len(memories) > 0 {
		fmt.Printf("  %s\n", fg(D, fmt.Sprintf("🧠 %d context file(s) loaded", len(memories))))
	}

	// ── Session resume — handled by Configure, just show info ──
	if sess := ctrl.Session(); sess != nil && sess.Len() > 0 {
		fmt.Printf("  %s  %s\n", fg(D, "📂 resumed:"), fg(C, fmt.Sprintf("%d messages", sess.Len())))
	}

	// ── lineedit: full cursor movement, insert anywhere, ↑↓ history ──
	histPath := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "input_history")
	cwd, _ := os.Getwd()
	ed := lineedit.NewEditor("▸ ", histPath, cwd)

	history := make([]string, 0, 100)
	var lastPrompt string // for /retry after Ctrl+C interruption
	for {
		// Print mode line above prompt (outside raw mode)
		fmt.Fprintf(os.Stdout, "\n%s\n", fg(D, "  ["+iconForMode(ctrl.PermissionMode())+" "+string(ctrl.PermissionMode())+"]"))

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
			telemetry.NewFeedbackStore().Submit("text", msg, "chat: "+text, "")
			fmt.Printf("\n  %s\n", fg(G, "💬 feedback recorded — thank you!"))
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
			if err != nil { fmt.Printf("\n  %s\n", fg(Rd, "✗ "+err.Error())) } else { fmt.Printf("\n  %s  %v\n", fg(G, "↩ rewound"), rewound) }
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
			go runGoalMode(ctrl, strings.TrimPrefix(text, "/goal "))
			fmt.Printf("\n  %s\n\n", fg(G+B, "🎯 goal started · working autonomously…"))
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
			fmt.Printf("\n  %s\n\n", fg(D, "unknown command · /help for help · /models for models"))
			continue
		}
		fmt.Printf("\n%s\n", fg(B+C, text))
		lastPrompt = text // save for /retry after interruption
		turnCtx, turnCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT)
		go func() { <-sigCh; turnCancel() }()
		outputBuf.Reset()
		ctrl.Run(turnCtx, text)
		turnCancel(); signal.Stop(sigCh)
		// Flush buffered agent output before lineedit re-enters raw mode
		if out := outputBuf.String(); out != "" {
			fmt.Fprint(os.Stdout, out)
		}
		drawFooter()
		fmt.Print("\n")
	}
	return nil
}

// ── Drawing ────────────────────────────────────────────────

func drawBanner(ctrl *control.Controller) {
	header := fmt.Sprintf("🪄  %s  ·  %s/%s  ·  %s %s",
		fg(B+W, "LUMEN"),
		fg(G, ctrl.ProviderName()),
		fg(C, ctrl.ModelName()),
		iconForMode(ctrl.PermissionMode()),
		fg(D, string(ctrl.PermissionMode())))
	fmt.Printf("\n  %s\n\n", header)
}

func iconForMode(m permission.Mode) string {
	switch m { case permission.ModeBypass: return "🔓"; case permission.ModePlan: return "🔒"; case permission.ModeDefault: return "🛡"; case permission.ModeAcceptEdits: return "✅"; default: return "❓" }
}

func drawHelp() {
	fmt.Printf("\n  %s\n", fg(B, "commands"))
	fmt.Printf("  %s  ✨ AI interviews you, then builds\n", fg(C, "/wizard"))
	fmt.Printf("  %s  🎯 autonomous goal execution\n", fg(C, "/goal <task>"))
	fmt.Printf("  %s  📋 plan → review → execute\n", fg(C, "/workflow <task>"))
	fmt.Printf("  %s  ⚡ plan → auto-execute\n", fg(C, "/ultra <task>"))
	fmt.Printf("  %s  ↩ undo last file edits\n", fg(C, "/undo"))
	fmt.Printf("  %s  🔄 retry last task (after Ctrl+C)\n", fg(C, "/retry"))
	fmt.Printf("  %s  🏥 agent status\n", fg(C, "/status"))
	fmt.Printf("  %s  🗂️  list 26 models\n", fg(C, "/models"))
	fmt.Printf("  %s  🔄 switch model\n", fg(C, "/model <name>"))
	fmt.Printf("  %s  🔓🔒🛡 permission modes\n", fg(C, "/mode"))
	fmt.Printf("  %s  💬 submit feedback\n", fg(C, "/feedback"))
	fmt.Printf("  %s  📊 generate report\n", fg(C, "/share"))
	fmt.Printf("  %s  📈 view analytics\n", fg(C, "/analytics"))
	fmt.Printf("  %s  ☁️  auto-upload\n", fg(C, "/uplink"))
	fmt.Printf("  %s  🧬 evolve knowledge base\n", fg(C, "/evolve"))
	fmt.Printf("  %s  📜 recent messages\n", fg(C, "/history"))
	fmt.Printf("  %s  🎯 invoke skill\n\n", fg(C, "/<skill>"))
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
	case "on": cfg.Enabled = true; telemetry.SaveUploadConfig(cfg); fmt.Printf("\n  %s\n", fg(G+B, "☁️ uplink = ON"))
	case "off": cfg.Enabled = false; telemetry.SaveUploadConfig(cfg); fmt.Printf("\n  %s\n", fg(D, "☁️ uplink = OFF"))
	}
}

func onChatExit() {
	// Save session mark for next startup resume
	if ctrl := currentCtrl; ctrl != nil {
		ctrl.SaveMark()
	}
	url, err := telemetry.MaybeUpload()
	if err != nil { fmt.Fprintf(os.Stderr, "\n  %s\n", fg(D, "☁️ upload: "+err.Error())); return }
	if url != "" { fmt.Fprintf(os.Stderr, "\n  %s %s\n", fg(G, "☁️ report sent"), fg(C, url)) }
}


// ── Workflow / Ultra ──────────────────────────────────────

func runWorkflow(ctrl *control.Controller, prompt string) {
	fmt.Printf("\n  %s\n  %s\n\n", fg(B, "📋 Plan Phase"), fg(D, "producing plan in read-only mode…"))
	ctrl.SetPermissionMode(permission.ModePlan); ctx := context.Background()
	if err := ctrl.Plan(ctx, prompt); err != nil { fmt.Printf("  %s\n", fg(Rd, "✗ "+err.Error())); return }
	fmt.Printf("\n  %s\n  %s\n  %s  %s\n", fg(B, "👀 Review"), fg(C, "plan above — review carefully."), fg(C, "/execute"), fg(D, "/reject"))
	lastPlan = prompt; planReady = true
}

func runUltra(ctrl *control.Controller, prompt string) {
	fmt.Printf("\n  %s\n  %s\n\n", fg(B, "⚡ Ultra Mode"), fg(D, "plan → auto-approve → execute"))
	ctrl.SetPermissionMode(permission.ModePlan); ctx := context.Background()
	if err := ctrl.Plan(ctx, prompt); err != nil { fmt.Printf("\n  %s\n", fg(Rd, "✗ "+err.Error())); return }
	ctrl.Agent().SetPlanMode(false); ctrl.SetPermissionMode(permission.ModeBypass)
	fmt.Printf("\n  %s\n", fg(B, "🚀 Executing"))
	if err := ctrl.Run(ctx, lastPlan); err != nil { fmt.Printf("\n  %s\n", fg(Rd, "✗ "+err.Error())) }
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
	fmt.Printf("  %s  %s\n\n", fg(B, "🤖 Autonomous Execution"), fg(D, "running to completion…"))
	fmt.Printf("  %s\n", fg(D, fmt.Sprintf("🧠 %d knowledge patterns matched", len(patterns))))
	for _, p := range patterns { fmt.Printf("    %s  %s → %s\n", fg(C, "["+p.Category+"]"), fg(D, p.Condition), fg(D, p.Action)) }
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
	fmt.Printf("\n  %s  %v\n", fg(G, "↩ rewound"), rewound)
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
			model.Send(tui.VerifyMsg{State: vs, Detail: e.Text})

		case event.Text:
			textBuf.WriteString(e.Text)

		case event.ToolDispatch:
			step++
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
			model.Send(tui.TuiMsg{
				Role: "tool",
				ToolCalls: []tui.ToolCall{{
					Name:   e.Tool.Name,
					Output: e.Tool.Output,
					Error:  e.Tool.Err,
					Status: status,
					Step:   int(step),
				}},
			})

		case event.UsageKind:
			if e.Usage != nil {
				st.tkIn.Store(int64(e.Usage.PromptTokens))
				st.tkOut.Store(int64(e.Usage.CompletionTokens))
				st.tkCache.Store(int64(e.Usage.CacheHitTokens))
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
				Steps:    step,
				State:    "idle",
			})
		}
	})
}
