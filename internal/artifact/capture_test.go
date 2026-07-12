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
