package terminal

import "testing"

func TestStripANSI(t *testing.T) {
	input := "\x1b[32mgreen\x1b[0m text"
	got := StripANSI(input)
	if got != "green text" {
		t.Errorf("StripANSI: got %q", got)
	}
}

func TestDetectTermSize(t *testing.T) {
	cols, rows := DetectTermSize()
	t.Logf("term size: %dx%d", cols, rows)
	if cols <= 0 || rows <= 0 {
		t.Error("should return positive dimensions")
	}
}

func TestIsTerminal(t *testing.T) {
	result := IsTerminal()
	t.Logf("is terminal: %v", result)
}

func TestDisplayWidth(t *testing.T) {
	if w := DisplayWidth("hello"); w != 5 {
		t.Errorf("ascii: got %d", w)
	}
	if w := DisplayWidth("你好"); w != 4 {
		t.Errorf("cjk: got %d", w)
	}
}

func TestTruncateToWidth(t *testing.T) {
	if s := TruncateToWidth("hello world", 5); len(s) > 5 {
		t.Errorf("truncate: %q", s)
	}
}

func TestBox(t *testing.T) {
	out := Box("Title", "Content line")
	if out == "" {
		t.Error("Box should return non-empty")
	}
}

func TestProgressBar(t *testing.T) {
	bar := ProgressBar(50, 100, 20)
	if bar == "" {
		t.Error("ProgressBar should return non-empty")
	}
}

func TestHeader(t *testing.T) {
	h := Header("Section")
	if h == "" {
		t.Error("Header should return non-empty")
	}
}

func TestTable(t *testing.T) {
	tbl := Table([]string{"Name", "Value"}, [][]string{{"foo", "bar"}})
	if tbl == "" {
		t.Error("Table should return non-empty")
	}
}

func TestCountLines(t *testing.T) {
	if CountLines("a\nb\nc") != 3 {
		t.Error("count lines mismatch")
	}
	if CountLines("") != 0 {
		t.Error("empty should be 0")
	}
}

func TestFirstLine(t *testing.T) {
	if s := FirstLine("  hello\nworld"); s != "hello" {
		t.Errorf("FirstLine: got %q", s)
	}
}

func TestColor(t *testing.T) {
	s := Color(GreenFG, "ok")
	if s == "" {
		t.Error("Color should return non-empty")
	}
}

func TestBoldText(t *testing.T) {
	s := BoldText("important")
	if s == "" {
		t.Error("BoldText should return non-empty")
	}
}
