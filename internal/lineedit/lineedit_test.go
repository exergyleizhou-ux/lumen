package lineedit

import (
	"os"
	"path/filepath"
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

func newTestEditor(t *testing.T) *Editor {
	t.Helper()
	return NewEditor("> ", filepath.Join(t.TempDir(), "hist"), t.TempDir())
}
