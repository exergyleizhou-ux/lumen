package lineedit

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-runewidth"

	"golang.org/x/term"
)

// ── history ───────────────────────────────────────────────

type history struct {
	items []string
	idx   int // cursor; == len(items) means "the new line being edited"
}

func (h *history) add(s string) {
	if s == "" {
		h.idx = len(h.items)
		return
	}
	if len(h.items) > 0 && h.items[len(h.items)-1] == s {
		h.idx = len(h.items)
		return
	}
	h.items = append(h.items, s)
	h.idx = len(h.items)
}

func (h *history) up() (string, bool) {
	if len(h.items) == 0 {
		return "", false
	}
	if h.idx > 0 {
		h.idx--
	}
	return h.items[h.idx], true
}

func (h *history) down() (string, bool) {
	if h.idx < len(h.items)-1 {
		h.idx++
		return h.items[h.idx], true
	}
	h.idx = len(h.items)
	return "", true
}

// ── editor ────────────────────────────────────────────────

type action int

const (
	actNone action = iota
	actRedraw
	actSubmit
	actCancel
	actEOF
)

// Editor reads a line of input with editing, history, and completion.
type Editor struct {
	prompt    string
	histPath  string
	root      string
	in        *os.File
	out       io.Writer
	buf       buffer
	hist      history
	lastRows  int // number of terminal rows the last redraw occupied
	promptRow int // absolute terminal row where the prompt line starts (1-based)
}

// NewEditor creates an Editor with the given prompt, history file path, and
// completion root directory. It reads stdin and writes stdout.
func NewEditor(prompt, histPath, root string) *Editor {
	e := &Editor{prompt: prompt, histPath: histPath, root: root, in: os.Stdin, out: os.Stdout}
	e.loadHistory()
	return e
}

// handle applies one key event to the editor state and returns the resulting
// action. It performs no I/O, so it is fully unit-testable.
func (e *Editor) handle(ev keyEvent) action {
	switch ev.typ {
	case keyRune:
		e.buf.insert(ev.r)
		return actRedraw
	case keyEnter:
		return actSubmit
	case keyBackspace:
		e.buf.backspace()
		return actRedraw
	case keyDelete:
		e.buf.deleteFwd()
		return actRedraw
	case keyLeft:
		e.buf.left()
		return actRedraw
	case keyRight:
		e.buf.right()
		return actRedraw
	case keyHome:
		e.buf.home()
		return actRedraw
	case keyEnd:
		e.buf.end()
		return actRedraw
	case keyMouse:
		pos := e.clickToPos(ev.mouseRow, ev.mouseCol)
		if pos >= 0 {
			e.buf.pos = pos
			if e.buf.pos > len(e.buf.runes) {
				e.buf.pos = len(e.buf.runes)
			}
		}
		return actRedraw
	case keyUp:
		if s, ok := e.hist.up(); ok {
			e.buf.setLine(s)
		}
		return actRedraw
	case keyDown:
		if s, ok := e.hist.down(); ok {
			e.buf.setLine(s)
		}
		return actRedraw
	case keyTab:
		e.complete()
		return actRedraw
	case keyCtrlC:
		return actCancel
	case keyCtrlD:
		if len(e.buf.runes) == 0 {
			return actEOF
		}
		e.buf.deleteFwd()
		return actRedraw
	case keyCtrlW:
		e.wordBackspace()
		return actRedraw
	case keyCtrlK:
		e.buf.killToEnd()
		return actRedraw
	case keyEsc:
		// ESC: clear buffer if non-empty, otherwise no-op
		if len(e.buf.runes) > 0 {
			e.buf.clear()
		}
		return actRedraw
	default:
		return actNone
	}
}

// wordBackspace deletes from cursor backwards to the previous word boundary.
func (e *Editor) wordBackspace() {
	if e.buf.pos == 0 {
		return
	}
	// Skip trailing whitespace
	pos := e.buf.pos
	for pos > 0 && e.buf.runes[pos-1] == ' ' {
		pos--
		e.buf.runes = append(e.buf.runes[:pos], e.buf.runes[pos+1:]...)
	}
	// Delete to next word boundary or beginning
	for pos > 0 && e.buf.runes[pos-1] != ' ' {
		pos--
		e.buf.runes = append(e.buf.runes[:pos], e.buf.runes[pos+1:]...)
	}
	e.buf.pos = pos
}

// complete performs Tab-completion of slash-commands and @-file mentions.
func (e *Editor) complete() {
	line := e.buf.string()

	if strings.HasPrefix(line, "/") && !strings.Contains(line, " ") {
		m := MatchCommands(line)
		switch {
		case len(m) == 1:
			e.buf.setLine(m[0].Name + " ")
		case len(m) > 1:
			names := make([]string, len(m))
			for i, c := range m {
				names[i] = c.Name
			}
			if cp := commonPrefix(names); len(cp) > len(line) {
				e.buf.setLine(cp)
			}
		}
		return
	}

	word := currentWord(line)
	if !strings.HasPrefix(word, "@") {
		return
	}
	matches := CompletePath(e.root, word[1:])
	head := line[:len(line)-len(word)]
	switch {
	case len(matches) == 1:
		e.buf.setLine(head + "@" + matches[0])
	case len(matches) > 1:
		if cp := commonPrefix(matches); len(cp) > len(word)-1 {
			e.buf.setLine(head + "@" + cp)
		}
	}
}

func currentWord(line string) string {
	if i := strings.LastIndexByte(line, ' '); i >= 0 {
		return line[i+1:]
	}
	return line
}

// ReadLine reads one line interactively. On a non-terminal stdin it falls back
// to a plain buffered read so pipelines and tests still work. Returns io.EOF on
// Ctrl-D at an empty prompt.
func (e *Editor) ReadLine() (string, error) {
	fd := int(e.in.Fd())
	if !term.IsTerminal(fd) {
		return e.readCooked()
	}

	// promptRow=0 means clickToPos assumes the prompt is on row 0.
	// This is correct for a full-screen app where the prompt is always
	// at the same position, and avoids DSR round-trip latency + leaked
	// control sequences. In line-mode chat (multiple output lines above
	// the prompt), mouse clicks on wrapped lines will still position
	// correctly within the buffer row; only the absolute-row-offset
	// correction is skipped.
	e.promptRow = 0

	old, err := term.MakeRaw(fd)
	if err != nil {
		return e.readCooked()
	}
	defer term.Restore(fd, old)

	// Enable SGR mouse tracking (xterm protocol)
	io.WriteString(e.out, "\x1b[?1000h\x1b[?1006h")
	defer io.WriteString(e.out, "\x1b[?1006l\x1b[?1000l")

	e.buf.clear()
	e.hist.idx = len(e.hist.items)
	e.redraw()

	var pending []byte
	readBuf := make([]byte, 64)
	for {
		n, err := e.in.Read(readBuf)
		if err != nil {
			return "", err
		}
		pending = append(pending, readBuf[:n]...)
		for len(pending) > 0 {
			// Bare ESC (no following CSI): if we got exactly 0x1b alone
			// in this read, treat it as a standalone escape. CSI sequences
			// come atomically from terminals so they'll never be split.
			if len(pending) == 1 && pending[0] == 0x1b {
				pending = nil
				e.handle(keyEvent{typ: keyEsc})
				e.redraw()
				break
			}
			ev, consumed := decodeKey(pending)
			if consumed == 0 {
				break // incomplete sequence — read more
			}
			pending = pending[consumed:]
			switch e.handle(ev) {
			case actSubmit:
				line := e.buf.string()
				io.WriteString(e.out, "\r\n")
				if t := strings.TrimSpace(line); t != "" {
					e.hist.add(t)
					e.saveHistory(t)
				}
				return line, nil
			case actCancel:
				io.WriteString(e.out, "^C\r\n")
				return "", nil
			case actEOF:
				io.WriteString(e.out, "\r\n")
				return "", io.EOF
			default:
				e.redraw()
			}
		}
	}
}

func (e *Editor) readCooked() (string, error) {
	r := bufio.NewReader(e.in)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// redraw repaints the prompt and buffer.
func (e *Editor) redraw() {
	fd := int(e.in.Fd())
	termW := 80
	if w, _, err := term.GetSize(fd); err == nil && w > 0 {
		termW = w
	}

	// 1. Save cursor position (the "anchor").
	io.WriteString(e.out, "\x1b7")

	// 2. Clear from anchor to end of screen.
	io.WriteString(e.out, "\x1b[J")

	// 3. Restore to anchor, write prompt + full buffer.
	io.WriteString(e.out, "\x1b8")
	text := e.buf.string()
	io.WriteString(e.out, e.prompt)
	io.WriteString(e.out, text)

	// 4. Compute cursor position within the (possibly multi-row) buffer.
	promptW := runewidth.StringWidth(e.prompt)
	prefixW := promptW + runewidth.StringWidth(string(e.buf.runes[:e.buf.pos]))
	cursorRow := 0
	cursorCol := prefixW
	if termW > 0 {
		cursorRow = prefixW / termW
		cursorCol = prefixW % termW
	}

	// 5. Back to anchor, move down to cursor's row, then right to cursor's col.
	io.WriteString(e.out, "\x1b8")
	if cursorRow > 0 {
		fmt.Fprintf(e.out, "\x1b[%dB", cursorRow)
	}
	if cursorCol > 0 {
		fmt.Fprintf(e.out, "\x1b[%dC", cursorCol)
	}

	// 6. lastRows for mouse clickToPos.
	e.lastRows = 1
	if termW > 0 {
		w := runewidth.StringWidth(e.prompt) + runewidth.StringWidth(text)
		e.lastRows = (w + termW - 1) / termW
	}
	if e.lastRows < 1 {
		e.lastRows = 1
	}
}

// clickToPos translates a mouse click at (absolute row, absolute col) from
// the terminal into a rune position within the buffer.
func (e *Editor) clickToPos(absRow, absCol int) int {
	// Convert absolute terminal row to relative row (0 = prompt line)
	relRow := absRow
	if e.promptRow > 0 {
		relRow = absRow - e.promptRow
	}
	if relRow < 0 {
		relRow = 0
	}

	fd := int(e.in.Fd())
	termW := 80
	if w, _, err := term.GetSize(fd); err == nil && w > 0 {
		termW = w
	}
	promptWidth := runewidth.StringWidth(e.prompt)

	// Click before prompt on first relative line → home
	if relRow == 0 && absCol < promptWidth {
		return 0
	}

	// Convert (relRow, absCol) to flat text display-column offset.
	// Row 0: first termW-promptWidth cols are text, then terminal wraps.
	// Row N>0: full termW cols of text after wrapping.
	targetCol := relRow*termW + absCol - promptWidth

	col := 0
	for i, r := range e.buf.runes {
		w := runewidth.RuneWidth(r)
		if targetCol < col+w {
			return i
		}
		col += w
	}
	return len(e.buf.runes)
}

// ── history persistence ───────────────────────────────────

func (e *Editor) loadHistory() {
	if e.histPath == "" {
		return
	}
	data, err := os.ReadFile(e.histPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			e.hist.items = append(e.hist.items, line)
		}
	}
	e.hist.idx = len(e.hist.items)
}

func (e *Editor) saveHistory(line string) {
	if e.histPath == "" {
		return
	}
	f, err := os.OpenFile(e.histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}
