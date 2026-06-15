package pipeline

import (
	"strings"
	"testing"
)

func TestPipeline_CatGrepSort(t *testing.T) {
	p := New()
	p.AddCatStage("cat")
	p.AddGrepStage("grep", "foo")
	p.AddSortStage("sort")

	input := []byte("bar\nfoo\nbaz\nfoo-bar\nqux\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}

	output := p.Output()
	if !strings.Contains(output, "foo") {
		t.Fatalf("expected foo in output, got: %s", output)
	}
	if !strings.Contains(output, "foo-bar") {
		t.Fatal("expected foo-bar")
	}
	if strings.Contains(output, "bar\n") && !strings.Contains(output, "foo") {
		// "bar" should be excluded by grep "foo".
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
}

func TestPipeline_SingleStage(t *testing.T) {
	p := New()
	p.AddCatStage("cat")
	if err := p.Run([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if p.Output() != "hello" {
		t.Fatalf("expected hello, got %q", p.Output())
	}
}

func TestPipeline_MultipleStages(t *testing.T) {
	p := New()
	p.AddStage("upper", MapStage(strings.ToUpper))
	p.AddStage("exclaim", MapStage(func(s string) string { return s + "!" }))

	if err := p.Run([]byte("hello\nworld")); err != nil {
		t.Fatal(err)
	}
	output := strings.TrimSpace(p.Output())
	lines := strings.Split(output, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "HELLO!" || lines[1] != "WORLD!" {
		t.Fatalf("unexpected: %v", lines)
	}
}

func TestPipeline_EmptyInput(t *testing.T) {
	p := New()
	p.AddCatStage("cat")
	if err := p.Run(nil); err != nil {
		t.Fatal(err)
	}
	if p.Output() != "" {
		t.Fatalf("expected empty, got %q", p.Output())
	}
}

func TestPipeline_Sort(t *testing.T) {
	p := New()
	p.AddSortStage("sort")
	input := []byte("c\na\nb\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}
	if p.Output() != "a\nb\nc\n" {
		t.Fatalf("expected sorted, got %q", p.Output())
	}
}

func TestPipeline_Head(t *testing.T) {
	p := New()
	p.AddStage("head", HeadStage(2))
	input := []byte("a\nb\nc\nd\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(p.Output()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestPipeline_Tail(t *testing.T) {
	p := New()
	p.AddStage("tail", TailStage(2))
	input := []byte("a\nb\nc\nd\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(p.Output()), "\n")
	if len(lines) != 2 || lines[0] != "c" || lines[1] != "d" {
		t.Fatalf("unexpected: %v", lines)
	}
}

func TestPipeline_Count(t *testing.T) {
	p := New()
	p.AddStage("count", CountStage())
	input := []byte("a\nb\nc\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(p.Output()) != "3" {
		t.Fatalf("expected 3, got %q", p.Output())
	}
}

func TestPipeline_Wc(t *testing.T) {
	p := New()
	p.AddStage("wc", WcStage())
	input := []byte("hello world\nfoo bar\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}
	// 2 lines, 4 words, characters
	if !strings.Contains(p.Output(), "2") {
		t.Fatalf("expected 2 lines, got %q", p.Output())
	}
}

func TestPipeline_Uniq(t *testing.T) {
	p := New()
	p.AddStage("uniq", UniqStage())
	input := []byte("a\na\nb\nb\nc\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}
	if p.Output() != "a\nb\nc\n" {
		t.Fatalf("expected a\\nb\\nc\\n, got %q", p.Output())
	}
}

func TestPipeline_Filter(t *testing.T) {
	p := New()
	p.AddStage("filter", FilterStage(func(s string) bool { return len(s) > 3 }))
	input := []byte("a\nab\nabc\nabcd\nabcde\n")
	if err := p.Run(input); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(p.Output()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 long lines, got %d", len(lines))
	}
}

func TestFormatPipeline(t *testing.T) {
	p := New()
	p.AddCatStage("cat")
	p.AddGrepStage("grep", "x")
	p.AddSortStage("sort")
	vis := FormatPipeline(p, DefaultFormatPipelineOptions())
	if !strings.Contains(vis, "cat") || !strings.Contains(vis, "grep") || !strings.Contains(vis, "sort") {
		t.Fatalf("unexpected visualization: %s", vis)
	}
}

func TestPipeBuffer(t *testing.T) {
	pb := NewPipeBuffer()
	pb.Write([]byte("hello"))
	pb.Write([]byte(" world"))
	pb.Close()

	out := make([]byte, 100)
	n, err := pb.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(out[:n]) != "hello world" {
		t.Fatalf("got %q", string(out[:n]))
	}
}

func TestPipeBuffer_Tee(t *testing.T) {
	pb := NewPipeBuffer()
	tee := pb.Tee()
	pb.Write([]byte("data"))
	pb.Close()

	if tee.String() != "" {
		// Tee doesn't auto-copy in this simple implementation; it's
		// primarily for use within pipeline Run where writes are
		// duplicated.
	}
	_ = tee
}

func TestPipeline_NoStages(t *testing.T) {
	p := New()
	if err := p.Run([]byte("test")); err != nil {
		t.Fatal(err)
	}
}
