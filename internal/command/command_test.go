package command

import "testing"

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	cmds := r.List()
	if len(cmds) < 6 {
		t.Errorf("expected at least 6 built-in commands, got %d", len(cmds))
	}
}

func TestGetCommand(t *testing.T) {
	r := NewRegistry()
	cmd, ok := r.Get("status")
	if !ok {
		t.Fatal("status command not found")
	}
	if cmd.Name != "status" {
		t.Errorf("name: got %s", cmd.Name)
	}
}

func TestGetByAlias(t *testing.T) {
	r := NewRegistry()
	cmd, ok := r.Get("stats")
	if !ok {
		t.Fatal("stats alias not found")
	}
	if cmd.Name != "status" {
		t.Errorf("alias should resolve to status, got %s", cmd.Name)
	}
}

func TestGetUnknown(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("should not find unknown command")
	}
}

func TestHelpCommand(t *testing.T) {
	r := NewRegistry()
	cmd, _ := r.Get("help")
	result := cmd.Run(Context{})
	if result.Error != nil {
		t.Fatalf("help failed: %v", result.Error)
	}
	if result.Text == "" {
		t.Error("help should return text")
	}
}

func TestStatusCommandNilAgent(t *testing.T) {
	r := NewRegistry()
	cmd, _ := r.Get("status")
	result := cmd.Run(Context{})
	if result.Error == nil {
		t.Log("status without agent returns gracefully")
	}
}
