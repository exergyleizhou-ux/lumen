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
	lastRows  int    // number of terminal rows the last redraw occupied
	promptRow int    // absolute terminal row where the prompt line starts (1-based)
	pending   []byte // input bytes read but not yet consumed (carries across ReadLine calls)
	pasting   bool   // true between ESC[200~ and ESC[201~ (bracketed paste)
}

// NewEditor creates an Editor with the given prompt, history file path, and
// completion root directory. It reads stdin and writes stdout.
func NewEditor(prompt, histPath, root string) *Editor {
	e := &Editor{prompt: prompt, histPath: histPath, root: root, in: os.Stdin, out: os.Stdout}
	e.loadHistory()
	return e
}

// EnableBracketedPaste turns on terminal bracketed-paste reporting for the whole
// session. Call once when the REPL starts (and DisableBracketedPaste on exit).
// Enabling it session-wide — rather than per ReadLine — means a paste that
// arrives while a turn is running is still wrapped in ESC[200~ … ESC[201~ and
// handled as one block on the next prompt. No-op when stdin is not a terminal.
func (e *Editor) EnableBracketedPaste() {
	if term.IsTerminal(int(e.in.Fd())) {
		io.WriteString(e.out, "\x1b[?2004h")
	}
}

// DisableBracketedPaste turns bracketed-paste reporting back off so the shell
// the user returns to doesn't inherit it. No-op when stdin is not a terminal.
func (e *Editor) DisableBracketedPaste() {
	if term.IsTerminal(int(e.in.Fd())) {
		io.WriteString(e.out, "\x1b[?2004l")
	}
}

// handle applies one key event to the editor state and returns the resulting
// action. It performs no I/O, so it is fully unit-testable.
func (e *Editor) handle(ev keyEvent) action {
	switch ev.typ {
	case keyRune:
		e.buf.insert(ev.r)
		return actRedraw
	case keyEnter:
		if e.pasting {
			// A newline INSIDE a bracketed paste is pasted content, not a submit.
			// Flatten it to a space so a multi-line paste becomes one editable
			// line the user submits with a single explicit Enter — instead of N
			// lines that each auto-submit as their own agent turn.
			e.buf.insert(' ')
			return actRedraw
		}
		return actSubmit
	case keyPasteStart:
		e.pasting = true
		return actNone
	case keyPasteEnd:
		e.pasting = false
		return actRedraw
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

	// Bracketed-paste mode (ESC[200~ … ESC[201~) is enabled once for the whole
	// REPL via EnableBracketedPaste, NOT per ReadLine. Toggling it per call left
	// a window — between turns, while a turn was running — where a paste was not
	// wrapped and resumed submitting line-by-line. drain() handles the markers
	// whenever they arrive.

	e.buf.clear()
	e.pasting = false
	e.hist.idx = len(e.hist.items)
	e.redraw()

	// e.pending carries unconsumed bytes across reads (and across ReadLine
	// calls). Anything already read past a submitting newline — or the partial
	// tail of a multibyte rune split by a read boundary — stays here instead of
	// being dropped. Dropping a partial UTF-8 rune is what produced the leading
	// "�" on every line of a pasted multi-line block.
	readBuf := make([]byte, 4096) // pastes arrive in large chunks
	for {
		line, act, dirty := e.drain()
		// Repaint at most once per read batch — a large paste touches the buffer
		// many times but only the final state needs to be drawn.
		if dirty {
			e.redraw()
		}
		switch act {
		case actSubmit:
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
		}
		// actNone → e.pending holds only an incomplete sequence (or is empty):
		// read more and retry.
		n, err := e.in.Read(readBuf)
		if err != nil {
			return "", err
		}
		e.pending = append(e.pending, readBuf[:n]...)
	}
}

// drain consumes complete key events from e.pending, applying each to the
// buffer/paste state. It stops at the first submit (Enter outside a paste),
// cancel (Ctrl-C), or EOF (Ctrl-D on empty), returning that action; otherwise
// it returns actNone, meaning e.pending is empty or holds only an incomplete
// escape sequence or partial UTF-8 rune that the caller must complete with more
// input. Leftover bytes always remain in e.pending. dirty reports whether the
// visible buffer changed (so the caller can repaint once per batch). drain does
// no I/O, so it is fully unit-testable.
func (e *Editor) drain() (line string, act action, dirty bool) {
	for len(e.pending) > 0 {
		// Bare ESC (no following CSI): a lone 0x1b is a standalone Escape. Real
		// CSI sequences arrive atomically, so they're never a single byte here.
		if len(e.pending) == 1 && e.pending[0] == 0x1b {
			e.pending = e.pending[:0]
			e.handle(keyEvent{typ: keyEsc})
			return "", actNone, true
		}
		ev, consumed := decodeKey(e.pending)
		if consumed == 0 {
			return "", actNone, dirty // incomplete — wait for more bytes
		}
		e.pending = e.pending[consumed:]
		switch e.handle(ev) {
		case actSubmit:
			return e.buf.string(), actSubmit, true
		case actCancel:
			return "", actCancel, dirty
		case actEOF:
			return "", actEOF, dirty
		default:
			dirty = true
		}
	}
	return "", actNone, dirty
}

func (e *Editor) readCooked() (string, error) {
	r := bufio.NewReader(e.in)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// redraw repaints the prompt and buffer. Uses ONLY per-line \x1b[K so the
// terminal's native scrollback buffer is never destroyed. \x1b[J would
// clear scrollback history on many terminals (macOS Terminal, iTerm2).
func (e *Editor) redraw() {
	fd := int(e.in.Fd())
	termW := 80
	if w, _, err := term.GetSize(fd); err == nil && w > 0 {
		termW = w
	}

	prompt := e.prompt
	text := e.buf.string()

	// Step 1: Move to start of current line, then clear every row the
	// previous render occupied — one \x1b[K at a time, moving upward.
	// \x1b[K only clears a single line; it NEVER touches scrollback.
	io.WriteString(e.out, "\r\x1b[K")
	for i := 1; i < e.lastRows; i++ {
		io.WriteString(e.out, "\x1b[A\x1b[K")
	}

	// Step 2: Write prompt + full buffer (may auto-wrap).
	io.WriteString(e.out, prompt)
	io.WriteString(e.out, text)

	// Step 3: Position cursor at correct column.
	prefix := string(e.buf.runes[:e.buf.pos])
	cursorOffset := runewidth.StringWidth(prompt) + runewidth.StringWidth(prefix)
	io.WriteString(e.out, "\r")
	if cursorOffset > 0 {
		fmt.Fprintf(e.out, "\x1b[%dC", cursorOffset)
	}

	// Step 4: Track rows for next clear.
	promptW := runewidth.StringWidth(prompt)
	textW := runewidth.StringWidth(text)
	e.lastRows = 1
	if termW > 0 {
		e.lastRows = (promptW + textW + termW - 1) / termW
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
