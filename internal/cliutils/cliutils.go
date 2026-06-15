// Package cliutils provides terminal utilities for building CLI tools.
// It includes a spinner, progress bar, ANSI colour helpers, a table writer,
// interactive confirm prompt, and input reader.
package cliutils

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// ANSI colour helpers.

// Colors provides ANSI escape sequences for terminal output.
type Colors struct{}

// Standard ANSI codes.
const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Italic  = "\033[3m"
	Under   = "\033[4m"
	Reverse = "\033[7m"
)

// Foreground colours.
const (
	FgBlack   = "\033[30m"
	FgRed     = "\033[31m"
	FgGreen   = "\033[32m"
	FgYellow  = "\033[33m"
	FgBlue    = "\033[34m"
	FgMagenta = "\033[35m"
	FgCyan    = "\033[36m"
	FgWhite   = "\033[37m"
	FgDefault = "\033[39m"
)

// Background colours.
const (
	BgBlack   = "\033[40m"
	BgRed     = "\033[41m"
	BgGreen   = "\033[42m"
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
	BgCyan    = "\033[46m"
	BgWhite   = "\033[47m"
	BgDefault = "\033[49m"
)

// Bright foreground.
const (
	FgBrightBlack   = "\033[90m"
	FgBrightRed     = "\033[91m"
	FgBrightGreen   = "\033[92m"
	FgBrightYellow  = "\033[93m"
	FgBrightBlue    = "\033[94m"
	FgBrightMagenta = "\033[95m"
	FgBrightCyan    = "\033[96m"
	FgBrightWhite   = "\033[97m"
)

// Wrap a string with colour codes and reset.
func (_ Colors) Red(s string) string     { return FgRed + s + Reset }
func (_ Colors) Green(s string) string   { return FgGreen + s + Reset }
func (_ Colors) Yellow(s string) string  { return FgYellow + s + Reset }
func (_ Colors) Blue(s string) string    { return FgBlue + s + Reset }
func (_ Colors) Magenta(s string) string { return FgMagenta + s + Reset }
func (_ Colors) Cyan(s string) string    { return FgCyan + s + Reset }
func (_ Colors) White(s string) string   { return FgWhite + s + Reset }
func (_ Colors) Bold(s string) string    { return Bold + s + Reset }
func (_ Colors) Dimmed(s string) string  { return Dim + s + Reset }

// Palette returns a named colour wrapper, e.g. "red" → FgRed.
func (_ Colors) Palette(name string) string {
	switch strings.ToLower(name) {
	case "red":
		return FgRed
	case "green":
		return FgGreen
	case "yellow":
		return FgYellow
	case "blue":
		return FgBlue
	case "magenta":
		return FgMagenta
	case "cyan":
		return FgCyan
	case "white":
		return FgWhite
	case "black":
		return FgBlack
	case "bright-red":
		return FgBrightRed
	case "bright-green":
		return FgBrightGreen
	case "bright-yellow":
		return FgBrightYellow
	case "bright-blue":
		return FgBrightBlue
	}
	return ""
}

// StripANSI removes all ANSI escape sequences from a string.
func StripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm'.
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// HasANSI reports whether the string contains ANSI escape codes.
func HasANSI(s string) bool {
	return strings.Contains(s, "\x1b[")
}

// ---------------------------------------------------------------------------
// Spinner — an animated indicator for in-progress operations.

// Spinner frames.
var (
	SpinnerDots   = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	SpinnerLine   = []string{"|", "/", "—", "\\"}
	SpinnerDots2  = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	SpinnerBounce = []string{"[    ]", "[=   ]", "[==  ]", "[=== ]", "[ ===]", "[  ==]", "[   =]", "[    ]"}
)

// Spinner draws an animated indicator on stderr.
type Spinner struct {
	frames   []string
	message  string
	stop     chan struct{}
	done     chan struct{}
	w        io.Writer
	interval time.Duration
	mu       sync.Mutex
	running  bool
}

// NewSpinner creates a spinner with default frames.
func NewSpinner(message string) *Spinner {
	return &Spinner{
		frames:   SpinnerDots,
		message:  message,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		w:        os.Stderr,
		interval: 100 * time.Millisecond,
	}
}

// SetFrames overrides the animation frames.
func (s *Spinner) SetFrames(f []string) *Spinner { s.frames = f; return s }

// SetInterval sets the frame duration.
func (s *Spinner) SetInterval(d time.Duration) *Spinner { s.interval = d; return s }

// SetWriter sets the output writer (default stderr).
func (s *Spinner) SetWriter(w io.Writer) *Spinner { s.w = w; return s }

// Message updates the label text.
func (s *Spinner) Message(msg string) {
	s.mu.Lock()
	s.message = msg
	s.mu.Unlock()
}

// Start begins the animation.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	go s.run()
}

// Stop ends the animation and clears the line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()
	close(s.stop)
	<-s.done
	// Clear the spinner line.
	fmt.Fprint(s.w, "\r\033[K")
}

// StopWithMessage stops and prints a final message.
func (s *Spinner) StopWithMessage(msg string) {
	s.Stop()
	fmt.Fprintln(s.w, msg)
}

func (s *Spinner) run() {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	idx := 0
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.message
			s.mu.Unlock()
			frame := s.frames[idx%len(s.frames)]
			idx++
			line := fmt.Sprintf("\r%s %s", frame, msg)
			fmt.Fprint(s.w, line)
		}
	}
}

// ---------------------------------------------------------------------------
// ProgressBar — a simple terminal progress bar.

// ProgressBar renders a [====>    ] style progress indicator.
type ProgressBar struct {
	total     int64
	current   int64
	width     int
	w         io.Writer
	start     time.Time
	mu        sync.Mutex
	prefix    string
	showPct   bool
	showCount bool
	showETA   bool
}

// NewProgressBar creates a progress bar with a given total.
func NewProgressBar(total int64) *ProgressBar {
	return &ProgressBar{
		total:     total,
		width:     40,
		w:         os.Stderr,
		start:     time.Now(),
		showPct:   true,
		showCount: true,
		showETA:   true,
	}
}

// SetWidth overrides the bar width in characters.
func (pb *ProgressBar) SetWidth(w int) *ProgressBar { pb.width = w; return pb }

// SetWriter sets the output destination.
func (pb *ProgressBar) SetWriter(w io.Writer) *ProgressBar { pb.w = w; return pb }

// SetPrefix sets a label before the bar.
func (pb *ProgressBar) SetPrefix(p string) *ProgressBar { pb.prefix = p; return pb }

// Add advances the progress by delta.
func (pb *ProgressBar) Add(delta int64) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.current += delta
	if pb.current > pb.total {
		pb.current = pb.total
	}
	pb.render()
}

// Set sets the absolute progress value.
func (pb *ProgressBar) Set(v int64) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.current = v
	if pb.current > pb.total {
		pb.current = pb.total
	}
	pb.render()
}

// Done marks the bar as complete and prints a newline.
func (pb *ProgressBar) Done() {
	pb.mu.Lock()
	pb.current = pb.total
	pb.render()
	pb.mu.Unlock()
	fmt.Fprint(pb.w, "\n")
}

func (pb *ProgressBar) render() {
	if pb.total == 0 {
		return
	}
	frac := float64(pb.current) / float64(pb.total)
	done := int(frac * float64(pb.width))
	rem := pb.width - done
	bar := strings.Repeat("=", done)
	if done < pb.width {
		bar += ">"
		rem--
	}
	bar += strings.Repeat(" ", rem)

	var parts []string
	if pb.prefix != "" {
		parts = append(parts, pb.prefix)
	}
	parts = append(parts, "["+bar+"]")
	if pb.showPct {
		parts = append(parts, fmt.Sprintf("%3.0f%%", frac*100))
	}
	if pb.showCount {
		parts = append(parts, fmt.Sprintf("%d/%d", pb.current, pb.total))
	}
	if pb.showETA && pb.current > 0 {
		elapsed := time.Since(pb.start)
		rate := float64(pb.current) / elapsed.Seconds()
		if rate > 0 {
			remaining := float64(pb.total-pb.current) / rate
			parts = append(parts, fmt.Sprintf("%s remaining", time.Duration(remaining)*time.Second))
		}
	}
	line := "\r" + strings.Join(parts, " ") + "\033[K"
	fmt.Fprint(pb.w, line)
}

// ---------------------------------------------------------------------------
// TableWriter — writes aligned tabular data.

// Align controls column alignment.
type Align int

const (
	AlignLeft Align = iota
	AlignRight
	AlignCenter
)

// TableWriter builds a formatted table.
type TableWriter struct {
	headers   []string
	rows      [][]string
	aligns    []Align
	colWidths []int
	padCols   int // padding on each side of a column
}

// NewTableWriter creates a table writer with headers.
func NewTableWriter(headers ...string) *TableWriter {
	tw := &TableWriter{
		headers: headers,
		padCols: 1,
	}
	tw.colWidths = make([]int, len(headers))
	tw.aligns = make([]Align, len(headers))
	for i, h := range headers {
		tw.colWidths[i] = utf8.RuneCountInString(h)
	}
	return tw
}

// SetAlign sets alignment for all columns, or per-column.
func (tw *TableWriter) SetAlign(aligns ...Align) *TableWriter {
	for i, a := range aligns {
		if i < len(tw.aligns) {
			tw.aligns[i] = a
		}
	}
	return tw
}

// AddRow appends a row.
func (tw *TableWriter) AddRow(cells ...string) *TableWriter {
	tw.rows = append(tw.rows, cells)
	for i, c := range cells {
		if i < len(tw.colWidths) {
			w := utf8.RuneCountInString(c)
			if w > tw.colWidths[i] {
				tw.colWidths[i] = w
			}
		}
	}
	return tw
}

// Render formats the table as a string.
func (tw *TableWriter) Render() string {
	var b strings.Builder
	pad := strings.Repeat(" ", tw.padCols)
	tw.writeRow(&b, tw.headers, tw.colWidths, tw.aligns, pad, true)
	// Separator.
	for i, w := range tw.colWidths {
		b.WriteString(strings.Repeat("─", w+2*tw.padCols))
		if i < len(tw.colWidths)-1 {
			b.WriteString("┼")
		}
	}
	b.WriteString("\n")
	for _, row := range tw.rows {
		tw.writeRow(&b, row, tw.colWidths, tw.aligns, pad, false)
	}
	return b.String()
}

// RenderPlain renders without Unicode box-drawing (ASCII only).
func (tw *TableWriter) RenderPlain() string {
	var b strings.Builder
	pad := strings.Repeat(" ", tw.padCols)
	tw.writeRow(&b, tw.headers, tw.colWidths, tw.aligns, pad, true)
	// Separator.
	for i, w := range tw.colWidths {
		b.WriteString(strings.Repeat("-", w+2*tw.padCols))
		if i < len(tw.colWidths)-1 {
			b.WriteString("+")
		}
	}
	b.WriteString("\n")
	for _, row := range tw.rows {
		tw.writeRow(&b, row, tw.colWidths, tw.aligns, pad, false)
	}
	return b.String()
}

func (tw *TableWriter) writeRow(b *strings.Builder, cells []string, widths []int, aligns []Align, pad string, header bool) {
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		var aligned string
		al := AlignLeft
		if i < len(aligns) {
			al = aligns[i]
		}
		cellWidth := utf8.RuneCountInString(cell)
		switch al {
		case AlignRight:
			aligned = strings.Repeat(" ", w-cellWidth) + cell
		case AlignCenter:
			left := (w - cellWidth) / 2
			right := w - cellWidth - left
			aligned = strings.Repeat(" ", left) + cell + strings.Repeat(" ", right)
		default:
			aligned = cell + strings.Repeat(" ", w-cellWidth)
		}
		b.WriteString(pad)
		if header {
			b.WriteString(Bold + aligned + Reset)
		} else {
			b.WriteString(aligned)
		}
		b.WriteString(pad)
		if i < len(widths)-1 {
			b.WriteString("│")
		}
	}
	b.WriteString("\n")
}

// ---------------------------------------------------------------------------
// Confirm — interactive yes/no prompt.

// Confirm asks the user a yes/no question on stderr and reads from stdin.
// Default indicates the default answer when the user presses enter.
func Confirm(question string, defaultYes bool) (bool, error) {
	hint := " (y/N)"
	if defaultYes {
		hint = " (Y/n)"
	}
	fmt.Fprint(os.Stderr, question+hint+": ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes, nil
	}
	switch line {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	}
	return false, fmt.Errorf("cliutils: ambiguous response %q", line)
}

// ---------------------------------------------------------------------------
// ReadInput — read a line from stdin.

// ReadInput reads a line from stdin with an optional prompt.
func ReadInput(prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(os.Stderr, prompt+" ")
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// ReadPassword reads a line without echoing (stub — falls back to ReadInput).
func ReadPassword(prompt string) (string, error) {
	// Stub: real implementation needs terminal-specific syscalls.
	return ReadInput(prompt)
}

// ---------------------------------------------------------------------------
// MultiSelect — interactive multi-choice prompt.

// MultiSelect presents a list and lets the user pick with arrow keys (simplified: space toggles, enter confirms).
// This is a stub that falls back to numbered selection.
func MultiSelect(prompt string, options []string) ([]string, error) {
	if len(options) == 0 {
		return nil, nil
	}
	fmt.Fprintln(os.Stderr, prompt)
	for i, o := range options {
		fmt.Fprintf(os.Stderr, "  [%d] %s\n", i+1, o)
	}
	fmt.Fprint(os.Stderr, "Enter numbers separated by commas (or 'all'): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "all" {
		out := make([]string, len(options))
		copy(out, options)
		return out, nil
	}
	var selected []string
	parts := strings.Split(line, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		idx, err := parseInt(p)
		if err != nil || idx < 1 || idx > len(options) {
			continue
		}
		selected = append(selected, options[idx-1])
	}
	return selected, nil
}

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %s", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Info / Warning / Error / Success helpers that print coloured lines.

var clr Colors

// Info prints a blue informational line.
func Info(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, clr.Blue("ℹ "+fmt.Sprintf(format, args...)))
}

// Success prints a green success line.
func Success(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, clr.Green("✓ "+fmt.Sprintf(format, args...)))
}

// Warn prints a yellow warning line.
func Warn(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, clr.Yellow("⚠ "+fmt.Sprintf(format, args...)))
}

// Error prints a red error line.
func Error(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, clr.Red("✗ "+fmt.Sprintf(format, args...)))
}

// ---------------------------------------------------------------------------
// Clearing lines and cursor control.

// ClearLine clears the current line.
func ClearLine(w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprint(w, "\r\033[K")
}

// MoveUp moves the cursor up n lines.
func MoveUp(w io.Writer, n int) {
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "\033[%dA", n)
}

// HideCursor hides the terminal cursor.
func HideCursor(w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprint(w, "\033[?25l")
}

// ShowCursor shows the terminal cursor.
func ShowCursor(w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprint(w, "\033[?25h")
}
