// Package main — Production TUI: Grok Build / Claude Code quality terminal UI.
// Fixed layout, Grok-style color palette, inline tool status, live metrics.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/term"

	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/permission"
)

// ── color palette ──────────────────────────────────────
const (
	cReset   = "\033[0m"
	cBold    = "\033[1m"
	cDim     = "\033[2m"
	cRed     = "\033[31m"
	cGreen   = "\033[32m"
	cYellow  = "\033[33m"
	cBlue    = "\033[34m"
	cMagenta = "\033[35m"
	cCyan    = "\033[36m"
	cWhite   = "\033[37m"
	cGray    = "\033[90m"
	cBgBlack  = "\033[40m"
	cBgGray   = "\033[100m"
	cBgBlue   = "\033[44m"
	cBgCyan   = "\033[46m"
)

// ── runtime ────────────────────────────────────────────

type tuiRuntime struct {
	ctrl    *control.Controller
	w, h    int

	tokensIn   atomic.Int64
	tokensOut  atomic.Int64
	costMicros atomic.Int64
	toolCalls  atomic.Int64

	currentTool atomic.Value // string
}

func (t *tuiRuntime) addCost(v float64) {
	t.costMicros.Add(int64(v * 1_000_000))
}
func (t *tuiRuntime) cost() float64 {
	return float64(t.costMicros.Load()) / 1_000_000
}
func costEstimate(in, out int) float64 {
	return float64(in)*0.14/1e6 + float64(out)*0.28/1e6
}

// ── main entry ─────────────────────────────────────────

func runTUIChat(ctrl *control.Controller, modeOverride string) error {
	rt := &tuiRuntime{ctrl: ctrl}
	if err := ctrl.Configure(rt.sink(), nil, ""); err != nil {
		return err
	}
	// Apply CLI mode override AFTER Configure (which resets mode from config)
	if modeOverride != "" {
		ctrl.SetPermissionMode(permission.ParseMode(modeOverride))
	}

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		// Save mode override before line fallback reconfigures
		savedMode := ctrl.PermissionMode()
		err2 := runLineChat(ctrl)
		ctrl.SetPermissionMode(savedMode)
		return err2
	}
	defer term.Restore(fd, old)

	rt.w, rt.h, _ = term.GetSize(fd)
	if rt.w < 40 { rt.w = 80 }
	if rt.h < 12 { rt.h = 24 }

	// resize watcher
	go func() {
		for range time.NewTicker(800 * time.Millisecond).C {
			w, h, err := term.GetSize(fd)
			if err == nil { rt.w, rt.h = w, h }
		}
	}()

	defer fmt.Print("\033[?25h")
	fmt.Print("\033[?25l\033[2J\033[H")

	// ── 2. Draw initial screen ──
	rt.drawWelcome()
	rt.drawStatusBar()
	fmt.Print("\n")
	rt.drawSeparator()

	var input []byte

	// ── 3. Main loop ──
	for {
		// prompt at fixed position
		fmt.Print(cCyan + cBold + "❯ " + cReset)
		fmt.Print(string(input))
		fmt.Print(" ") // cursor visible area
		fmt.Print("\033[?25h")

		buf := make([]byte, 1)
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 { break }
		ch := buf[0]

		fmt.Print("\033[?25l") // hide during processing

		switch ch {
		case 3: // Ctrl+C
			return rt.quit("Cancelled")

		case 13: // Enter
			text := strings.TrimSpace(string(input))
			input = input[:0]
			fmt.Print("\r\033[K")

			switch text {
			case "":
				continue
			case "/exit", "/quit":
				return rt.quit("Goodbye ✨")
			case "/help":
				rt.drawHelp()
				continue
			case "/stats":
				rt.drawStats()
				continue
			case "/clear":
				fmt.Print("\033[2J\033[H")
				rt.drawWelcome()
				rt.drawStatusBar()
				continue
			case "/mode":
				// Show mode options
				rt.drawModeHelp()
				continue
			default:
				// /mode <name> — switch permission mode
				if strings.HasPrefix(text, "/mode ") {
					modeStr := strings.TrimPrefix(text, "/mode ")
					newMode := permission.ParseMode(modeStr)
					rt.ctrl.SetPermissionMode(newMode)
					fmt.Printf("\n%s  ✓ Mode switched to %s%s\n", cGreen, newMode, cReset)
					rt.drawStatusBar()
					continue
				}
			}

			// echo user
			fmt.Printf("\n%s%s🧑 You%s %s\n", cBold, cCyan, cReset, cDim+time.Now().Format("15:04")+cReset)
			fmt.Printf("  %s\n\n", text)

			// run agent
			fmt.Print(cYellow + "  … thinking" + cReset)
			ctx := context.Background()
			if err := ctrl.Run(ctx, text); err != nil {
				fmt.Printf("\r\033[K%s  ✕ %v%s\n", cRed, err, cReset)
			}
			fmt.Printf("\r\033[K")
			fmt.Print("\n")
			rt.drawStatusBar()

		case 127, 8: // Backspace
			if len(input) > 0 { input = input[:len(input)-1] }

		default:
			if ch >= 32 {
				input = append(input, ch)
			}
		}

		// redraw prompt line for non-enter
		if ch != 13 {
			fmt.Print("\r\033[K")
		}
	}
	return nil
}

// ── sink ───────────────────────────────────────────────

func (t *tuiRuntime) sink() event.Sink {
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Text:
			renderClean(e.Text)
		case event.ToolDispatch:
			if e.Tool.Name != "" {
				t.toolCalls.Add(1)
				t.currentTool.Store(e.Tool.Name)
				fmt.Printf("\n%s  ⚡ %s%s %s   ", cYellow, cReset, cDim, e.Tool.Name)
				if e.Tool.Description != "" {
					fmt.Printf("%s", trunc(e.Tool.Description, 50))
				}
				fmt.Print(cReset)
			}
		case event.ToolResult:
			if e.Tool.Err != "" {
				fmt.Printf("\n%s  ✕ %s%s", cRed, e.Tool.Err, cReset)
			}
			t.currentTool.Store("")
		case event.UsageKind:
			if e.Usage != nil {
				t.tokensIn.Add(int64(e.Usage.PromptTokens))
				t.tokensOut.Add(int64(e.Usage.CompletionTokens))
				t.addCost(costEstimate(e.Usage.PromptTokens, e.Usage.CompletionTokens))
			}
		}
	})
}

// ── drawing ────────────────────────────────────────────

func (t *tuiRuntime) drawWelcome() {
	w := t.w
	brand := "LUMEN"
	tagline := "Agent Operating System"
	pad := (w - len(brand) - len(tagline) - 3) / 2
	if pad < 0 { pad = 0 }

	fmt.Printf("%s%s%s%s  %s%s%s%s\n",
		cBgCyan, cWhite, strings.Repeat(" ", pad),
		cBold+brand, cReset,
		cBgCyan, cWhite, cDim+tagline+strings.Repeat(" ", w-pad-len(brand)-len(tagline)-3)+cReset)
}

func (t *tuiRuntime) drawStatusBar() {
	w := t.w
	mode := t.ctrl.PermissionMode()
	modeIcon := "●"
	switch mode {
	case permission.ModePlan: modeIcon = "⊙"
	case permission.ModeBypass: modeIcon = "◉"
	case permission.ModeAcceptEdits: modeIcon = "◒"
	}
	left := fmt.Sprintf(" %s %s/%s [%s] ", modeIcon, t.ctrl.ProviderName(), t.ctrl.ModelName(), mode)

	ti, to := t.tokensIn.Load(), t.tokensOut.Load()
	ct := t.toolCalls.Load()
	right := fmt.Sprintf(" %d:%d tk  $%.4f  %d tools  %s ",
		ti/1000, to/1000, t.cost(), ct, time.Now().Format("15:04:05"))

	pad := w - len(left) - len(right)
	if pad < 1 { pad = 1 }

	fmt.Printf("\r%s%s%s%s%s%s",
		cBgGray, cWhite, left,
		cReset, cBgGray, cWhite)
	fmt.Print(strings.Repeat(" ", pad))
	fmt.Printf("%s%s%s", right, cReset, "\n")
}

func (t *tuiRuntime) drawSeparator() {
	fmt.Print(cDim + strings.Repeat("─", t.w) + cReset + "\n")
}

func (t *tuiRuntime) drawHelp() {
	fmt.Print("\n")
	fmt.Printf("  %sCommands%s\n", cBold, cReset)
	fmt.Printf("  %s/exit%s     Quit\n", cCyan, cReset)
	fmt.Printf("  %s/help%s     This help\n", cCyan, cReset)
	fmt.Printf("  %s/stats%s    Live statistics\n", cCyan, cReset)
	fmt.Printf("  %s/clear%s    Clear screen\n", cCyan, cReset)
	fmt.Printf("  %s/mode%s     Show / switch permission mode\n", cCyan, cReset)
	fmt.Printf("           %s/mode bypass%s — allow all tools\n", cDim, cReset)
	fmt.Printf("           %s/mode default%s — safe tools auto, writes confirm\n", cDim, cReset)
	fmt.Printf("           %s/mode plan%s — read-only, blocks all writes\n", cDim, cReset)
	fmt.Printf("           %s/mode accept-edits%s — allow edits, block dangerous\n", cDim, cReset)
	fmt.Print("\n")
	fmt.Printf("  %s91 tools%s available.\n\n", cBold, cReset)
}

func (t *tuiRuntime) drawModeHelp() {
	mode := t.ctrl.PermissionMode()
	fmt.Print("\n")
	fmt.Printf("  %sPermission Mode: %s%s\n\n", cBold, mode, cReset)
	fmt.Printf("  %sbypass%s       Allow all tools — no questions asked\n", cCyan, cReset)
	fmt.Printf("  %sdefault%s      Safe tools auto-allowed, write tools confirm (recommended)\n", cCyan, cReset)
	fmt.Printf("  %saccept-edits%s Allow non-dangerous tools, block destructive commands\n", cCyan, cReset)
	fmt.Printf("  %splan%s         Read-only — all writes blocked, for reviewing plans\n", cCyan, cReset)
	fmt.Print("\n")
	fmt.Printf("  Switch: %s/mode <name>%s\n\n", cCyan+cBold, cReset)
}

func (t *tuiRuntime) drawStats() {
	fmt.Print("\n")
	fmt.Printf("  %s📊 Live Stats%s\n\n", cBold, cReset)
	fmt.Printf("  %sTokens In%s      %d\n", cDim, cReset, t.tokensIn.Load())
	fmt.Printf("  %sTokens Out%s     %d\n", cDim, cReset, t.tokensOut.Load())
	fmt.Printf("  %sTotal Cost%s     $%.6f\n", cDim, cReset, t.cost())
	fmt.Printf("  %sTool Calls%s     %d\n", cDim, cReset, t.toolCalls.Load())
	fmt.Printf("  %sModel%s          %s/%s\n", cDim, cReset, t.ctrl.ProviderName(), t.ctrl.ModelName())
	fmt.Print("\n")
}

func (t *tuiRuntime) quit(msg string) error {
	fmt.Printf("\r\033[K\n%s%s%s\n", cGreen, msg, cReset)
	return nil
}

// ── helpers ────────────────────────────────────────────

func trunc(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n-3] + "..."
}

// ── line-mode fallback ─────────────────────────────────

func runLineChat(ctrl *control.Controller) error {
	if err := ctrl.Configure(chatSink(), nil, ""); err != nil {
		return err
	}

	w, _, _ := term.GetSize(int(os.Stdin.Fd()))
	if w < 40 { w = 80 }

	fmt.Print("\033[2J\033[H")
	fmt.Print(cBgCyan + cWhite)
	fmt.Printf(" %-*s ", w-1, "LUMEN · "+ctrl.ProviderName()+" / "+ctrl.ModelName()+" ["+string(ctrl.PermissionMode())+"]")
	fmt.Print(cReset + "\n")
	fmt.Printf("%s%*s%s\n", cDim, w-1, "/exit /help /stats /mode /clear  ·  91 tools", cReset)
	fmt.Print(cDim + strings.Repeat("─", w) + cReset + "\n\n")

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s❯ %s", cCyan+cBold, cReset)
		line, err := reader.ReadString('\n')
		if err != nil { break }
		line = strings.TrimSpace(line)
		if line == "" { continue }
		if line == "/exit" || line == "/quit" { break }
		if line == "/help" {
			fmt.Printf("  %s/exit /help /stats /clear%s  —  or type to chat.\n", cDim, cReset)
			continue
		}
		if line == "/stats" {
			fmt.Printf("  %s91 tools  ·  %s/%s  ·  type /stats for live metrics%s\n\n",
				cDim, ctrl.ProviderName(), ctrl.ModelName(), cReset)
			continue
		}
		if line == "/clear" {
			fmt.Print("\033[2J\033[H")
			fmt.Print(cBgCyan + cWhite)
			fmt.Printf(" %-*s ", w-1, "LUMEN · "+ctrl.ProviderName()+" / "+ctrl.ModelName())
			fmt.Print(cReset + "\n")
			fmt.Print(cDim + strings.Repeat("─", w) + cReset + "\n\n")
			continue
		}

		fmt.Printf("\n%s🧑 You%s %s\n  %s\n\n", cCyan+cBold, cReset, cDim+time.Now().Format("15:04")+cReset, line)
		fmt.Printf("%s  … thinking%s", cYellow, cReset)

		ctx := context.Background()
		if err := ctrl.Run(ctx, line); err != nil {
			fmt.Printf("\r\033[K%s  ✕ %v%s\n", cRed, err, cReset)
		}
		fmt.Printf("\r\033[K\n")
		fmt.Print(cDim + strings.Repeat("─", w) + cReset + "\n")
	}
	fmt.Printf("\n%sGoodbye ✨%s\n", cGreen, cReset)
	return nil
}

