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
	// Must contain \r and \x1b[J (clear to end of screen for multi-line safety)
	if !strings.Contains(result, "\r\x1b[J") {
		t.Fatal("redraw missing clear-screen sequence")
	}
	// Cursor positioning: \r then \x1b[5C (> + hel = 5 cols)
	if !strings.Contains(result, "\r\x1b[5C") {
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

	// Must contain \x1b[J (clear to end of screen — covers all old rows)
	if !strings.Contains(result, "\x1b[J") {
		t.Fatalf("multi-line redraw missing clear-screen: %q", result)
	}
	// Must contain \x1b[1A (move up 1 row to cover previous 2-row draw)
	if !strings.Contains(result, "\x1b[1A") {
		t.Fatalf("multi-line redraw missing move-up: %q", result)
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

	// Click at column 3: prompt "▸ " = 2 cols, so buffer col = 1
	// "hello" → col 0='h', col 1='e' → click at col 1 = after 'h', pos should be 1
	ev := keyEvent{typ: keyMouse, mouseCol: 3, mouseBtn: 0}
	e.handle(ev)
	if e.buf.pos != 1 {
		t.Fatalf("mouse at col 3: pos=%d, want 1", e.buf.pos)
	}

	// Click past end → cursor should go to end
	ev = keyEvent{typ: keyMouse, mouseCol: 80, mouseBtn: 0}
	e.handle(ev)
	if e.buf.pos != len(e.buf.runes) {
		t.Fatalf("mouse past end: pos=%d, want %d", e.buf.pos, len(e.buf.runes))
	}

	// Click on prompt area (col 0) → cursor should go home
	ev = keyEvent{typ: keyMouse, mouseCol: 0, mouseBtn: 0}
	e.handle(ev)
	if e.buf.pos != 0 {
		t.Fatalf("mouse on prompt: pos=%d, want 0", e.buf.pos)
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

	// Incomplete X10 (only 3 bytes) — should wait for more
	ev2, consumed2 := decodeKey([]byte{0x1b, '[', 'M'})
	if consumed2 != 0 {
		t.Errorf("incomplete X10: consumed=%d, want 0", consumed2)
	}
	_ = ev2
}
