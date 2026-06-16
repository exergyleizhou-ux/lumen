package editverify

import (
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if !c.Enabled {
		t.Error("Enabled should default to true")
	}
	if c.Command != "" {
		t.Errorf("Command should default to empty, got %q", c.Command)
	}
	if c.Scope != "changed-pkg" {
		t.Errorf("Scope should default to changed-pkg, got %q", c.Scope)
	}
	if !c.RunTests {
		t.Error("RunTests should default to true")
	}
	if c.MaxRepairCycles != 3 {
		t.Errorf("MaxRepairCycles should default to 3, got %d", c.MaxRepairCycles)
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if got := truncate(short); got != short {
		t.Errorf("short string should pass through, got %q", got)
	}
	long := strings.Repeat("x", maxOutputBytes+100)
	got := truncate(long)
	if len(got) >= len(long) {
		t.Errorf("long string should be truncated, got len %d", len(got))
	}
	if !strings.HasSuffix(got, "(truncated)") {
		t.Error("truncated output should be marked")
	}
}
