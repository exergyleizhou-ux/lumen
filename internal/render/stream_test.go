package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestStreamRendersCompletedProseLine(t *testing.T) {
	var b bytes.Buffer
	s := NewStream(&b)
	s.Write("hello **world**\n")
	out := b.String()
	if !strings.Contains(plain(out), "hello world") {
		t.Fatalf("missing text: %q", plain(out))
	}
	if strings.Contains(out, "**") {
		t.Fatal("bold markers should be gone")
	}
	if !hasANSI(out) {
		t.Fatal("expected styling")
	}
}

func TestStreamBuffersPartialLineUntilFlush(t *testing.T) {
	var b bytes.Buffer
	s := NewStream(&b)
	s.Write("partial line, no newline")
	if b.Len() != 0 {
		t.Fatalf("partial line should be buffered, got %q", b.String())
	}
	s.Flush()
	if !strings.Contains(plain(b.String()), "partial line, no newline") {
		t.Fatalf("flush did not emit partial line: %q", b.String())
	}
}

func TestStreamReassemblesLineSplitAcrossChunks(t *testing.T) {
	var b bytes.Buffer
	s := NewStream(&b)
	s.Write("foo ")
	s.Write("bar\n")
	if !strings.Contains(plain(b.String()), "foo bar") {
		t.Fatalf("split line not reassembled: %q", plain(b.String()))
	}
}

func TestStreamHighlightsFencedCodeBlockOnClose(t *testing.T) {
	var b bytes.Buffer
	s := NewStream(&b)
	s.Write("```go\n")
	if strings.Contains(plain(b.String()), "func") {
		t.Fatal("code should not emit until the closing fence")
	}
	s.Write("func main() {}\n")
	s.Write("```\n")
	out := plain(b.String())
	if !strings.Contains(out, "func main() {}") {
		t.Fatalf("code content missing: %q", out)
	}
	if strings.Contains(out, "```") {
		t.Fatal("fences should not be shown")
	}
	if !hasANSI(b.String()) {
		t.Fatal("expected syntax highlighting")
	}
}

func TestStreamAppliesIndent(t *testing.T) {
	var b bytes.Buffer
	s := NewStream(&b)
	s.Indent = "  "
	s.Write("line one\n")
	if !strings.HasPrefix(b.String(), "  ") {
		t.Fatalf("indent not applied: %q", b.String())
	}
}
