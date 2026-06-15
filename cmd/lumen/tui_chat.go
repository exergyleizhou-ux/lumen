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

const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cBlue   = "\033[34m"
	cMagenta = "\033[35m"
	cCyan   = "\033[36m"
	cWhite  = "\033[37m"
	cGray   = "\033[90m"
	cBgBlue  = "\033[44m"
	cBgCyan  = "\033[46m"
)

type tuiRuntime struct {
	ctrl      *control.Controller
	w, h      int
	tokensIn  atomic.Int64
	tokensOut atomic.Int64
	costMicros atomic.Int64
	toolCalls atomic.Int64
	curTool   atomic.Value // string
}

func (t *tuiRuntime) cost() float64 { return float64(t.costMicros.Load()) / 1e6 }
func (t *tuiRuntime) addCost(v float64) { t.costMicros.Add(int64(v * 1e6)) }

// ── Main entry ────────────────────────────────────────────

func runTUIChat(ctrl *control.Controller, modeOverride string) error {
	rt := &tuiRuntime{ctrl: ctrl}
	if err := ctrl.Configure(rt.sink(), nil, ""); err != nil {
		return err
	}
	if modeOverride != "" {
		ctrl.SetPermissionMode(permission.ParseMode(modeOverride))
	}

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return runLineChat(ctrl)
	}
	defer term.Restore(fd, old)

	rt.w, rt.h, _ = term.GetSize(fd)
	if rt.w < 40 { rt.w = 80 }
	if rt.h < 12 { rt.h = 24 }

	go func() {
		for range time.NewTicker(time.Second).C {
			w, h, _ := term.GetSize(fd)
			rt.w, rt.h = w, h
		}
	}()

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	rt.drawBanner()

	var input []byte
	for {
		rt.drawPrompt(string(input))
		fmt.Print("\033[?25h")

		buf := make([]byte, 1)
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 { break }
		ch := buf[0]

		switch ch {
		case 3: // Ctrl+C
			fmt.Print("\r\033[K\n\nGoodbye ✨\n")
			return nil

		case 13: // Enter
			text := strings.TrimSpace(string(input))
			input = input[:0]
			fmt.Print("\r\033[K")

			if text == "" { continue }
			switch {
			case text == "/exit" || text == "/quit":
				fmt.Print("\n\nGoodbye ✨\n")
				return nil
			case text == "/help":
				rt.drawHelp()
				continue
			case text == "/stats":
				rt.drawStats()
				continue
			case strings.HasPrefix(text, "/mode"):
				parts := strings.Fields(text)
				if len(parts) == 1 {
					rt.drawModeHelp()
				} else {
					m := permission.ParseMode(parts[1])
					rt.ctrl.SetPermissionMode(m)
					fmt.Printf("%s◆ mode: %s%s  ", cCyan, m, cReset)
				}
				continue
			}

			// User message
			fmt.Printf("%s⏣%s %s  ", cCyan+cBold, cReset, cDim+time.Now().Format("15:04")+cReset)
			fmt.Print(text + "\n\n")

			// Run
			ctx := context.Background()
			if err := ctrl.Run(ctx, text); err != nil {
				fmt.Printf("%s✕ %v%s\n", cRed, err, cReset)
			}
			fmt.Print("\n")

		case 127, 8: // Backspace
			if len(input) > 0 { input = input[:len(input)-1] }

		default:
			if ch >= 32 { input = append(input, ch) }
		}
	}
	return nil
}

// ── Sink — intercepts agent events ────────────────────────

func (t *tuiRuntime) sink() event.Sink {
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Text:
			renderClean(e.Text)
		case event.ToolDispatch:
			t.toolCalls.Add(1)
			t.curTool.Store(e.Tool.Name)
			fmt.Printf("%s  ⚡ %s%s  ", cCyan, cReset, e.Tool.Name)
		case event.ToolResult:
			t.curTool.Store("")
			if e.Tool.Err != "" {
				fmt.Printf("%s✕ %s%s  ", cRed, e.Tool.Err, cReset)
			} else {
				fmt.Print(cGreen + "✓" + cReset + "  ")
			}
		case event.UsageKind:
			if e.Usage != nil {
				t.tokensIn.Add(int64(e.Usage.PromptTokens))
				t.tokensOut.Add(int64(e.Usage.CompletionTokens))
				t.addCost(float64(e.Usage.PromptTokens)*0.14/1e6 + float64(e.Usage.CompletionTokens)*0.28/1e6)
			}
		}
	})
}

// ── Drawing ───────────────────────────────────────────────

func (t *tuiRuntime) drawBanner() {
	w := t.w
	if w < 40 { w = 80 }
	fmt.Print("\033[2J\033[H")
	fmt.Print(cBgCyan + cWhite + cBold)
	fmt.Printf("  LUMEN  ")
	fmt.Print(cReset + cBgCyan + cWhite)
	fmt.Printf(" %s/%s ", t.ctrl.ProviderName(), t.ctrl.ModelName())
	mode := t.ctrl.PermissionMode()
	fmt.Printf("[%s] ", mode)
	pad := w - 20 - len(t.ctrl.ProviderName()) - len(t.ctrl.ModelName())
	if pad < 1 { pad = 1 }
	fmt.Print(strings.Repeat(" ", pad) + cReset + "\n")
	fmt.Print(cDim + "  ⏣ chat   │   ⚡ tools   │   ✕ errors   │   /help /stats /mode /exit" + cReset + "\n\n")
}

func (t *tuiRuntime) drawPrompt(current string) {
	mode := t.ctrl.PermissionMode()
	modeStr := string(mode)
	if modeStr == "bypass" { modeStr = "◆" }
	fmt.Printf("%s%s%s%s  ", cCyan+cBold, modeStr, cReset, cCyan)
	fmt.Print(current)
}

func (t *tuiRuntime) drawHelp() {
	fmt.Print("\n")
	fmt.Printf("  %sCOMMANDS%s\n\n", cBold, cReset)
	fmt.Printf("  %s/exit%s     Quit\n", cCyan, cReset)
	fmt.Printf("  %s/mode%s     Show permission modes\n", cCyan, cReset)
	fmt.Printf("         %s/mode bypass%s — allow all, no questions\n", cDim, cReset)
	fmt.Printf("         %s/mode plan%s — read-only, writes blocked\n", cDim, cReset)
	fmt.Printf("         %s/mode default%s — safe tools auto, writes confirm\n", cDim, cReset)
	fmt.Printf("         %s/mode accept-edits%s — allow edits, block dangerous\n", cDim, cReset)
	fmt.Printf("  %s/stats%s    Token/cost stats\n", cCyan, cReset)
	fmt.Print("\n")
}

func (t *tuiRuntime) drawModeHelp() {
	m := t.ctrl.PermissionMode()
	fmt.Print("\n")
	fmt.Printf("  %sMODE: %s%s\n\n", cBold, m, cReset)
	fmt.Printf("  %sbypass%s       full access — like Claude Code\n", cCyan+cBold, cReset)
	fmt.Printf("  %splan%s         read-only — like Claude Code /plan\n", cCyan, cReset)
	fmt.Printf("  %sdefault%s      safe tools auto, writes confirm\n", cCyan, cReset)
	fmt.Printf("  %saccept-edits%s  allow edits, block dangerous\n\n", cCyan, cReset)
}

func (t *tuiRuntime) drawStats() {
	fmt.Print("\n")
	fmt.Printf("  %sSTATS%s\n", cBold, cReset)
	fmt.Printf("  tokens:  %d → %d\n", t.tokensIn.Load(), t.tokensOut.Load())
	fmt.Printf("  cost:    $%.6f\n", t.cost())
	fmt.Printf("  tools:   %d\n", t.toolCalls.Load())
	fmt.Printf("  model:   %s/%s\n\n", t.ctrl.ProviderName(), t.ctrl.ModelName())
}


// ── Line-mode fallback ────────────────────────────────────

func runLineChat(ctrl *control.Controller) error {
	if err := ctrl.Configure(chatSink(), nil, ""); err != nil {
		return err
	}
	w, _, _ := term.GetSize(int(os.Stdin.Fd()))
	if w < 40 { w = 80 }

	fmt.Print("\033[2J\033[H")
	fmt.Print(cBgCyan + cWhite + cBold)
	fmt.Print("  LUMEN  ")
	fmt.Print(cReset + cBgCyan + cWhite)
	fmt.Printf(" %s/%s [%s] \n", ctrl.ProviderName(), ctrl.ModelName(), ctrl.PermissionMode())
	fmt.Print(cReset + "\n")

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s❯ %s", cCyan+cBold, cReset)
		line, err := reader.ReadString('\n')
		if err != nil { break }
		line = strings.TrimSpace(line)
		if line == "" { continue }
		if line == "/exit" || line == "/quit" { break }
		if line == "/help" { fmt.Printf("  /exit  /help  /stats  /mode\n\n"); continue }
		if strings.HasPrefix(line, "/mode ") {
			m := permission.ParseMode(strings.TrimPrefix(line, "/mode "))
			ctrl.SetPermissionMode(m)
			fmt.Printf("%s◆ switched to %s%s\n", cCyan, m, cReset)
			continue
		}
		if line == "/mode" { fmt.Printf("  /mode bypass | plan | default | accept-edits\n\n"); continue }

		fmt.Printf("\n%s⏣%s %s  %s\n\n", cCyan+cBold, cReset, cDim+time.Now().Format("15:04")+cReset, line)
		ctx := context.Background()
		ctrl.Run(ctx, line)
		fmt.Print("\n")
	}
	fmt.Printf("\nGoodbye ✨\n")
	return nil
}

