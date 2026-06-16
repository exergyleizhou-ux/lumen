package render

import (
	"io"
	"strings"
)

// Stream renders streamed markdown line-by-line so prose appears live (with
// inline styling) while fenced code blocks are held until their closing fence
// and emitted syntax-highlighted. This keeps the token-streaming "feel" without
// sacrificing rich rendering.
//
// Feed model text via Write (any chunking is fine — lines are reassembled).
// Call Flush at a block boundary (before a tool call, or at turn end) to emit
// any buffered partial line.
type Stream struct {
	w   io.Writer
	buf strings.Builder // pending bytes not yet terminated by a newline

	// Indent is prefixed to every emitted line (e.g. "  " for the chat gutter).
	Indent string

	inCode   bool
	codeLang string
	code     []string
}

// NewStream creates a Stream writing to w.
func NewStream(w io.Writer) *Stream { return &Stream{w: w} }

// Write feeds a streamed text chunk. Completed lines are rendered immediately;
// any trailing partial line is buffered until the next Write or Flush.
func (s *Stream) Write(text string) {
	s.buf.WriteString(text)
	content := s.buf.String()
	for {
		nl := strings.IndexByte(content, '\n')
		if nl < 0 {
			break
		}
		s.line(content[:nl])
		content = content[nl+1:]
	}
	s.buf.Reset()
	s.buf.WriteString(content)
}

// Flush emits any buffered partial line. Use at a block boundary.
func (s *Stream) Flush() {
	rem := s.buf.String()
	s.buf.Reset()
	if s.inCode {
		if rem != "" {
			s.code = append(s.code, rem)
		}
		if len(s.code) > 0 {
			s.emit(renderCodeBlock(strings.Join(s.code, "\n"), s.codeLang))
		}
		s.inCode = false
		s.code = nil
		return
	}
	if rem != "" {
		s.emit(renderLine(rem))
	}
}

func (s *Stream) line(line string) {
	if strings.HasPrefix(strings.TrimSpace(line), "```") {
		if !s.inCode {
			s.inCode = true
			s.codeLang = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "```"))
			s.code = nil
		} else {
			s.emit(renderCodeBlock(strings.Join(s.code, "\n"), s.codeLang))
			s.inCode = false
			s.code = nil
		}
		return
	}
	if s.inCode {
		s.code = append(s.code, line)
		return
	}
	s.emit(renderLine(line))
}

// emit writes a (possibly multi-line) rendered fragment, prefixing each line
// with Indent.
func (s *Stream) emit(rendered string) {
	for _, ln := range strings.Split(rendered, "\n") {
		io.WriteString(s.w, s.Indent+ln+"\n")
	}
}
