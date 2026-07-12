package artifact

import (
	"context"
	"lumen/internal/event"
	"lumen/internal/runstate"
	"lumen/internal/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureBindsToolCallAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	ws, _ := workspace.NewLocal("w", root, "u", nil)
	store := NewMemoryStore()
	sink := &CapturingSink{Context: context.Background(), Store: store, Owner: runstate.Owner{UserID: "u", WorkspaceID: "w"}, RunID: "r", Model: "m", Workspace: ws, Next: event.Discard}
	dispatch := event.Event{Kind: event.ToolDispatch, StepID: "step", Tool: event.Tool{ID: "tc", Name: "write_file", Args: `{"path":"out.txt"}`}}
	result := event.Event{Kind: event.ToolResult, EventID: "e", Tool: event.Tool{ID: "tc", Name: "write_file"}}
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(root, "out.txt"), []byte("ok"), 0600); err != nil {
			t.Fatal(err)
		}
		sink.Emit(dispatch)
		sink.Emit(result)
	}
	items, _ := store.ListRun(runstate.Owner{UserID: "u", WorkspaceID: "w"}, "r")
	if len(items) != 1 || items[0].StepID != "step" || items[0].ToolCallID != "tc" || len(items[0].InputRefs) != 1 {
		t.Fatalf("items=%+v", items)
	}
}

func TestCaptureConsumesRealStreamingToolEventShape(t *testing.T) {
	root := t.TempDir()
	ws, _ := workspace.NewLocal("w", root, "u", nil)
	store := NewMemoryStore()
	sink := &CapturingSink{Context: context.Background(), Store: store, Owner: runstate.Owner{UserID: "u", WorkspaceID: "w"}, RunID: "r", Workspace: ws, Next: event.Discard}

	// OpenAI-compatible streams announce the tool first, then the agent emits
	// the finalized call with args, followed by a result carrying stable outer
	// IDs. The initial args-less dispatch must neither lose nor poison capture.
	sink.Emit(event.Event{Kind: event.ToolDispatch, StepID: "tc", ToolCallID: "tc", Tool: event.Tool{ID: "tc", Name: "write_file"}})
	sink.Emit(event.Event{Kind: event.ToolDispatch, StepID: "tc", ToolCallID: "tc", Tool: event.Tool{ID: "tc", Name: "write_file", Args: `{"path":"reports/result.md","content":"durable"}`}})
	if err := os.MkdirAll(filepath.Join(root, "reports"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reports", "result.md"), []byte("durable"), 0600); err != nil {
		t.Fatal(err)
	}
	sink.Emit(event.Event{Kind: event.ToolResult, EventID: "r:3", StepID: "tc", ToolCallID: "tc", Tool: event.Tool{ID: "tc", Name: "write_file", Output: "wrote 7 bytes to reports/result.md"}})

	items, err := store.ListRun(runstate.Owner{UserID: "u", WorkspaceID: "w"}, "r")
	if err != nil || len(items) != 1 {
		t.Fatalf("artifacts=%+v err=%v", items, err)
	}
	if items[0].Path != "reports/result.md" || items[0].StepID != "tc" || items[0].ToolCallID != "tc" || items[0].SHA256 == "" {
		t.Fatalf("artifact=%+v", items[0])
	}
}
