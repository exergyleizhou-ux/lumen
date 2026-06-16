package lineedit

import "unicode/utf8"

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
)

type keyEvent struct {
	typ keyType
	r   rune
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
