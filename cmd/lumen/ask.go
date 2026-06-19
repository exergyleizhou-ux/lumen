package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"lumen/internal/event"
)

// lineAsker is the interactive-chat implementation of agent.Asker. When the
// `ask` tool runs mid-turn it renders each multiple-choice question and reads
// the user's selection from the terminal (which is in cooked/line mode while a
// turn runs, so input echoes normally). Without this wired in, the ask tool
// falls back to "headless — decide for yourself", which is why the agent used
// to guess instead of asking.
type lineAsker struct {
	in  io.Reader
	out io.Writer
}

func newLineAsker() *lineAsker { return &lineAsker{in: os.Stdin, out: os.Stdout} }

// Ask implements agent.Asker.
func (a *lineAsker) Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	answers := make([]event.AskAnswer, len(questions))
	for i, q := range questions {
		io.WriteString(a.out, renderAskQuestion(q))
		hint := "  " + fg(D, "▸ enter a number")
		if q.MultiSelect {
			hint = "  " + fg(D, "▸ enter numbers (comma-separated for multiple)")
		}
		io.WriteString(a.out, hint+" "+fg(C, "› "))

		line, err := readUntilNewline(a.in)
		if err != nil && line == "" {
			// EOF / closed input: fall back to the first option so the turn can
			// proceed instead of erroring out.
			line = ""
		}
		idxs := parseAskChoice(line, len(q.Options), q.MultiSelect)
		labels := make([]string, 0, len(idxs))
		for _, ix := range idxs {
			labels = append(labels, q.Options[ix].Label)
		}
		answers[i] = event.AskAnswer{Header: q.Header, Answers: labels}
	}
	return answers, nil
}

// renderAskQuestion formats one question with numbered options.
func renderAskQuestion(q event.AskQuestion) string {
	var b strings.Builder
	b.WriteString("\n  " + fg(B+C, "❓ "+q.Header) + "\n")
	if q.Question != "" {
		b.WriteString("  " + fg(W, q.Question) + "\n")
	}
	for i, o := range q.Options {
		line := fmt.Sprintf("    %s %s", fg(G, strconv.Itoa(i+1)+"."), o.Label)
		if o.Description != "" {
			line += "  " + fg(D, "— "+o.Description)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// parseAskChoice turns a user's reply into 0-based option indices. Accepts
// numbers separated by commas/spaces. Out-of-range and non-numeric tokens are
// ignored. For single-select only the first valid pick is kept. When nothing
// valid is entered it defaults to the first option so the turn never stalls.
func parseAskChoice(input string, n int, multi bool) []int {
	if n <= 0 {
		return nil
	}
	fields := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	var out []int
	seen := make(map[int]bool)
	for _, f := range fields {
		v, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || v < 1 || v > n {
			continue
		}
		ix := v - 1
		if seen[ix] {
			continue
		}
		seen[ix] = true
		out = append(out, ix)
		if !multi {
			break
		}
	}
	if len(out) == 0 {
		return []int{0} // default to first option
	}
	return out
}

// readUntilNewline reads one line (up to and including '\n') one byte at a time
// so it never consumes bytes past the newline — important because the line
// editor reads the same stdin directly and must not lose buffered input.
func readUntilNewline(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return strings.TrimRight(b.String(), "\r"), nil
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			return b.String(), err
		}
	}
}
