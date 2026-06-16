package lineedit

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type keyType int

const (
	keyUnknown keyType = iota
	keyRune
	keyEnter
	keyBackspace
	keyDelete
	keyLeft
	keyRight
	keyUp
	keyDown
	keyHome
	keyEnd
	keyTab
	keyCtrlC
	keyCtrlD
	keyCtrlW
	keyEsc
	keyMouse
)

type keyEvent struct {
	typ keyType
	r   rune
	// Mouse event fields (zero when typ != keyMouse)
	mouseCol int // 0-based column in terminal
	mouseRow int // 0-based row within the editor's rendered area (prompt line = 0)
	mouseBtn int // 0=left, 1=middle, 2=right
}

// decodeKey decodes the first key from b and returns the event plus the number
// of bytes consumed. A consumed count of 0 means b holds an incomplete sequence
// (a partial escape code or partial UTF-8 rune) and the caller should read more.
func decodeKey(b []byte) (keyEvent, int) {
	if len(b) == 0 {
		return keyEvent{typ: keyUnknown}, 0
	}
	c := b[0]
	switch {
	case c == 0x1b: // ESC — control sequence
		// X10 mouse: \x1b[M + 3 bytes (button+32, col+32, row+32)
		// Used by macOS Terminal.app and other terminals that don't support SGR.
		if len(b) >= 3 && b[1] == '[' && b[2] == 'M' {
			if len(b) < 6 {
				return keyEvent{typ: keyUnknown}, 0 // incomplete — wait for 3 payload bytes
			}
			btn := int(b[3]) - 32
			col := int(b[4]) - 32
			row := int(b[5]) - 32
			if btn == 0 && col >= 1 && row >= 1 {
				return keyEvent{typ: keyMouse, mouseCol: col - 1, mouseRow: row - 1, mouseBtn: 0}, 6
			}
			return keyEvent{typ: keyUnknown}, 6
		}
		// SGR mouse: \x1b[<btn;col;rowM (press) or m (release)
		// Used by iTerm2, Kitty, Alacritty, VS Code terminal, etc.
		if len(b) >= 6 && b[1] == '[' && b[2] == '<' {
			// Find the terminating M or m
			end := -1
			for i := 3; i < len(b) && i < 32; i++ {
				if b[i] == 'M' || b[i] == 'm' {
					end = i
					break
				}
			}
			if end < 0 {
				return keyEvent{typ: keyUnknown}, 0 // incomplete
			}
			// Parse btn;col;row
			body := string(b[3:end])
			parts := strings.Split(body, ";")
			if len(parts) >= 3 {
				var btn, col, row int
				fmt.Sscanf(parts[0], "%d", &btn)
				fmt.Sscanf(parts[1], "%d", &col)
				fmt.Sscanf(parts[2], "%d", &row)
				// Only handle left-click press (button 0, M suffix)
				if btn == 0 && b[end] == 'M' && col >= 1 && row >= 1 {
					return keyEvent{typ: keyMouse, mouseCol: col - 1, mouseRow: row - 1, mouseBtn: 0}, end + 1
				}
			}
			return keyEvent{typ: keyUnknown}, end + 1
		}
		if len(b) >= 3 && b[1] == '[' {
			switch b[2] {
			case 'A':
				return keyEvent{typ: keyUp}, 3
			case 'B':
				return keyEvent{typ: keyDown}, 3
			case 'C':
				return keyEvent{typ: keyRight}, 3
			case 'D':
				return keyEvent{typ: keyLeft}, 3
			case 'H':
				return keyEvent{typ: keyHome}, 3
			case 'F':
				return keyEvent{typ: keyEnd}, 3
			case '3':
				if len(b) >= 4 && b[3] == '~' {
					return keyEvent{typ: keyDelete}, 4
				}
				return keyEvent{typ: keyUnknown}, 0 // need the '~'
			}
			return keyEvent{typ: keyUnknown}, 3 // unrecognized CSI — skip it
		}
		if len(b) < 3 {
			return keyEvent{typ: keyUnknown}, 0 // incomplete escape — wait
		}
		return keyEvent{typ: keyUnknown}, 1
	case c == '\r' || c == '\n':
		return keyEvent{typ: keyEnter}, 1
	case c == 0x7f || c == 0x08:
		return keyEvent{typ: keyBackspace}, 1
	case c == '\t':
		return keyEvent{typ: keyTab}, 1
	case c == 0x03:
		return keyEvent{typ: keyCtrlC}, 1
	case c == 0x04:
		return keyEvent{typ: keyCtrlD}, 1
	case c == 0x17:
		return keyEvent{typ: keyCtrlW}, 1 // Ctrl-W: delete word backwards
	case c == 0x01:
		return keyEvent{typ: keyHome}, 1 // Ctrl-A
	case c == 0x05:
		return keyEvent{typ: keyEnd}, 1 // Ctrl-E
	case c < 0x20:
		return keyEvent{typ: keyUnknown}, 1
	default:
		if !utf8.FullRune(b) {
			return keyEvent{typ: keyUnknown}, 0 // partial rune — wait
		}
		r, size := utf8.DecodeRune(b)
		return keyEvent{typ: keyRune, r: r}, size
	}
}
