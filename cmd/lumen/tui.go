// tui.go — minimal clean terminal UI. Reasonix-quality prompt + streaming.
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

type tuiRuntime struct {
	ctrl   *control.Controller
	tkIn   atomic.Int64
	tkOut  atomic.Int64
	calls  atomic.Int64
	cMicro atomic.Int64 // cost * 1e6
}

func (t *tuiRuntime) cost() float64 { return float64(t.cMicro.Load()) / 1_000_000 }

// ── main entry ────────────────────────────────────────────

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
		return runLineMode(ctrl)
	}
	defer term.Restore(fd, old)

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")
	rt.banner()

	var input []byte
	for {
		// Prompt
		fmt.Printf("\n\033[1;36m▸\033[0m ")
		fmt.Print(string(input))
		fmt.Print("\033[?25h")

		buf := make([]byte, 1)
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 { break }
		ch := buf[0]

		switch ch {
		case 3:
			fmt.Print("\r\033[K\n")
			return nil
		case 13:
			text := strings.TrimSpace(string(input))
			input = input[:0]
			fmt.Print("\033[?25l\r\033[K")
			if text == "" { continue }
			if text == "/exit" || text == "/quit" {
				fmt.Print("\033[?25h\n")
				return nil
			}

			rt.handleLine(text)
		case 127, 8:
			if len(input) > 0 { input = input[:len(input)-1] }
		default:
			if ch >= 32 { input = append(input, ch) }
		}
	}
	return nil
}

// ── line dispatch ─────────────────────────────────────────

func (rt *tuiRuntime) handleLine(line string) {
	switch {
	case line == "/help":
		rt.help()
	case line == "/stats":
		rt.stats()
	case strings.HasPrefix(line, "/mode"):
		parts := strings.Fields(line)
		if len(parts) == 1 {
			rt.modeHelp()
		} else {
			m := permission.ParseMode(parts[1])
			rt.ctrl.SetPermissionMode(m)
			rt.banner()
		}
	default:
		// Echo user
		fmt.Printf("\033[36m▸\033[0m %s  \033[90m%s\033[0m\n", line, time.Now().Format("15:04"))
		ctx := context.Background()
		if err := rt.ctrl.Run(ctx, line); err != nil {
			fmt.Printf("\033[31m  error: %v\033[0m\n", err)
		}
		flushBuffer()
		fmt.Print("\n")
		rt.docFooter()
	}
}

// ── sink ──────────────────────────────────────────────────

func (rt *tuiRuntime) sink() event.Sink {
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Text:
			renderText(e.Text)
		case event.ToolDispatch:
			rt.calls.Add(1)
			fmt.Printf("\n\033[90m  · %s\033[0m  ", e.Tool.Name)
		case event.ToolResult:
			if e.Tool.Err != "" {
				fmt.Printf("\033[31m ✕\033[0m  ")
			} else {
				fmt.Print("\033[32m ✓\033[0m  ")
			}
		case event.UsageKind:
			if e.Usage != nil {
				rt.tkIn.Add(int64(e.Usage.PromptTokens))
				rt.tkOut.Add(int64(e.Usage.CompletionTokens))
				rt.cMicro.Add(int64(float64(e.Usage.PromptTokens)*0.14*1000 + float64(e.Usage.CompletionTokens)*0.28*1000))
			}
		}
	})
}

// ── footer ────────────────────────────────────────────────

func (rt *tuiRuntime) docFooter() {
	ti, to := rt.tkIn.Load(), rt.tkOut.Load()
	if ti+to == 0 { return }
	fmt.Printf("\033[90m  ── %d:%d tokens · $%.4f\033[0m\n", ti/1000, to/1000, rt.cost())
}

// ── banner / help / stats ─────────────────────────────────

func (rt *tuiRuntime) banner() {
	fmt.Print("\033[2J\033[H")
	fmt.Printf("\033[36;1m  lumen\033[0m · %s/%s \033[36m[%s]\033[0m\n\n",
		rt.ctrl.ProviderName(), rt.ctrl.ModelName(), rt.ctrl.PermissionMode())
}

func (rt *tuiRuntime) help() {
	fmt.Print("\n")
	fmt.Println("  /exit     quit")
	fmt.Println("  /stats    token usage")
	fmt.Println("  /mode     permission: bypass | plan | default | accept-edits")
	fmt.Println("  /help     this message")
	fmt.Print("\n")
}

func (rt *tuiRuntime) modeHelp() {
	fmt.Print("\n")
	fmt.Println("  bypass       full access — all tools allowed")
	fmt.Println("  plan         read-only — writes blocked")
	fmt.Println("  default      safe tools auto, writes confirm")
	fmt.Println("  accept-edits allow edits, block dangerous")
	fmt.Print("\n")
}

func (rt *tuiRuntime) stats() {
	fmt.Print("\n")
	fmt.Printf("  tokens  %d → %d\n", rt.tkIn.Load(), rt.tkOut.Load())
	fmt.Printf("  cost    $%.6f\n", rt.cost())
	fmt.Printf("  tools   %d\n", rt.calls.Load())
	fmt.Printf("  model   %s/%s\n", rt.ctrl.ProviderName(), rt.ctrl.ModelName())
	fmt.Print("\n")
}

// ── line-mode fallback ────────────────────────────────────

func runLineMode(ctrl *control.Controller) error {
	if err := ctrl.Configure(chatSink(), nil, ""); err != nil {
		return err
	}
	fmt.Printf("\033[36;1m  lumen\033[0m · %s/%s\n\n", ctrl.ProviderName(), ctrl.ModelName())
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\033[36m▸\033[0m ")
		if !sc.Scan() { break }
		text := strings.TrimSpace(sc.Text())
		if text == "" { continue }
		if text == "/exit" || text == "/quit" { break }
		if text == "/help" { fmt.Println("  /exit  /help  /stats  /mode"); continue }
		if text == "/stats" { fmt.Println("  stats: run `lumen chat` for live counters"); continue }
		if strings.HasPrefix(text, "/mode") {
			parts := strings.Fields(text)
			if len(parts) > 1 {
				ctrl.SetPermissionMode(permission.ParseMode(parts[1]))
				fmt.Printf("  switched to %s\n", parts[1])
			}
			continue
		}
		fmt.Printf("\n\033[36m▸\033[0m %s  \033[90m%s\033[0m\n", text, time.Now().Format("15:04"))
		ctrl.Run(context.Background(), text)
		flushBuffer()
		fmt.Print("\n")
	}
	return nil
}
