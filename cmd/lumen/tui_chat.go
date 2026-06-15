// Package main — Full-featured TUI: header, status bar, tool visualization,  
// live token/cost tracking, and agent output intercept for clean rendering.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"

	"lumen/internal/control"
	"lumen/internal/event"
)

// ── TUI Runtime State ──────────────────────────────────

type tuiRuntime struct {
	mu       sync.Mutex
	ctrl     *control.Controller
	width    int
	height   int

	// Live counters — int64 as fixed-point microdollars
	tokensIn   atomic.Int64
	tokensOut  atomic.Int64
	totalCostMicros atomic.Int64 // USD * 1e6 for atomic storage
	subAgents  atomic.Int64
	toolCalls  atomic.Int64
	toolName   atomic.Value // string — current tool name

	// Chat history for scrollback
	history   []string
	maxHist   int
}

func (t *tuiRuntime) addCost(delta float64) {
	micros := int64(delta * 1_000_000)
	t.totalCostMicros.Add(micros)
}

func (t *tuiRuntime) cost() float64 {
	return float64(t.totalCostMicros.Load()) / 1_000_000
}

func newTUIRuntime(ctrl *control.Controller) *tuiRuntime {
	t := &tuiRuntime{ctrl: ctrl, maxHist: 200}
	w, h, _ := term.GetSize(int(os.Stdin.Fd()))
	if w < 40 { w = 80 }
	if h < 10 { h = 24 }
	t.width = w
	t.height = h
	return t
}

// tuiSink intercepts agent events and renders them via the TUI.
func (t *tuiRuntime) sink() event.Sink {
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Text:
			fmt.Print(e.Text)
			t.appendHistory(e.Text)
		case event.ToolDispatch:
			if e.Tool.Name != "" {
				t.toolCalls.Add(1)
				t.toolName.Store(e.Tool.Name)
				fmt.Printf("\033[90m🔧 %s\033[0m\n", e.Tool.Name)
			}
		case event.ToolResult:
			t.toolName.Store("")
			if e.Tool.Err != "" {
				fmt.Printf("\033[31m  ✗ %s\033[0m\n", e.Tool.Err)
			}
		case event.UsageKind:
			if e.Usage != nil {
				t.tokensIn.Add(int64(e.Usage.PromptTokens))
				t.tokensOut.Add(int64(e.Usage.CompletionTokens))
				t.addCost(costEstimate(e.Usage.PromptTokens, e.Usage.CompletionTokens))
				fmt.Printf("\r\033[K") // clear current line
			}
		case event.Reasoning:
			// Skip verbose reasoning in chat mode
		}
	})
}

func (t *tuiRuntime) appendHistory(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.history = append(t.history, text)
	if len(t.history) > t.maxHist {
		t.history = t.history[1:]
	}
}

func costEstimate(in, out int) float64 {
	return float64(in)*0.15/1_000_000 + float64(out)*0.60/1_000_000
}

// ── Full TUI Chat ──────────────────────────────────────

func runTUIChat(ctrl *control.Controller) error {
	rt := newTUIRuntime(ctrl)

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return runLineChat(ctrl)
	}
	defer term.Restore(fd, oldState)

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	// Re-configure with TUI sink
	if err := ctrl.Configure(rt.sink(), nil, ""); err != nil {
		term.Restore(fd, oldState)
		fmt.Printf("config: %v\n", err)
		return err
	}

	// Clear and draw initial screen
	fmt.Print("\033[2J\033[H")
	rt.drawWelcome()
	rt.drawStatusBar()

	var input []byte

	// Resize goroutine
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			w, h, err := term.GetSize(fd)
			if err == nil {
				rt.mu.Lock()
				rt.width = w
				rt.height = h
				rt.mu.Unlock()
			}
		}
	}()

	for {
		// Prompt line
		rt.drawPrompt(string(input))
		fmt.Print("\033[?25h")

		buf := make([]byte, 1)
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 { break }
		ch := buf[0]

		switch ch {
		case 3: // Ctrl+C
			fmt.Print("\r\033[K\n\033[?25hGoodbye ✨\n")
			return nil

		case 13: // Enter
			text := string(input)
			fmt.Print("\r\033[K")
			input = input[:0]

			if text == "" { continue }
			if text == "/exit" || text == "/quit" {
				fmt.Print("\n\033[?25hGoodbye ✨\n")
				return nil
			}
			if text == "/help" {
				rt.drawHelp()
				continue
			}
			if text == "/stats" {
				rt.drawStats()
				continue
			}

			// Print user message
			fmt.Printf("\033[36m\033[1m🧑 You\033[0m %s\n", time.Now().Format("15:04"))
			fmt.Printf("  %s\n\n", text)

			// Run agent with status update
			rt.drawStatusLine("thinking")
			ctx := context.Background()
			if err := ctrl.Run(ctx, text); err != nil {
				fmt.Printf("\033[31m  Error: %v\033[0m\n", err)
			}
			fmt.Println()
			rt.drawStatusLine("ready")
			rt.drawStatusBar()

		case 127, 8: // Backspace
			if len(input) > 0 {
				input = input[:len(input)-1]
			}

		default:
			if ch >= 32 && ch <= 126 {
				input = append(input, ch)
			}
		}

		if ch != 13 {
			fmt.Print("\r\033[K")
		}
	}

	return nil
}

// ── Drawing ─────────────────────────────────────────────

func (t *tuiRuntime) drawWelcome() {
	w := t.width
	fmt.Print("\033[H")
	fmt.Print("\033[44m\033[37m")
	fmt.Printf(" %-*s \033[0m\n", w-1, "🪄 LUMEN — Agent Operating System")
	fmt.Print("\033[100m\033[37m")
	fmt.Printf(" %-*s \033[0m\n", w-1, fmt.Sprintf("%s / %s  │  %d tools loaded  │  /help /stats /exit",
		t.ctrl.ProviderName(), t.ctrl.ModelName(), 91))
}

func (t *tuiRuntime) drawPrompt(current string) {
	fmt.Print("\033[1m\033[36m▸ \033[0m")
	fmt.Print(current)
}

func (t *tuiRuntime) drawStatusLine(status string) {
	w := t.width
	icon := "●"
	switch status {
	case "thinking": icon = "◉"
	case "ready": icon = "●"
	case "error": icon = "✕"
	}
	line := fmt.Sprintf("%s %s", icon, status)
	fmt.Printf("\r\033[K\033[90m%s\033[0m", line)
	_ = w
}

func (t *tuiRuntime) drawStatusBar() {
	w := t.width
	if w < 40 { w = 80 }

	ti := t.tokensIn.Load()
	to := t.tokensOut.Load()
	cost := t.cost()
	subs := t.subAgents.Load()
	tools := t.toolCalls.Load()

	left := fmt.Sprintf(" ● ready │ %s/%s ", t.ctrl.ProviderName(), t.ctrl.ModelName())
	right := fmt.Sprintf(" %d:%d tk │ $%.4f │ 🧵 %d │ 🔧 %d │ %s ",
		ti/1000, to/1000, cost, subs, tools, time.Now().Format("15:04:05"))

	pad := w - len(left) - len(right)
	if pad < 1 { pad = 1 }

	fmt.Print("\r")
	fmt.Print("\033[44m\033[37m")
	fmt.Print(left)
	fmt.Print(strings.Repeat(" ", pad))
	fmt.Print(right)
	fmt.Print("\033[0m\n")
}

func (t *tuiRuntime) drawHelp() {
	fmt.Println()
	fmt.Println("  \033[1mCommands:\033[0m")
	fmt.Println("  \033[36m/exit\033[0m    Quit")
	fmt.Println("  \033[36m/help\033[0m    Show this help")
	fmt.Println("  \033[36m/stats\033[0m   Show live stats (tokens, cost, tools)")
	fmt.Println()
	fmt.Println("  \033[1mType anything\033[0m to chat with the agent.")
	fmt.Println("  The agent can call 91 built-in tools: file ops, GitHub,")
	fmt.Println("  graph algorithms, security operations, LLM queries, and more.")
}

func (t *tuiRuntime) drawStats() {
	ti := t.tokensIn.Load()
	to := t.tokensOut.Load()
	cost := t.cost()
	tools := t.toolCalls.Load()
	subs := t.subAgents.Load()
	hist := len(t.history)

	fmt.Println()
	fmt.Println("  \033[1m📊 Live Stats\033[0m")
	fmt.Printf("  Tokens:       %d in / %d out\n", ti, to)
	fmt.Printf("  Cost:         $%.6f\n", cost)
	fmt.Printf("  Tool calls:   %d\n", tools)
	fmt.Printf("  Sub-agents:   %d\n", subs)
	fmt.Printf("  History:      %d lines\n", hist)
	fmt.Println()
}

// ── Line-mode fallback ──────────────────────────────────

func runLineChat(ctrl *control.Controller) error {
	w, _, _ := term.GetSize(int(os.Stdin.Fd()))
	if w < 40 { w = 80 }

	fmt.Print("\033[2J\033[H")
	fmt.Print("\033[44m\033[37m")
	fmt.Printf(" %-*s \033[0m\n", w-1, "🪄 LUMEN — Agent Operating System")
	fmt.Print("\033[100m\033[37m")
	fmt.Printf(" %-*s \033[0m\n\n", w-1, fmt.Sprintf("%s / %s  │  91 tools  │  /exit /help /stats", ctrl.ProviderName(), ctrl.ModelName()))

	// Reconfigure with chat sink
	rt := newTUIRuntime(ctrl)
	ctrl.Configure(rt.sink(), nil, "")

	var input string
	for {
		fmt.Print("\033[1m\033[36m▸ \033[0m")
		n, _ := fmt.Scanln(&input)
		if n == 0 { input = ""; continue }
		input = strings.TrimSpace(input)
		if input == "" { continue }
		if input == "/exit" || input == "/quit" { break }
		if input == "/help" { fmt.Println("  /exit  /help  /stats  — or type to chat."); continue }
		if input == "/stats" {
			fmt.Printf("  %d in / %d out tokens  |  $%.6f  |  %d tool calls\n\n",
				rt.tokensIn.Load(), rt.tokensOut.Load(), rt.cost(), rt.toolCalls.Load())
			continue
		}

		fmt.Printf("\n\033[36m🧑 You\033[0m %s\n  %s\n\n", time.Now().Format("15:04"), input)
		ctx := context.Background()
		ctrl.Run(ctx, input)
		fmt.Println()
	}
	fmt.Println("\nGoodbye ✨")
	return nil
}
