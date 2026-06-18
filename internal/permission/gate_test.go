package permission

import (
	"context"
	"encoding/json"
	"testing"
)

func TestModeBypass(t *testing.T) {
	g := NewGate(ModeBypass, nil)

	// Safe commands pass even in bypass
	allow, reason, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"echo hello"}`), false)
	if err != nil {
		t.Fatalf("safe command should not error: %v", err)
	}
	if !allow {
		t.Errorf("safe command should be allowed: %s", reason)
	}

	// Destructive bash commands are blocked by content guard regardless of mode
	allow, _, _ = g.Check(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /"}`), false)
	if allow {
		t.Error("destructive command should be blocked even in bypass mode")
	}

	// Writes to sensitive/persistence paths are blocked even in bypass mode.
	for _, p := range []string{"~/.ssh/authorized_keys", ".git/hooks/pre-commit", "~/.bashrc", "/etc/cron.d/x"} {
		allow, _, _ = g.Check(context.Background(), "write_file", json.RawMessage(`{"path":"`+p+`"}`), false)
		if allow {
			t.Errorf("write to sensitive path %q should be blocked even in bypass mode", p)
		}
	}
	// A normal project write still passes in bypass.
	allow, _, _ = g.Check(context.Background(), "write_file", json.RawMessage(`{"path":"internal/foo.go"}`), false)
	if !allow {
		t.Error("normal project write should be allowed in bypass mode")
	}
}

func TestModePlan(t *testing.T) {
	g := NewGate(ModePlan, nil)

	// Read-only: allowed
	allow, _, _ := g.Check(context.Background(), "read_file", json.RawMessage(`{"path":"x"}`), true)
	if !allow {
		t.Error("plan mode should allow read-only tools")
	}

	// Writer: blocked
	allow, reason, _ := g.Check(context.Background(), "write_file", json.RawMessage(`{"path":"x"}`), false)
	if allow {
		t.Error("plan mode should block writer tools")
	}
	if reason == "" {
		t.Error("plan mode block should have a reason")
	}
}

func TestModeDefaultSafeTools(t *testing.T) {
	g := NewGate(ModeDefault, nil)

	safeTools := []string{"read_file", "grep", "glob", "ls", "web_fetch", "web_search", "ask"}
	for _, name := range safeTools {
		allow, _, err := g.Check(context.Background(), name, json.RawMessage(`{}`), true)
		if err != nil {
			t.Errorf("safe tool %s unexpected error: %v", name, err)
		}
		if !allow {
			t.Errorf("safe tool %s should be allowed in default mode", name)
		}
	}
}

func TestModeDefaultDangerousToolsNoAsker(t *testing.T) {
	g := NewGate(ModeDefault, nil)

	// bash without asker → blocked
	allow, reason, _ := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"ls"}`), false)
	if allow {
		t.Error("dangerous tools should be blocked without asker")
	}
	if reason == "" {
		t.Error("block reason should not be empty")
	}
}

func TestModeDefaultDangerousToolsWithAsker(t *testing.T) {
	asker := func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		return true, nil // user approves
	}
	g := NewGate(ModeDefault, asker)

	allow, _, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"ls"}`), false)
	if err != nil {
		t.Fatalf("asker should not error: %v", err)
	}
	if !allow {
		t.Error("dangerous tool should be allowed when asker approves")
	}
}

func TestModeDefaultDangerousToolsDenied(t *testing.T) {
	asker := func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		return false, nil // user denies
	}
	g := NewGate(ModeDefault, asker)

	allow, _, _ := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"ls"}`), false)
	if allow {
		t.Error("dangerous tool should be denied when asker says no")
	}
}

func TestModeDefaultWriterToolsHeadless(t *testing.T) {
	g := NewGate(ModeDefault, nil)

	// Non-dangerous writer without asker → allowed (headless mode)
	allow, _, _ := g.Check(context.Background(), "write_file", json.RawMessage(`{"path":"x"}`), false)
	if !allow {
		t.Error("non-dangerous writer should be allowed in headless default mode")
	}
}

func TestModeDefaultWriterToolsInteractive(t *testing.T) {
	asker := func(ctx context.Context, toolName string, args json.RawMessage) (bool, error) {
		return true, nil
	}
	g := NewGate(ModeDefault, asker)

	allow, _, _ := g.Check(context.Background(), "write_file", json.RawMessage(`{"path":"x"}`), false)
	if !allow {
		t.Error("writer should be allowed when asker approves")
	}
}

func TestModeAcceptEdits(t *testing.T) {
	g := NewGate(ModeAcceptEdits, nil)

	// Non-dangerous writer: allowed
	allow, _, _ := g.Check(context.Background(), "write_file", json.RawMessage(`{}`), false)
	if !allow {
		t.Error("accept-edits should allow writers")
	}

	// Dangerous: blocked without asker
	allow, _, _ = g.Check(context.Background(), "bash", json.RawMessage(`{}`), false)
	if allow {
		t.Error("accept-edits should block dangerous tools without asker")
	}
}

func TestSummarizeArgs(t *testing.T) {
	if s := SummarizeArgs("bash", json.RawMessage(`{"command":"go build"}`)); s != "bash: go build" {
		t.Errorf("bash summary: want 'bash: go build', got %q", s)
	}
	if s := SummarizeArgs("write_file", json.RawMessage(`{"path":"/tmp/x.go"}`)); s != "write /tmp/x.go" {
		t.Errorf("write summary: want 'write /tmp/x.go', got %q", s)
	}
	if s := SummarizeArgs("edit_file", json.RawMessage(`{"path":"main.go"}`)); s != "edit main.go" {
		t.Errorf("edit summary: want 'edit main.go', got %q", s)
	}
	if s := SummarizeArgs("unknown_tool", json.RawMessage(`{}`)); s != "unknown_tool" {
		t.Errorf("unknown tool summary: want 'unknown_tool', got %q", s)
	}
}

func TestParseMode(t *testing.T) {
	if ParseMode("bypass") != ModeBypass {
		t.Error("ParseMode bypass")
	}
	if ParseMode("BYPASS") != ModeBypass {
		t.Error("ParseMode case insensitive")
	}
	if ParseMode("accept-edits") != ModeAcceptEdits {
		t.Error("ParseMode accept-edits")
	}
	if ParseMode("accept_edits") != ModeAcceptEdits {
		t.Error("ParseMode accept_edits")
	}
	if ParseMode("plan") != ModePlan {
		t.Error("ParseMode plan")
	}
	if ParseMode("") != ModeDefault {
		t.Error("empty string should be default")
	}
	if ParseMode("unknown") != ModeDefault {
		t.Error("unknown should be default")
	}
}
