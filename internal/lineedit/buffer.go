package lineedit

// buffer is a cursor-aware editable line of runes. The cursor (pos) sits
// between runes: 0 = before the first rune, len = after the last.
type buffer struct {
	runes []rune
	pos   int
}

func (b *buffer) insert(r rune) {
	b.runes = append(b.runes, 0)
	copy(b.runes[b.pos+1:], b.runes[b.pos:])
	b.runes[b.pos] = r
	b.pos++
}

func (b *buffer) insertString(s string) {
	for _, r := range s {
		b.insert(r)
	}
}

func (b *buffer) backspace() bool {
	if b.pos == 0 {
		return false
	}
	b.runes = append(b.runes[:b.pos-1], b.runes[b.pos:]...)
	b.pos--
	return true
}

func (b *buffer) deleteFwd() bool {
	if b.pos >= len(b.runes) {
		return false
	}
	b.runes = append(b.runes[:b.pos], b.runes[b.pos+1:]...)
	return true
}

func (b *buffer) left() bool {
	if b.pos == 0 {
		return false
	}
	b.pos--
	return true
}

func (b *buffer) right() bool {
	if b.pos >= len(b.runes) {
		return false
	}
	b.pos++
	return true
}

func (b *buffer) home() { b.pos = 0 }
func (b *buffer) end()  { b.pos = len(b.runes) }

func (b *buffer) string() string { return string(b.runes) }

func (b *buffer) setLine(s string) {
	b.runes = []rune(s)
	b.pos = len(b.runes)
}

func (b *buffer) clear() {
	b.runes = b.runes[:0]
	b.pos = 0
}
