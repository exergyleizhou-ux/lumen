package permission

import (
	"context"
	"encoding/json"
	"testing"
)

// The write-path guard (sensitive/persistence paths: SSH keys, shell rc, git
// hooks, /etc…) must cover EVERY path-taking disk writer, not just
// write_file/edit_file. multi_edit, notebook_edit and delete_range also write
// to disk and are auto-approved in bypass/headless mode, so a prompt-injected
// model could plant persistence through them.
func TestModeBypassGuardsAllWriterTools(t *testing.T) {
	g := NewGate(ModeBypass, nil)
	writers := []string{"multi_edit", "notebook_edit", "delete_range"}

	for _, tool := range writers {
		allow, _, _ := g.Check(context.Background(), tool,
			json.RawMessage(`{"path":"~/.ssh/authorized_keys"}`), false)
		if allow {
			t.Errorf("%s to ~/.ssh/authorized_keys must be blocked even in bypass mode", tool)
		}
	}

	// A normal project path must still pass for each writer.
	for _, tool := range writers {
		allow, _, _ := g.Check(context.Background(), tool,
			json.RawMessage(`{"path":"internal/foo.go"}`), false)
		if !allow {
			t.Errorf("%s to a normal project path must be allowed", tool)
		}
	}
}
