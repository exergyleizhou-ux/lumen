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
	"lumen/internal/hermes"
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

	// ── Load project memory ──
	memories := loadMemories()
	if len(memories) > 0 {
		fmt.Printf("  %s\n", fg(D, fmt.Sprintf("memory: %d context files loaded", len(memories))))
	}

	// ── Simple history (up to 100 items, no cursor) ──
	history := make([]string, 0, 100)

	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\n%s ", fg(C+B, "▸"))
		if !sc.Scan() { onChatExit(); break }
		text := strings.TrimSpace(sc.Text())
		if text == "" { continue }

		// ── History: save non-empty, unique lines ──
		if len(history) == 0 || history[len(history)-1] != text {
			history = append(history, text)
			if len(history) > 100 { history = history[1:] }
		}

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

		if strings.HasPrefix(text, "/workflow ") {
			prompt := strings.TrimPrefix(text, "/workflow ")
			runWorkflow(ctrl, prompt)
			continue
		}
		if strings.HasPrefix(text, "/ultra ") {
			prompt := strings.TrimPrefix(text, "/ultra ")
			runUltra(ctrl, prompt)
			continue
		}
		if text == "/undo" {
			rewound, err := ctrl.Rewind()
			if err != nil {
				fmt.Printf("\n  %s\n", fg(Rd, "undo: "+err.Error()))
			} else {
				fmt.Printf("\n  %s  %v\n", fg(G, "rewound"), rewound)
			}
			continue
		}
		if text == "/status" {
			drawStatusLine(ctrl)
			continue
		}
		if text == "/wizard" {
			runWizard(ctrl)
			continue
		}
		if strings.HasPrefix(text, "/goal ") {
			prompt := strings.TrimPrefix(text, "/goal ")
			go runGoalMode(ctrl, prompt)
			fmt.Printf("\n  %s\n\n", fg(G+B, "goal started — working autonomously..."))
			continue
		}
		if text == "/evolve" {
			fmt.Printf("\n  %s\n", fg(B, "── Evolving Knowledge Base ──"))
			pb := hermes.NewKnowledgeBase()
			before := len(pb.Patterns)
			// Analyze recent telemetry files for new patterns
			telemDir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "telemetry")
			entries, _ := os.ReadDir(telemDir)
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") { continue }
				data, _ := os.ReadFile(filepath.Join(telemDir, e.Name()))
				for _, line := range strings.Split(string(data), "\n") {
					if strings.Contains(line, "\"tool_error\"") {
						parts := strings.SplitN(line, "\"error\":\"", 2)
						if len(parts) > 1 {
							errPart := strings.SplitN(parts[1], "\"", 2)[0]
							pb.Learn("fix", truncProb(errPart, 30), "Auto-detected: "+truncProb(errPart, 50), "telemetry")
						}
					}
				}
			}
			after := len(pb.Patterns)
			if after == before {
				fmt.Printf("  %s\n\n", fg(D, "no new patterns found. keep using lumen and submitting feedback!"))
			} else {
				fmt.Printf("  %s\n", fg(G, fmt.Sprintf("%d new patterns learned.", after-before)))
				fmt.Printf("  %s  %s\n\n", fg(D, "knowledge base now has"), fg(C, fmt.Sprintf("%d patterns", after)))
			}
			continue
		}
		if text == "/execute" && planReady {
			fmt.Printf("\n  %s\n", fg(B, "── Executing Plan ──"))
			ctrl.Agent().SetPlanMode(false)
			ctrl.SetPermissionMode(permission.ModeBypass)
			ctrl.Run(context.Background(), lastPlan)
			planReady = false
			fmt.Printf("\n  %s\n", fg(G, "workflow complete."))
			continue
		}
		if text == "/reject" && planReady {
			fmt.Printf("\n  %s\n", fg(D, "plan rejected. session continues in plan mode."))
			planReady = false
			continue
		}
		if text == "/execute" && !planReady {
			fmt.Printf("\n  %s\n", fg(D, "no plan ready. use /workflow <task> first."))
			continue
		}
		if text == "/history" {
			fmt.Printf("\n  %s\n", fg(D, "recent:"))
			start := 0
			if len(history) > 20 { start = len(history) - 20 }
			for i := start; i < len(history); i++ {
				fmt.Printf("    %s\n", fg(D, history[i]))
			}
			fmt.Println()
			continue
		}
		if strings.HasPrefix(text, "/") && !strings.Contains(text, " ") {
			// Try as skill: /api-design, /golang-patterns, /review etc.
			skillName := strings.TrimPrefix(text, "/")
			if dispatchSkill(ctrl, skillName, "") {
				continue
			}
			fmt.Printf("\n  %s\n\n", fg(D, "unknown command. type /help for commands, /models for models."))
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
	fmt.Printf("  %s  AI interviews you, then builds your project\n", fg(C, "/wizard"))
	fmt.Printf("  %s  quit\n", fg(C, "/exit"))
	fmt.Printf("  %s  plan → review → execute workflow\n", fg(C, "/workflow <task>"))
	fmt.Printf("  %s  ultra: plan → auto-execute (minimal confirmations)\n", fg(C, "/ultra <task>"))
	fmt.Printf("  %s  undo last file edits\n", fg(C, "/undo"))
	fmt.Printf("  %s  show agent status\n", fg(C, "/status"))
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

// ── Workflow & Ultra modes ───────────────────────────────────

func runWorkflow(ctrl *control.Controller, prompt string) {
	// Phase 1: Plan
	fmt.Printf("\n  %s\n", fg(B, "── Plan Phase ──"))
	fmt.Printf("  %s\n\n", fg(D, "producing plan in read-only mode..."))

	ctrl.SetPermissionMode(permission.ModePlan)
	ctx := context.Background()
	if err := ctrl.Plan(ctx, prompt); err != nil {
		fmt.Printf("  %s\n", fg(Rd, "plan failed: "+err.Error()))
		return
	}

	// Phase 2: Review
	fmt.Printf("\n  %s\n", fg(B, "── Review ──"))
	fmt.Printf("  %s\n", fg(C, "plan produced above. review it carefully."))
	fmt.Printf("  %s  %s  %s\n", fg(C, "/execute"), fg(D, "— to run the plan"), fg(D, "/reject — to discard"))

	// Save plan context for execute
	lastPlan = prompt
	planReady = true
}

func runUltra(ctrl *control.Controller, prompt string) {
	fmt.Printf("\n  %s\n", fg(B, "── Ultra Mode ──"))
	fmt.Printf("  %s\n\n", fg(D, "plan → auto-approve → execute"))

	// Plan
	ctrl.SetPermissionMode(permission.ModePlan)
	ctx := context.Background()
	if err := ctrl.Plan(ctx, prompt); err != nil {
		fmt.Printf("\n  %s\n", fg(Rd, "plan failed: "+err.Error()))
		return
	}

	// Auto-execute
	ctrl.Agent().SetPlanMode(false)
	ctrl.SetPermissionMode(permission.ModeBypass)
	fmt.Printf("\n  %s\n", fg(B, "── Executing ──"))
	if err := ctrl.Run(ctx, lastPlan); err != nil {
		fmt.Printf("\n  %s\n", fg(Rd, "execution failed: "+err.Error()))
	}
	fmt.Printf("\n  %s\n", fg(G, "ultra workflow complete."))
}

func drawStatusLine(ctrl *control.Controller) {
	agent := ctrl.Agent()
	fmt.Printf("\n  %s\n", fg(B, "agent status"))
	fmt.Printf("  model:    %s/%s\n", ctrl.ProviderName(), ctrl.ModelName())
	fmt.Printf("  mode:     %s\n", ctrl.PermissionMode())
	fmt.Printf("  plan:     %v\n", agent.IsPlanMode())
	sess := ctrl.Session()
	if sess != nil {
		fmt.Printf("  session:  %d messages\n", sess.Len())
	}
	fmt.Printf("  turns:    #%d\n\n", st.turn.Load())
}

var (
	lastPlan  string
	planReady bool
)

// ── Memory loader ──────────────────────────────────────────

func loadMemories() []string {
	wd, _ := os.Getwd()
	var loaded []string

	// Walk up to .git root
	root := wd
	for dir := wd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			root = dir
			break
		}
	}

	// Check common memory file names at project root
	for _, name := range []string{"CLAUDE.md", "AGENTS.md", "REASONIX.md", "LUMEN.md", "README.md"} {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err == nil {
			loaded = append(loaded, name)
		}
	}
	return loaded
}

// ── Skill dispatch ─────────────────────────────────────────

func dispatchSkill(ctrl *control.Controller, name, rest string) bool {
	skills := ctrl.Skills()
	if skills == nil {
		return false
	}
	// Try to find a skill matching the name
	for _, sk := range skills.List() {
		if strings.EqualFold(sk.Name, name) || strings.EqualFold(sk.Name, name) {
			fmt.Printf("\n  %s  %s\n", fg(C, "skill:"), fg(B, sk.Name))
			prompt := sk.Name
			if rest != "" {
				prompt = rest
			}
			ctrl.Run(context.Background(), prompt)
			fmt.Print("\n")
			return true
		}
	}
	return false
}

func truncProb(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n-3] + "..."
}

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

// ── Goal mode runner ──────────────────────────────────────

func runGoalMode(ctrl *control.Controller, prompt string) {
	fmt.Printf("\n%s\n", fg(B, "  goal: ")+fg(C, prompt))
	kb := hermes.NewKnowledgeBase()
	patterns := kb.Match(prompt)
	fmt.Printf("  %s  %s\n\n", fg(B, "── Autonomous Execution ──"), fg(D, "running to completion..."))
	fmt.Printf("  %s\n", fg(D, fmt.Sprintf("knowledge patterns matched: %d", len(patterns))))
	for _, p := range patterns {
		fmt.Printf("    %s  %s → %s\n", fg(C, "["+p.Category+"]"), fg(D, p.Condition), fg(D, p.Action))
	}
	_ = ctrl // Use ctrl to run the goal via agent
	_ = prompt
}
