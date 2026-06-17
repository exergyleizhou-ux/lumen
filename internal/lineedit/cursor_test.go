package lineedit

import (
	"bytes"
	"strings"
	"testing"
)

func TestLeftArrow(t *testing.T) {
	// Simulate: type "ab", left arrow, type "x" → should produce "axb"
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	
	// Type "ab"
	for _, r := range "ab" {
		e.buf.insert(r)
	}
	if s := e.buf.string(); s != "ab" {
		t.Fatalf("after typing ab: got %q, want %q", s, "ab")
	}
	
	// Left arrow
	ev, _ := decodeKey([]byte("\x1b[D"))
	if ev.typ != keyLeft { t.Fatalf("left arrow not decoded: %v", ev) }
	e.handle(ev)
	if e.buf.pos != 1 { t.Fatalf("after left: pos=%d, want 1", e.buf.pos) }
	
	// Type "x"
	e.buf.insert('x')
	if s := e.buf.string(); s != "axb" {
		t.Fatalf("after insert x at pos 1: got %q, want %q", s, "axb")
	}
}

func TestArrowKeyRoundTrip(t *testing.T) {
	// Verify all arrow/home/end keys decode and handle correctly
	tests := []struct {
		name  string
		bytes []byte
		typ   keyType
	}{
		{"Up", []byte("\x1b[A"), keyUp},
		{"Down", []byte("\x1b[B"), keyDown},
		{"Right", []byte("\x1b[C"), keyRight},
		{"Left", []byte("\x1b[D"), keyLeft},
		{"Home (CSI)", []byte("\x1b[H"), keyHome},
		{"End (CSI)", []byte("\x1b[F"), keyEnd},
		{"Delete", []byte("\x1b[3~"), keyDelete},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, consumed := decodeKey(tt.bytes)
			if consumed != len(tt.bytes) {
				t.Errorf("consumed=%d, want %d", consumed, len(tt.bytes))
			}
			if ev.typ != tt.typ {
				t.Errorf("typ=%v, want %v", ev.typ, tt.typ)
			}
		})
	}
}

func TestCursorMovementWithChinese(t *testing.T) {
	// 你好 = 2 Chinese chars
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	
	for _, r := range "你好" {
		e.buf.insert(r)
	}
	if e.buf.string() != "你好" { t.Fatal("chinese insert failed") }
	if e.buf.pos != 2 { t.Fatalf("pos=%d, want 2", e.buf.pos) }
	
	// Move left by 1
	e.buf.left()
	if e.buf.pos != 1 { t.Fatalf("left: pos=%d, want 1", e.buf.pos) }
	
	// Insert 'x' between 你 and 好
	e.buf.insert('x')
	if e.buf.string() != "你x好" { t.Fatalf("got %q, want 你x好", e.buf.string()) }
}

func TestRedrawColPositioning(t *testing.T) {
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	for _, r := range "hello" {
		e.buf.insert(r)
	}
	e.buf.left()
	e.buf.left() // cursor between 'l' and 'l' in "hello" — pos=3

	var out bytes.Buffer
	e.out = &out
	e.redraw()

	result := out.String()

	// Full "hello" must be contiguous in output (no split)
	if !strings.Contains(result, "hello") {
		t.Fatalf("redraw output missing 'hello': %q", result)
	}
	// Must contain \x1b7 (save cursor) and \x1b8 (restore cursor)
	if !strings.Contains(result, "\x1b7") {
		t.Fatal("redraw missing save-cursor sequence")
	}
	if !strings.Contains(result, "\x1b8") {
		t.Fatal("redraw missing restore-cursor sequence")
	}
	// Cursor positioning: \x1b[5C (>  = 2 + hel = 3 more = 5)
	if !strings.Contains(result, "\x1b[5C") {
		t.Fatalf("redraw missing cursor positioning: %q", result)
	}
}

func TestRedrawCJKCursorPosition(t *testing.T) {
	e := NewEditor("▸ ", "", ".")
	e.buf.clear()
	// 你好 = 2 CJK chars, 4 display columns
	for _, r := range "你好" {
		e.buf.insert(r)
	}
	// Move left by 1 rune → cursor after 你 (pos=1, display col = prompt_width + 你_cols = 0 + 2 = 2)
	e.buf.left()

	var out bytes.Buffer
	e.out = &out
	e.redraw()

	result := out.String()

	if !strings.Contains(result, "你好") {
		t.Fatalf("redraw output missing CJK: %q", result)
	}
	// ▸  = 2 cols (▸ + space), 你 = 2 cols → cursor at col 4
	if !strings.Contains(result, "\x1b[4C") {
		t.Fatalf("CJK cursor not at expected position: %q", result)
	}
}

func TestRedrawMultiLineClearsPreviousRows(t *testing.T) {
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	// Insert enough text to span 2 terminal rows (80-col terminal)
	longText := strings.Repeat("abcdefghij", 10) // 100 chars
	for _, r := range longText {
		e.buf.insert(r)
	}
	e.lastRows = 2 // simulate previous redraw occupied 2 rows

	var out bytes.Buffer
	e.out = &out
	e.redraw()

	result := out.String()

	// Must contain \x1b[J (clear from saved position to end of screen)
	if !strings.Contains(result, "\x1b[J") {
		t.Fatalf("multi-line redraw missing \x1b[J: %q", result)
	}
	// Must contain \x1b7 and \x1b8 (save/restore cursor)
	if !strings.Contains(result, "\x1b7") {
		t.Fatal("multi-line redraw missing save-cursor")
	}
	if !strings.Contains(result, "\x1b8") {
		t.Fatal("multi-line redraw missing restore-cursor")
	}
	// Must still contain the full text
	if !strings.Contains(result, longText) {
		t.Fatal("multi-line redraw lost text content")
	}
	// lastRows should be updated to the new row count (100+2=102 cols / 80 = 2 rows)
	if e.lastRows < 2 {
		t.Errorf("lastRows after redraw = %d, want >= 2", e.lastRows)
	}
}

func TestMouseClickRepositionsCursor(t *testing.T) {
	e := NewEditor("▸ ", "", ".")
	e.buf.clear()
	for _, r := range "hello" {
		e.buf.insert(r)
	}
	e.buf.home() // cursor at 0

	// Click at (row=0, col=3): prompt "▸ " = 2 cols, so text col = 1
	// text "hello" → col 0='h', col 1='e' → pos should be 1
	ev := keyEvent{typ: keyMouse, mouseCol: 3, mouseRow: 0, mouseBtn: 0}
	e.handle(ev)
	if e.buf.pos != 1 {
		t.Fatalf("mouse at (0,3): pos=%d, want 1", e.buf.pos)
	}

	// Click past end → cursor should go to end
	ev = keyEvent{typ: keyMouse, mouseCol: 80, mouseRow: 1, mouseBtn: 0}
	e.handle(ev)
	if e.buf.pos != len(e.buf.runes) {
		t.Fatalf("mouse past end: pos=%d, want %d", e.buf.pos, len(e.buf.runes))
	}

	// Click before prompt (col 0, row 0) → cursor should go home
	ev = keyEvent{typ: keyMouse, mouseCol: 0, mouseRow: 0, mouseBtn: 0}
	e.handle(ev)
	if e.buf.pos != 0 {
		t.Fatalf("mouse before prompt: pos=%d, want 0", e.buf.pos)
	}
}

func TestDecodeSGRMouse(t *testing.T) {
	// SGR left-click press at column 10, row 1
	ev, consumed := decodeKey([]byte("\x1b[<0;10;1M"))
	if consumed != 10 {
		t.Errorf("consumed=%d, want 10", consumed)
	}
	if ev.typ != keyMouse {
		t.Fatalf("typ=%v, want keyMouse", ev.typ)
	}
	if ev.mouseCol != 9 { // 0-based
		t.Errorf("mouseCol=%d, want 9", ev.mouseCol)
	}
	if ev.mouseRow != 0 { // 0-based: row 1 → 0
		t.Errorf("mouseRow=%d, want 0", ev.mouseRow)
	}

	// SGR release — should be ignored (not mouse)
	ev2, _ := decodeKey([]byte("\x1b[<0;10;1m"))
	if ev2.typ == keyMouse {
		t.Fatal("mouse release should not be keyMouse")
	}
}

func TestDecodeX10Mouse(t *testing.T) {
	// X10 left-click at column 10, row 5
	// button=0 → byte=32, col=10 → byte=42, row=5 → byte=37
	ev, consumed := decodeKey([]byte{0x1b, '[', 'M', 32, 42, 37})
	if consumed != 6 {
		t.Errorf("consumed=%d, want 6", consumed)
	}
	if ev.typ != keyMouse {
		t.Fatalf("typ=%v, want keyMouse", ev.typ)
	}
	if ev.mouseCol != 9 { // col 10 → 0-based 9
		t.Errorf("mouseCol=%d, want 9", ev.mouseCol)
	}
	if ev.mouseRow != 4 { // row 5 → 0-based 4
		t.Errorf("mouseRow=%d, want 4", ev.mouseRow)
	}

	// Incomplete X10 (only 3 bytes) — should wait for more
	ev2, consumed2 := decodeKey([]byte{0x1b, '[', 'M'})
	if consumed2 != 0 {
		t.Errorf("incomplete X10: consumed=%d, want 0", consumed2)
	}
	_ = ev2
}

// TestStressMultiLineTypeDelete simulates a real typing session:
// type a lot → text wraps → backspace some → insert in middle → arrow around
func TestStressMultiLineTypeDelete(t *testing.T) {
	e := NewEditor("▸ ", "", ".")
	e.buf.clear()
	var out bytes.Buffer
	e.out = &out

	// Phase 1: Type 3 lines of text
	long := strings.Repeat("abc", 50) // 150 chars → ~3 lines at 80 cols
	for _, r := range long {
		e.buf.insert(r)
	}
	if e.buf.pos != len([]rune(long)) {
		t.Fatalf("after typing: pos=%d want %d", e.buf.pos, len([]rune(long)))
	}
	e.redraw()
	result := out.String()
	if !strings.Contains(result, long) {
		t.Fatal("typing output lost text")
	}
	// Must contain \x1b[J (clear) and \x1b7/\x1b8 (save/restore)
	if !strings.Contains(result, "\x1b[J") {
		t.Fatal("multi-line type missing \x1b[J")
	}

	// Phase 2: Backspace 10 times — should not leave ghost text
	out.Reset()
	for i := 0; i < 10; i++ {
		e.buf.backspace()
	}
	e.redraw()
	result2 := out.String()
	expected := long[:len([]rune(long))-10]
	if !strings.Contains(result2, expected) {
		t.Fatal("backspace output lost text")
	}
	// Verify old longer text is NOT in the output
	if strings.Contains(result2, long) {
		t.Fatal("backspace left ghost text from previous render")
	}

	// Phase 3: Move cursor to middle, insert 5 chars
	out.Reset()
	for i := 0; i < 50; i++ {
		e.buf.left()
	}
	for _, r := range "INSERT" {
		e.buf.insert(r)
	}
	e.redraw()
	result3 := out.String()
	if !strings.Contains(result3, "INSERT") {
		t.Fatal("insert-in-middle output lost INSERT text")
	}

	// Phase 4: Delete some chars forward
	out.Reset()
	for i := 0; i < 3; i++ {
		e.buf.deleteFwd()
	}
	e.redraw()
	result4 := out.String()
	// Must still be valid text
	if strings.Count(result4, "▸ ") > 1 {
		t.Fatal("redraw duplicated prompt")
	}

	// Phase 5: Clear and type a short line — must not leave old multi-line ghost
	out.Reset()
	e.buf.clear()
	e.buf.insertString("short")
	e.redraw()
	result5 := out.String()
	if strings.Contains(result5, "abc") {
		t.Fatal("after clear, old multi-line text still visible")
	}
	if !strings.Contains(result5, "short") {
		t.Fatal("after clear, new text not shown")
	}
}

// TestShrinkMultiLine verifies that when text shrinks from 3 lines to 1,
// old ghost lines are cleared (the key scenario \x1b[J fixes).
func TestShrinkMultiLine(t *testing.T) {
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	var out bytes.Buffer
	e.out = &out

	// Phase 1: 3 lines of text
	e.buf.insertString(strings.Repeat("x", 200)) // ~3 lines at 80 cols
	e.redraw()
	out.Reset()

	// Phase 2: shrink to 1 line
	e.buf.clear()
	e.buf.insertString("hi")
	e.redraw()
	result := out.String()

	// Must contain "hi"
	if !strings.Contains(result, "hi") {
		t.Fatal("shrink lost new text")
	}
	// Must NOT contain "xxx" from previous render
	if strings.Contains(result, "xxx") {
		t.Fatalf("shrink left ghost text: %q", result)
	}
	// Must contain \x1b[J (the fix)
	if !strings.Contains(result, "\x1b[J") {
		t.Fatal("shrink missing clear-screen")
	}
}

// TestGrowMultiLine verifies that when text grows from 1 line to 3,
// the entire new text is visible and no truncation happens.
func TestGrowMultiLine(t *testing.T) {
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	var out bytes.Buffer
	e.out = &out

	// Phase 1: 1 line
	e.buf.insertString("short")
	e.redraw()
	out.Reset()

	// Phase 2: grow to 3 lines
	e.buf.clear()
	longer := strings.Repeat("abcdefghij", 20) // 200 chars
	e.buf.insertString(longer)
	e.redraw()
	result := out.String()

	if !strings.Contains(result, longer) {
		t.Fatal("grow lost text content")
	}
	if !strings.Contains(result, "\x1b[J") {
		t.Fatal("grow missing clear-screen")
	}
}

func TestWordBackspace(t *testing.T) {
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	e.buf.insertString("hello world test")
	e.buf.end()

	// Ctrl+W once → "hello world "
	e.wordBackspace()
	if e.buf.string() != "hello world " {
		t.Fatalf("after wordBackspace: got %q, want %q", e.buf.string(), "hello world ")
	}

	// Ctrl+W again → "hello "
	e.wordBackspace()
	if e.buf.string() != "hello " {
		t.Fatalf("after 2nd wordBackspace: got %q, want %q", e.buf.string(), "hello ")
	}
}

func TestCtrlWKeyDecode(t *testing.T) {
	ev, consumed := decodeKey([]byte{0x17})
	if ev.typ != keyCtrlW {
		t.Fatalf("0x17 typ=%v want keyCtrlW", ev.typ)
	}
	if consumed != 1 {
		t.Errorf("consumed=%d want 1", consumed)
	}
}

func TestCtrlKKeyDecode(t *testing.T) {
	ev, consumed := decodeKey([]byte{0x0b})
	if ev.typ != keyCtrlK {
		t.Fatalf("0x0b typ=%v want keyCtrlK", ev.typ)
	}
	if consumed != 1 {
		t.Errorf("consumed=%d want 1", consumed)
	}
}

func TestEscClearsBuffer(t *testing.T) {
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	e.buf.insertString("some text")

	ev := keyEvent{typ: keyEsc}
	if act := e.handle(ev); act != actRedraw {
		t.Fatalf("ESC action=%v want actRedraw", act)
	}
	if e.buf.string() != "" {
		t.Fatalf("ESC should clear buffer, got %q", e.buf.string())
	}

	// ESC on empty buffer → no-op, still actRedraw
	if act := e.handle(ev); act != actRedraw {
		t.Fatalf("ESC on empty action=%v want actRedraw", act)
	}
}

func TestMultiRowCursorRowCol(t *testing.T) {
	e := NewEditor("> ", "", ".")
	e.buf.clear()
	// Enough text that cursor wraps to row 1 at 80 cols
	long := strings.Repeat("x", 100) // prompt=2 + 100 = 102 cols → row 1 at col 22
	e.buf.insertString(long)
	e.buf.end() // cursor at end, pos=100

	var out bytes.Buffer
	e.out = &out
	e.redraw()
	result := out.String()

	// Cursor at end of 100 x's: prompt=2 cols, text=100 cols → total=102
	// At 80-col terminal: row=1 (102/80=1), col=22 (102%80=22)
	// So we need \x1b[1B\x1b[22C after \x1b8
	if !strings.Contains(result, "\x1b[1B") || !strings.Contains(result, "\x1b[22C") {
		if !strings.Contains(result, "\x1b[1B") {
			t.Errorf("multi-row cursor missing row-down: %q", result)
		}
		if !strings.Contains(result, "\x1b[22C") {
			t.Errorf("multi-row cursor missing col-right: %q", result)
		}
	}
}

// ── Ctrl-K (kill to end of line) ──────────────────────────

func TestBufferKillToEndMiddle(t *testing.T) {
	var b buffer
	b.insertString("hello world")
	b.pos = 5 // cursor after "hello"
	got := b.killToEnd()
	if got != " world" {
		t.Fatalf("killToEnd at pos 5: got %q, want %q", got, " world")
	}
	if b.string() != "hello" {
		t.Fatalf("buffer after killToEnd: got %q, want %q", b.string(), "hello")
	}
	if b.pos != 5 {
		t.Fatalf("pos after killToEnd: %d, want 5", b.pos)
	}
}

func TestBufferKillToEndAtStart(t *testing.T) {
	var b buffer
	b.insertString("hello world")
	b.home()
	got := b.killToEnd()
	if got != "hello world" {
		t.Fatalf("killToEnd at start: got %q, want %q", got, "hello world")
	}
	if b.string() != "" {
		t.Fatalf("buffer after killToEnd at start: got %q, want empty", b.string())
	}
	if b.pos != 0 {
		t.Fatalf("pos after killToEnd at start: %d, want 0", b.pos)
	}
}

func TestBufferKillToEndAtEnd(t *testing.T) {
	var b buffer
	b.insertString("hello")
	b.end()
	got := b.killToEnd()
	if got != "" {
		t.Fatalf("killToEnd at end: got %q, want empty", got)
	}
	if b.string() != "hello" {
		t.Fatalf("buffer unchanged after killToEnd at end: got %q", b.string())
	}
}

func TestBufferKillToEndCJK(t *testing.T) {
	var b buffer
	b.insertString("你好世界")
	b.pos = 2 // cursor after "你好"
	got := b.killToEnd()
	if got != "世界" {
		t.Fatalf("killToEnd CJK: got %q, want %q", got, "世界")
	}
	if b.string() != "你好" {
		t.Fatalf("buffer after killToEnd CJK: got %q, want %q", b.string(), "你好")
	}
}

func TestHandleCtrlKKillToEnd(t *testing.T) {
	e := newTestEditor(t)
	e.buf.insertString("hello world")
	e.buf.pos = 5 // cursor after "hello"

	ev := keyEvent{typ: keyCtrlK}
	if act := e.handle(ev); act != actRedraw {
		t.Fatalf("Ctrl-K action=%v want actRedraw", act)
	}
	if e.buf.string() != "hello" {
		t.Fatalf("buffer after Ctrl-K: got %q, want %q", e.buf.string(), "hello")
	}
	if e.buf.pos != 5 {
		t.Fatalf("pos after Ctrl-K: %d, want 5", e.buf.pos)
	}
}

func TestHandleCtrlKAtEndIsNoop(t *testing.T) {
	e := newTestEditor(t)
	e.buf.insertString("hello")
	e.buf.end()

	ev := keyEvent{typ: keyCtrlK}
	act := e.handle(ev)
	if act != actRedraw {
		t.Fatalf("Ctrl-K at end action=%v want actRedraw", act)
	}
	if e.buf.string() != "hello" {
		t.Fatalf("buffer unchanged after Ctrl-K at end: got %q", e.buf.string())
	}
}
