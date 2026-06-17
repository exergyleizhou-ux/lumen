package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lumen/internal/reliability"
)

// sessionStats holds aggregated statistics for one session file.
type sessionStats struct {
	File      string    `json:"file"`
	Date      time.Time `json:"date"`
	Lines     int       `json:"lines"`
	Messages  int       `json:"messages"`  // non-tool JSONL entries (system+user+assistant)
	Turns     int       `json:"turns"`     // user→assistant exchanges
	EstTokens int       `json:"est_tokens"` // rough estimate from content length
}

// runStats reads session history files and prints aggregate statistics.
func runStats() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	histDir := filepath.Join(home, ".lumen", "history")

	entries, err := os.ReadDir(histDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", histDir, err)
		os.Exit(1)
	}

	var sessions []sessionStats
	totalMessages := 0
	totalTurns := 0
	totalTokens := 0
	totalLines := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(histDir, entry.Name())
		ss := parseSessionFile(path, entry)
		if ss == nil {
			continue
		}
		sessions = append(sessions, *ss)
		totalMessages += ss.Messages
		totalTurns += ss.Turns
		totalTokens += ss.EstTokens
		totalLines += ss.Lines
	}

	if len(sessions) == 0 {
		fmt.Println("No session history found.")
		return
	}

	// Sort by date descending
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Date.After(sessions[j].Date)
	})

	// ── Per-session table ──
	fmt.Printf("%-22s %5s %5s %5s %8s\n", "Session", "Msgs", "Turns", "Lines", "EstTok")
	fmt.Println(strings.Repeat("─", 52))
	for _, s := range sessions {
		name := filepath.Base(s.File)
		if len(name) > 22 {
			name = name[:22]
		}
		fmt.Printf("%-22s %5d %5d %5d %8d\n", name, s.Messages, s.Turns, s.Lines, s.EstTokens)
	}

	// ── Totals ──
	fmt.Println(strings.Repeat("─", 52))
	fmt.Printf("%-22s %5d %5d %5d %8d\n", "TOTAL", totalMessages, totalTurns, totalLines, totalTokens)

	// ── Summary ──
	fmt.Println()
	fmt.Printf("Session files: %d\n", len(sessions))
	fmt.Printf("Total messages: %d (%d turns)\n", totalMessages, totalTurns)
	fmt.Printf("Estimated tokens: ~%d\n", totalTokens)
	fmt.Printf("Avg tokens/session: ~%d\n", totalTokens/len(sessions))
	fmt.Printf("Avg tokens/turn: ~%d\n", avg(totalTokens, totalTurns))
	fmt.Printf("Avg turns/session: ~%d\n", avg(totalTurns, len(sessions)))
}

// parseSessionFile reads one history file and returns stats, or nil if unreadable.
// Supports both .jsonl (structured) and .log (plain text) formats.
func parseSessionFile(path string, entry os.DirEntry) *sessionStats {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	ss := &sessionStats{
		File: entry.Name(),
		Date: parseFileDate(entry.Name()),
	}

	if strings.HasSuffix(entry.Name(), ".jsonl") {
		scanner := bufio.NewScanner(f)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			ss.Lines++

			var raw struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls any    `json:"tool_calls,omitempty"`
			}
			if err := json.Unmarshal([]byte(line), &raw); err != nil {
				continue
			}
			// Count messages: system, user, assistant (not tool responses)
			if raw.Role != "tool" {
				ss.Messages++
				if raw.Role == "user" {
					ss.Turns++
				}
			}
			ss.EstTokens += estimateTokens(raw.Content)
		}
	} else if strings.HasSuffix(entry.Name(), ".log") {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			ss.Lines++
			ss.Messages++
			if strings.HasPrefix(line, "> ") {
				ss.Turns++
				ss.EstTokens += estimateTokens(strings.TrimPrefix(line, "> "))
			}
		}
	} else {
		return nil // unknown format
	}

	return ss
}

// parseFileDate extracts the date from a session filename like "2026-06-16-221909.jsonl".
func parseFileDate(name string) time.Time {
	// Strip extension
	name = strings.TrimSuffix(name, ".jsonl")
	name = strings.TrimSuffix(name, ".log")
	// Parse "2006-01-02-150405"
	t, err := time.Parse("2006-01-02-150405", name)
	if err != nil {
		return time.Time{}
	}
	return t
}

// estimateTokens returns a rough token count for a string.
// ~4 chars/token for ASCII, ~2 chars/token for CJK.
func estimateTokens(s string) int {
	ascii := 0
	cjk := 0
	for _, r := range s {
		if r > 0x2E80 && r < 0x9FFF || r > 0xF900 && r < 0xFAFF || r > 0xFF00 && r < 0xFFEF {
			cjk++
		} else {
			ascii++
		}
	}
	return ascii/4 + cjk/2 + 1
}

func avg(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}

// ── Monthly Reliability Report (SpaceX Phase 3) ──────────

func runReliability() {
	histDir := filepath.Join(os.ExpandEnv("$HOME"), ".lumen", "history")
	now := time.Now()
	r := reliability.Generate(histDir, now.Year(), now.Month())
	fmt.Print(r.Print())
	path, err := r.Save()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  ⚠️  save report: %v\n", err)
	} else {
		fmt.Printf("\n  📄 saved: %s\n", path)
	}
}
