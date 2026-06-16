// terminal.go — Claude Code / Reasonix color scheme.
// Every element has a distinct color so the eye can scan naturally.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/lineedit"
	"lumen/internal/permission"
	"lumen/internal/render"
)

type liveStats struct {
	tkIn    atomic.Int64
	tkOut   atomic.Int64
	tkCache atomic.Int64
	tools   atomic.Int64
	step    atomic.Int64
	costU   atomic.Int64
}

func (s *liveStats) addCost(v float64) { s.costU.Add(int64(v * 1_000_000)) }
func (s *liveStats) cost() float64     { return float64(s.costU.Load()) / 1_000_000 }

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
func white(s string) string       { return color(s, "\033[97m") }
func bold(s string) string        { return color(s, "\033[1m") }
func dim(s string) string         { return color(s, "\033[2m") }
func italic(s string) string      { return color(s, "\033[3m") }
func cyan(s string) string        { return color(s, "\033[36m") }
func green(s string) string       { return color(s, "\033[32m") }
func red(s string) string         { return color(s, "\033[31m") }
func yellow(s string) string      { return color(s, "\033[33m") }

// ── Sink: colour-coded output ──────────────────────────────
//
// In chat mode, all agent output goes to stderr so the line editor's
// stdout cursor management is never disturbed. Run mode shares the same
// sink but prints to stdout directly (no line editor to clash with).

func termSink() event.Sink {
	thinking := false
	textStarted := false
	textLen := 0
	truncated := false
	const maxOutput = 24 * 1024

	rstream := render.NewStream(os.Stderr)
	rstream.Indent = "  "

	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted:
			thinking = true
			textStarted = false
			textLen = 0
			truncated = false
			stats.step.Store(0)
			rstream = render.NewStream(os.Stderr)
			rstream.Indent = "  "

		case event.Reasoning:
			if thinking && !textStarted {
				fmt.Fprint(os.Stderr, italic(dim(render.Markdown(e.Text))))
			}

		case event.Text:
			if thinking && !textStarted {
				thinking = false
				textStarted = true
				fmt.Fprint(os.Stderr, "\n")
			}
			if textLen >= maxOutput {
				if !truncated {
					truncated = true
					rstream.Flush()
					fmt.Fprintf(os.Stderr, "\n  %s\n", dim(fmt.Sprintf("… output truncated (%d bytes max)", maxOutput)))
				}
				return
			}
			textLen += len(e.Text)
			rstream.Write(e.Text)

		case event.ToolDispatch:
			thinking = false
			textStarted = true
			rstream.Flush()
			stats.tools.Add(1)
			sn := stats.step.Add(1)
			fmt.Fprintf(os.Stderr, "\n  %s  %s", cyan(fmt.Sprintf("[%d]", sn)), dim(yellow("⚡ "+e.Tool.Name)))

		case event.ToolResult:
			if e.Tool.Err != "" {
				fmt.Fprintf(os.Stderr, "  %s\n", red("✗ "+e.Tool.Err))
			} else if e.Tool.Blocked {
				fmt.Fprintf(os.Stderr, "  %s\n", dim("⊘ blocked"))
			} else {
				fmt.Fprintf(os.Stderr, "  %s\n", green("✓"))
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
			fmt.Fprintf(os.Stderr, "\n%s\n", cyan("── Preview ──────────────────────────────"))
			fmt.Fprint(os.Stderr, e.DiffText)
			fmt.Fprintf(os.Stderr, "%s\n", cyan("──────────────────────────────────────────"))

		case event.TurnDone:
			rstream.Flush()
			drawFooter()
			thinking = false
			textStarted = false
			stats.step.Store(0)
		}
	})
}

// ── Footer ─────────────────────────────────────────────────

func drawFooter() {
	ti := stats.tkIn.Load()
	to := stats.tkOut.Load()
	tc := stats.tkCache.Load()
	cost := stats.cost()
	step := stats.step.Load()

	cachePct := 0
	if ti > 0 {
		cachePct = int(float64(tc) / float64(ti) * 100)
	}
	fmt.Fprintf(os.Stderr, "%s  %s∶%s %s  ·  %scache %d%%%s  ·  %s$%.4f%s  ·  %s%d steps%s\n",
		dim(""), cyan(fmt.Sprint(ti/1000)), green(fmt.Sprint(to/1000)), dim("tokens"),
		dim(""), cachePct, dim(""),
		dim(""), cost, dim(""),
		dim(""), step, dim(""))
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

	histPath := os.ExpandEnv("$HOME/.lumen/input_history")
	cwd, _ := os.Getwd()
	ed := lineedit.NewEditor("  "+cyan("▸")+" ", histPath, cwd)
	for {
		line, err := ed.ReadLine()
		if err == io.EOF {
			fmt.Println()
			return nil
		}
		if err != nil {
			return err
		}
		text := strings.TrimSpace(line)

		if text == "" {
			continue
		}
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

// ── Session recovery ──────────────────────────────────────

func loadLastSession(dir, currentFile string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	if len(entries) == 0 {
		return ""
	}

	// Find the most recent non-empty log file that isn't the current one
	var latest os.DirEntry
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if dir+"/"+e.Name() == currentFile {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() == 0 {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = e
		}
	}
	if latest == nil {
		return ""
	}
	return latest.Name()
}
