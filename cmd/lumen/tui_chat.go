// Package main — TUI chat: a proper interactive terminal UI with
// header bar, input prompt, and agent output. Uses raw mode for
// smooth key-by-key typing experience.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
	"strings"

	"lumen/internal/control"
)

// ── TUI Chat ─────────────────────────────────────────────

func runTUIChat(ctrl *control.Controller) error {
	fd := int(os.Stdin.Fd())

	// Try raw mode
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return runLineChat(ctrl)
	}
	defer term.Restore(fd, oldState)

	// Hide cursor during setup
	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	// Clear screen
	fmt.Print("\033[2J\033[H")

	// Initial header
	drawHeader(ctrl)

	var input []byte
	input = make([]byte, 0, 4096)

	for {
		// Show cursor for input
		fmt.Print("\033[?25h")

		// Prompt
		fmt.Print("\033[1m\033[36m> \033[0m")
		fmt.Print(string(input))
		fmt.Print("\033[5m \033[0m") // blinking cursor

		// Read one byte
		buf := make([]byte, 1)
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}
		ch := buf[0]

		switch ch {
		case 3: // Ctrl+C
			fmt.Print("\r\033[K")
			fmt.Print("\033[?25h")
			fmt.Print("\nGoodbye ✨\n")
			return nil

		case 13: // Enter
			text := string(input)
			// Clear the input line
			fmt.Print("\r\033[K")

			input = input[:0]

			if text == "" {
				continue
			}
			if text == "/exit" || text == "/quit" {
				fmt.Print("\033[?25h")
				fmt.Print("\nGoodbye ✨\n")
				return nil
			}
			if text == "/help" {
				fmt.Print("\r\033[K")
				fmt.Println("\033[90m  /exit   Quit")
				fmt.Println("  /help   Show this help")
				fmt.Println("  type anything to chat\033[0m")
				continue
			}

			// Echo user message
			fmt.Printf("\n\033[36m\033[1m🧑 You\033[0m \033[90m%s\033[0m\n", time.Now().Format("15:04"))
			fmt.Printf("  %s\n\n", text)

			// Run agent — output goes to stdout naturally
			ctx := context.Background()
			if err := ctrl.Run(ctx, text); err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
			}
			fmt.Println()

			// Redraw header
			drawHeader(ctrl)

		case 127, 8: // Backspace
			if len(input) > 0 {
				input = input[:len(input)-1]
				fmt.Print("\r\033[K")
			}

		default:
			if ch >= 32 && ch <= 126 {
				input = append(input, ch)
			}
		}

		if ch != 13 {
			// Redraw input line for non-enter keys
			fmt.Print("\r\033[K")
			fmt.Print("\033[1m\033[36m> \033[0m")
			fmt.Print(string(input))
		}
	}
	return nil
}

func drawHeader(ctrl *control.Controller) {
	w, _, _ := term.GetSize(int(os.Stdin.Fd()))
	if w < 40 { w = 80 }

	header := fmt.Sprintf(" 🪄 Lumen — %s / %s ", ctrl.ProviderName(), ctrl.ModelName())
	now := time.Now().Format("15:04:05")
	right := fmt.Sprintf(" %s │ /exit=quit ", now)

	pad := w - len(header) - len(right)
	if pad < 1 { pad = 1 }

	fmt.Print("\033[H") // move to top
	fmt.Print("\033[44m\033[37m")
	fmt.Print(header)
	fmt.Print(strings.Repeat(" ", pad))
	fmt.Print(right)
	fmt.Print("\033[0m\n")
	fmt.Print("\033[90m" + strings.Repeat("─", w) + "\033[0m\n")
}



// ── Fallback: line-based chat when raw mode fails ───────

func runLineChat(ctrl *control.Controller) error {
	fmt.Printf("\n🪄 Lumen Chat — %s/%s\n", ctrl.ProviderName(), ctrl.ModelName())
	fmt.Println("/exit to quit, type to chat\n")

	var input string
	for {
		fmt.Print("\033[1m\033[36m> \033[0m")
		n, _ := fmt.Scanln(&input)
		if n == 0 { continue }
		input = strings.TrimSpace(input)
		if input == "" { continue }
		if input == "/exit" || input == "/quit" { break }

		fmt.Println()
		ctx := context.Background()
		ctrl.Run(ctx, input)
		fmt.Println()
	}
	fmt.Println("\nGoodbye ✨")
	return nil
}
