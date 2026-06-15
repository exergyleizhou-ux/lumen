package timeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/event"
)

func TestRecorderNewTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	r.NewTurn()
	r.Record(Entry{Kind: "phase", Detail: "test"})

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "phase") {
		t.Error("timeline should contain recorded entry")
	}
}

func TestRecorderRecordEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tl.jsonl")
	r, _ := NewRecorder(path)
	defer r.Close()

	r.RecordEvent(event.Event{
		Kind: event.ToolDispatch,
		Tool: event.Tool{ID: "1", Name: "bash", Args: `{"command":"echo hi"}`},
	})
	r.RecordEvent(event.Event{
		Kind: event.ToolResult,
		Tool: event.Tool{ID: "1", Name: "bash", Output: "hi", Err: ""},
	})

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(lines))
	}
}

func TestLoadChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tl2.jsonl")
	r, _ := NewRecorder(path)
	defer r.Close()

	r.Record(Entry{
		Turn:    1,
		Kind:    "tool_result",
		ToolName: "write_file",
		Detail:  "wrote ok",
		Success:  true,
		Path:    "/tmp/test.go",
		Timestamp: time.Now(),
	})

	changes, err := LoadChanges(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(changes))
	}
	if changes[0].Path != "/tmp/test.go" {
		t.Errorf("path: want /tmp/test.go, got %s", changes[0].Path)
	}
}

func TestFormatTimeline(t *testing.T) {
	entries := []Entry{
		{Turn: 1, Kind: "phase", Detail: "executing"},
		{Turn: 1, Kind: "tool_dispatch", ToolName: "bash", Detail: "echo hi"},
		{Turn: 1, Kind: "tool_result", ToolName: "bash", Detail: "hi", Success: true},
	}
	formatted := FormatTimeline(entries)
	if !strings.Contains(formatted, "echo hi") {
		t.Error("formatted timeline should contain tool details")
	}
}

func TestFormatChanges(t *testing.T) {
	changes := []ChangedFile{
		{Path: "/tmp/a.go", Operations: []string{"write_file"}, Turns: []int{1}},
		{Path: "/tmp/b.go", Operations: []string{"edit_file", "write_file"}, Turns: []int{1, 3}},
	}
	formatted := FormatChanges(changes)
	if !strings.Contains(formatted, "a.go") || !strings.Contains(formatted, "b.go") {
		t.Error("format changes should list all files")
	}
}

func TestLoadTimelineEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.jsonl")
	_, err := LoadTimeline(path)
	if err == nil {
		t.Error("should error on missing file")
	}
}
