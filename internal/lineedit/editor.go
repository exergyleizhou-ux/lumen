package lineedit

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

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
	prompt   string
	histPath string
	root     string
	in       *os.File
	out      io.Writer
	buf      buffer
	hist     history
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
	default:
		return actNone
	}
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
	old, err := term.MakeRaw(fd)
	if err != nil {
		return e.readCooked()
	}
	defer term.Restore(fd, old)

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

// redraw repaints the prompt and current buffer, placing the cursor.
// Uses save/restore cursor rather than column arithmetic — immune to
// double-width characters (CJK, emoji, symbols) that break \x1b[%dC.
func (e *Editor) redraw() {
	io.WriteString(e.out, "\r\x1b[K")                // clear line
	io.WriteString(e.out, e.prompt)                    // prompt
	prefix := string(e.buf.runes[:e.buf.pos])          // text before cursor
	suffix := string(e.buf.runes[e.buf.pos:])          // text after cursor
	io.WriteString(e.out, prefix)                      // write prefix
	io.WriteString(e.out, "\x1b[s")                    // save — HERE is where cursor should be
	io.WriteString(e.out, suffix)                      // write suffix
	io.WriteString(e.out, "\x1b[u")                    // restore — back to correct position
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
