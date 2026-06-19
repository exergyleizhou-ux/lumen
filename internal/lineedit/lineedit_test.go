package lineedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── commands ──────────────────────────────────────────────

func TestMatchCommandsPrefix(t *testing.T) {
	got := MatchCommands("mo")
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name] = true
	}
	if !names["/mode"] || !names["/model"] {
		t.Fatalf("expected /mode and /model, got %v", names)
	}
	for _, c := range got {
		if c.Name == "/exit" {
			t.Fatal("/exit should not match prefix 'mo'")
		}
	}
}

func TestMatchCommandsAcceptsLeadingSlash(t *testing.T) {
	if len(MatchCommands("/mo")) != len(MatchCommands("mo")) {
		t.Fatal("leading slash should be tolerated")
	}
}

func TestCompletePath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "alpha.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "alfred.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "zeta.go"), nil, 0o644)
	got := CompletePath(dir, "al")
	if len(got) != 2 {
		t.Fatalf("want 2 matches for 'al', got %v", got)
	}
	if got[0] != "alfred.go" || got[1] != "alpha.go" {
		t.Fatalf("expected sorted [alfred.go alpha.go], got %v", got)
	}
}

// ── buffer ────────────────────────────────────────────────

func TestBufferInsertAndString(t *testing.T) {
	var b buffer
	b.insertString("helo")
	b.left()
	b.insert('l')
	if b.string() != "hello" {
		t.Fatalf("got %q", b.string())
	}
}

func TestBufferBackspace(t *testing.T) {
	var b buffer
	b.insertString("abc")
	b.backspace()
	if b.string() != "ab" {
		t.Fatalf("got %q", b.string())
	}
}

func TestBufferHomeEnd(t *testing.T) {
	var b buffer
	b.insertString("abc")
	b.home()
	b.insert('X')
	b.end()
	b.insert('Y')
	if b.string() != "XabcY" {
		t.Fatalf("got %q", b.string())
	}
}

// ── key decoding ──────────────────────────────────────────

func TestDecodeKeyArrows(t *testing.T) {
	ev, n := decodeKey([]byte("\x1b[A"))
	if ev.typ != keyUp || n != 3 {
		t.Fatalf("up: got typ=%v n=%d", ev.typ, n)
	}
	ev, n = decodeKey([]byte("\x1b[D"))
	if ev.typ != keyLeft || n != 3 {
		t.Fatalf("left: got typ=%v n=%d", ev.typ, n)
	}
}

func TestDecodeKeyRuneMultibyte(t *testing.T) {
	ev, n := decodeKey([]byte("中"))
	if ev.typ != keyRune || ev.r != '中' || n != 3 {
		t.Fatalf("got typ=%v r=%q n=%d", ev.typ, ev.r, n)
	}
}

func TestDecodeKeyControls(t *testing.T) {
	if ev, _ := decodeKey([]byte{0x7f}); ev.typ != keyBackspace {
		t.Fatal("0x7f should be backspace")
	}
	if ev, _ := decodeKey([]byte{0x0d}); ev.typ != keyEnter {
		t.Fatal("CR should be enter")
	}
	if ev, _ := decodeKey([]byte{0x03}); ev.typ != keyCtrlC {
		t.Fatal("0x03 should be ctrl-c")
	}
	if ev, _ := decodeKey([]byte{0x09}); ev.typ != keyTab {
		t.Fatal("0x09 should be tab")
	}
}

func TestDecodeKeyBracketedPaste(t *testing.T) {
	// ESC[200~ starts a paste, ESC[201~ ends it. Each is 6 bytes.
	if ev, n := decodeKey([]byte("\x1b[200~")); ev.typ != keyPasteStart || n != 6 {
		t.Fatalf("paste-start: got typ=%v n=%d", ev.typ, n)
	}
	if ev, n := decodeKey([]byte("\x1b[201~")); ev.typ != keyPasteEnd || n != 6 {
		t.Fatalf("paste-end: got typ=%v n=%d", ev.typ, n)
	}
	// A marker split across a read boundary must NOT be misdecoded — wait for more.
	if _, n := decodeKey([]byte("\x1b[20")); n != 0 {
		t.Fatalf("partial paste marker should consume 0, got n=%d", n)
	}
	// The start marker must take priority over the generic CSI fallthrough.
	if ev, n := decodeKey([]byte("\x1b[200~hello")); ev.typ != keyPasteStart || n != 6 {
		t.Fatalf("paste-start with trailing bytes: got typ=%v n=%d", ev.typ, n)
	}
}

// ── history ───────────────────────────────────────────────

func TestHistoryNavigation(t *testing.T) {
	var h history
	h.add("one")
	h.add("two")
	if s, ok := h.up(); !ok || s != "two" {
		t.Fatalf("first up: %q %v", s, ok)
	}
	if s, ok := h.up(); !ok || s != "one" {
		t.Fatalf("second up: %q %v", s, ok)
	}
	if s, ok := h.down(); !ok || s != "two" {
		t.Fatalf("down: %q %v", s, ok)
	}
}

// ── editor key handling (pure) ────────────────────────────

func TestHandleInsertAndSubmit(t *testing.T) {
	e := newTestEditor(t)
	for _, r := range "hi" {
		e.handle(keyEvent{typ: keyRune, r: r})
	}
	if act := e.handle(keyEvent{typ: keyEnter}); act != actSubmit {
		t.Fatalf("enter should submit, got %v", act)
	}
	if e.buf.string() != "hi" {
		t.Fatalf("buffer = %q", e.buf.string())
	}
}

func TestHandleCtrlDEmptyIsEOF(t *testing.T) {
	e := newTestEditor(t)
	if act := e.handle(keyEvent{typ: keyCtrlD}); act != actEOF {
		t.Fatalf("ctrl-d on empty should be EOF, got %v", act)
	}
}

func TestHandleUpRecallsHistory(t *testing.T) {
	e := newTestEditor(t)
	e.hist.add("earlier")
	e.handle(keyEvent{typ: keyUp})
	if e.buf.string() != "earlier" {
		t.Fatalf("up should recall history, got %q", e.buf.string())
	}
}

func TestHandleTabCompletesCommand(t *testing.T) {
	e := newTestEditor(t)
	for _, r := range "/mod" {
		e.handle(keyEvent{typ: keyRune, r: r})
	}
	e.handle(keyEvent{typ: keyTab})
	// "/mod" matches /mode and /model → common prefix "/mod" already; ensure no crash
	// and that a single deeper match would complete. Use a uniquely-prefixed cmd:
	e.buf.setLine("/exi")
	e.handle(keyEvent{typ: keyTab})
	if e.buf.string() != "/exit " {
		t.Fatalf("tab should complete /exi → '/exit ', got %q", e.buf.string())
	}
}

func TestHandlePasteDoesNotSubmit(t *testing.T) {
	e := newTestEditor(t)
	// Enter the bracketed paste, then feed pasted content containing a newline.
	if act := e.handle(keyEvent{typ: keyPasteStart}); act == actSubmit {
		t.Fatal("paste-start must not submit")
	}
	for _, r := range "line1" {
		e.handle(keyEvent{typ: keyRune, r: r})
	}
	// A newline WITHIN a paste is content, not a submit.
	if act := e.handle(keyEvent{typ: keyEnter}); act == actSubmit {
		t.Fatal("a newline inside a paste must NOT submit the line")
	}
	for _, r := range "line2" {
		e.handle(keyEvent{typ: keyRune, r: r})
	}
	e.handle(keyEvent{typ: keyPasteEnd})
	// The multi-line paste flattened into one editable line (newline → space).
	if got := e.buf.string(); got != "line1 line2" {
		t.Fatalf("paste should flatten to one line, got %q", got)
	}
	// A real Enter AFTER the paste submits the whole thing.
	if act := e.handle(keyEvent{typ: keyEnter}); act != actSubmit {
		t.Fatalf("Enter after paste should submit, got %v", act)
	}
}

func TestDrainMultilineCJKPasteSplitRunes(t *testing.T) {
	e := newTestEditor(t)
	// Reproduce the real failure: a terminal delivers the ESC[200~…ESC[201~
	// markers atomically but splits the pasted CJK content across read
	// boundaries — including in the MIDDLE of a 3-byte rune. The old code
	// discarded the partial-rune tail on submit, so the next read began
	// mid-rune and decoded to U+FFFD ("�"), the reported 乱码.
	start := []byte("\x1b[200~")
	end := []byte("\x1b[201~")
	content := []byte("你好\n世界") // 你=3B 好=3B \n=1B 世=3B 界=3B
	// Chunks chosen so a read boundary lands inside 你 (after 2 of its 3 bytes)
	// and inside 世 (after 1 byte). Markers stay whole; no chunk leaves a lone ESC.
	chunks := [][]byte{
		append(append([]byte{}, start...), content[0:2]...), // ESC[200~ + 你[:2]
		content[2:8],                            // 你[2:] 好 \n 世[:1]
		append(append([]byte{}, content[8:]...), append(end, '\r')...), // 世[1:] 界 ESC[201~ \r
	}
	var submitted string
	var submits int
	for _, ch := range chunks {
		e.pending = append(e.pending, ch...)
		if line, act, _ := e.drain(); act == actSubmit {
			submitted = line
			submits++
		}
	}
	if submits != 1 {
		t.Fatalf("a single multi-line paste must submit exactly once, got %d submits", submits)
	}
	if submitted != "你好 世界" {
		t.Fatalf("paste corrupted: got %q want %q", submitted, "你好 世界")
	}
	if strings.ContainsRune(submitted, '�') {
		t.Fatalf("paste contains the replacement char (the reported 乱码): %q", submitted)
	}
}

func TestDrainPreservesTypeahead(t *testing.T) {
	// Two complete lines arriving in one batch: drain returns the first and
	// keeps the rest in e.pending (previously the tail was discarded).
	e := newTestEditor(t)
	e.pending = []byte("one\rtwo\r")
	line, act, _ := e.drain()
	if act != actSubmit || line != "one" {
		t.Fatalf("first drain: got act=%v line=%q", act, line)
	}
	e.buf.clear()
	line, act, _ = e.drain()
	if act != actSubmit || line != "two" {
		t.Fatalf("second drain must recover the buffered line, got act=%v line=%q", act, line)
	}
}

func newTestEditor(t *testing.T) *Editor {
	t.Helper()
	return NewEditor("> ", filepath.Join(t.TempDir(), "hist"), t.TempDir())
}
