// terminal.go — Claude Code / Reasonix color scheme.
// Every element has a distinct color so the eye can scan naturally.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/permission"
)

type liveStats struct {
	tkIn    atomic.Int64
	tkOut   atomic.Int64
	tkCache atomic.Int64
	tools   atomic.Int64
	costU   atomic.Int64
}

func (s *liveStats) addCost(v float64)     { s.costU.Add(int64(v * 1_000_000)) }
func (s *liveStats) cost() float64          { return float64(s.costU.Load()) / 1_000_000 }

var stats = &liveStats{}

// ── Color palette ──────────────────────────────────────────
// Mirroring Claude Code / Reasonix conventions:
//
//   User input   → bold cyan (stands out from AI text)
//   Thinking     → italic dim (whisper, barely visible)
//   Model output → default white (the content itself)
//   Tool calls   → bold yellow (draws attention)
//   Tool OK      → green check
//   Tool fail    → red cross
//   Footer       → dim gray (metadata)
//   Header       → bold white brand + dim gray model

func color(s, code string) string { return code + s + "\033[0m" }
func white(s string) string  { return color(s, "\033[97m") }
func bold(s string) string  { return color(s, "\033[1m") }
func dim(s string) string   { return color(s, "\033[2m") }
func italic(s string) string { return color(s, "\033[3m") }
func cyan(s string) string  { return color(s, "\033[36m") }
func green(s string) string { return color(s, "\033[32m") }
func red(s string) string   { return color(s, "\033[31m") }
func yellow(s string) string { return color(s, "\033[33m") }

// ── Sink: colour-coded output ──────────────────────────────

func termSink() event.Sink {
	thinking := false
	textStarted := false
	textLen := 0
	const maxOutput = 8 * 1024 // 8KB per turn

	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted:
			thinking = true
			textStarted = false
			textLen = 0

		case event.Reasoning:
			if thinking && !textStarted {
				fmt.Fprint(os.Stdout, italic(dim(stripMD(e.Text))))
			}

		case event.Text:
			if thinking && !textStarted {
				thinking = false
				textStarted = true
				fmt.Print("\n")
			}
			cleaned := stripMD(e.Text)
			if textLen >= maxOutput {
				// Already over limit — only show notice once
				return
			}
			textLen += len(cleaned)
			if textLen > maxOutput {
				fmt.Fprint(os.Stdout, cleaned[:len(cleaned)-(textLen-maxOutput)])
				fmt.Fprintf(os.Stderr, "\n\n%s", dim(fmt.Sprintf("… output truncated (%d bytes max)", maxOutput)))
				return
			}
			fmt.Fprint(os.Stdout, cleaned)

		case event.ToolDispatch:
			thinking = false
			textStarted = true
			stats.tools.Add(1)
			fmt.Printf("\n  %s", dim(yellow("⚡ "+e.Tool.Name)))

		case event.ToolResult:
			if e.Tool.Err != "" {
				fmt.Printf("  %s\n", red("✗ "+e.Tool.Err))
			} else if e.Tool.Blocked {
				fmt.Printf("  %s\n", dim("⊘ blocked"))
			} else {
				fmt.Printf("  %s\n", green("✓"))
			}

		case event.UsageKind:
			if e.Usage != nil {
				stats.tkIn.Store(int64(e.Usage.PromptTokens))
				stats.tkOut.Store(int64(e.Usage.CompletionTokens))
				stats.tkCache.Store(int64(e.Usage.CacheHitTokens))
				stats.addCost(float64(e.Usage.PromptTokens)*0.14/1e6 +
					float64(e.Usage.CompletionTokens)*0.28/1e6)
			}

		case event.FilePreview:
			fmt.Printf("\n%s\n", cyan("── Preview ──────────────────────────────"))
			fmt.Print(e.DiffText)
			fmt.Printf("%s\n", cyan("──────────────────────────────────────────"))

		case event.TurnDone:
			drawFooter()
			thinking = false
			textStarted = false
		}
	})
}

// ── Footer ─────────────────────────────────────────────────

func drawFooter() {
	ti := stats.tkIn.Load()
	to := stats.tkOut.Load()
	tc := stats.tkCache.Load()
	cost := stats.cost()
	tools := stats.tools.Load()

	cachePct := 0
	if ti > 0 {
		cachePct = int(float64(tc) / float64(ti) * 100)
	}
	fmt.Fprintf(os.Stderr, "%s  %s∶%s %s  ·  %scache %d%%%s  ·  %s$%.4f%s  ·  %s%d tools%s\n",
		dim(""), cyan(fmt.Sprint(ti/1000)), green(fmt.Sprint(to/1000)), dim("tokens"),
		dim(""), cachePct, dim(""),
		dim(""), cost, dim(""),
		dim(""), tools, dim(""))
}

// ── Chat loop ──────────────────────────────────────────────

func runChatUI(ctrl *control.Controller, modeOverride string) error {
	if err := ctrl.Configure(termSink(), nil, ""); err != nil {
		return err
	}
	if modeOverride != "" {
		ctrl.SetPermissionMode(permission.ParseMode(modeOverride))
	}

	// ── Session file for history ──
	histDir := os.ExpandEnv("$HOME/.lumen/history")
	os.MkdirAll(histDir, 0700)
	histFilename := histDir + "/" + time.Now().Format("2006-01-02-150405") + ".log"
	histFile, _ := os.Create(histFilename)
	if histFile != nil {
		defer histFile.Close()
	}

	// ── Load previous session ──
	if prevSession := loadLastSession(histDir, histFilename); prevSession != "" {
		fmt.Printf("  %s\n\n", dim("last session: "+prevSession))
	}

	// ── Header ──
	fmt.Printf("\n  %s  %s\n\n", bold(white("lumen")), dim(ctrl.ProviderName()+"/"+ctrl.ModelName()))

	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("  " + cyan("▸") + " ")
		if !sc.Scan() { break }
		text := strings.TrimSpace(sc.Text())

		if text == "" { continue }
		switch {
		case text == "/exit" || text == "/quit":
			fmt.Println()
			return nil
		case text == "/help":
			fmt.Printf("%s  /exit  /mode  /mode bypass|plan|default\n\n", dim(""))
			continue
		case text == "/mode":
			fmt.Printf("%s  bypass  plan  default  accept-edits\n\n", dim(""))
			continue
		case strings.HasPrefix(text, "/mode "):
			m := permission.ParseMode(strings.TrimPrefix(text, "/mode "))
			ctrl.SetPermissionMode(m)
			fmt.Printf("  %s\n\n", bold(cyan("["+string(m)+"]")))
			continue
		}

		// Echo user input back in cyan so they see what they typed.
		// Claude Code does this too.
		fmt.Printf("\n  %s %s\n\n", bold(cyan(text)), dim("— you"))

		// Save to history file
		if histFile != nil {
			fmt.Fprintf(histFile, "--- %s ---\n", time.Now().Format("2006-01-02 15:04:05"))
			fmt.Fprintf(histFile, "> %s\n\n", text)
		}

		ctrl.Run(context.Background(), text)
		fmt.Print("\n")
	}
	return nil
}

// ── ANSI helpers ───────────────────────────────────────────
// Already above; only bold/dim/cyan/green/red/yellow/white are needed.

// ── Markdown stripper ──────────────────────────────────────

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

// ── Session recovery ──────────────────────────────────────

func loadLastSession(dir, currentFile string) string {
	entries, err := os.ReadDir(dir)
	if err != nil { return "" }
	if len(entries) == 0 { return "" }

	// Find the most recent non-empty log file that isn't the current one
	var latest os.DirEntry
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() { continue }
		if dir+"/"+e.Name() == currentFile { continue }
		info, err := e.Info()
		if err != nil { continue }
		if info.Size() == 0 { continue }
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = e
		}
	}
	if latest == nil { return "" }
	return latest.Name()
}
