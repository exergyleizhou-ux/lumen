package editverify

import (
	"testing"
)

func TestConfigFromTOML_empty(t *testing.T) {
	c, err := ConfigFromTOML([]byte(""))
	if err != nil {
		t.Fatalf("empty TOML: %v", err)
	}
	def := DefaultConfig()
	if c != def {
		t.Errorf("empty TOML should give DefaultConfig. got %+v, want %+v", c, def)
	}
}

func TestConfigFromTOML_enabledFalse(t *testing.T) {
	raw := []byte(`
[verify]
enabled = false
`)
	c, err := ConfigFromTOML(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Enabled != false {
		t.Errorf("Enabled = %v, want false", c.Enabled)
	}
	// Other fields must still be defaults
	def := DefaultConfig()
	if c.Command != def.Command {
		t.Errorf("Command should be default %q, got %q", def.Command, c.Command)
	}
	if c.RunTests != def.RunTests {
		t.Errorf("RunTests should be default %v, got %v", def.RunTests, c.RunTests)
	}
}

func TestConfigFromTOML_scopeAll(t *testing.T) {
	raw := []byte(`
[verify]
scope = "all"
`)
	c, err := ConfigFromTOML(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Scope != "all" {
		t.Errorf("Scope = %q, want all", c.Scope)
	}
}

func TestConfigFromTOML_invalidScope(t *testing.T) {
	raw := []byte(`
[verify]
scope = "full-scan"
`)
	c, err := ConfigFromTOML(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Scope != "changed-pkg" {
		t.Errorf("invalid scope should fallback to changed-pkg, got %q", c.Scope)
	}
}

func TestConfigFromTOML_maxRepairZero(t *testing.T) {
	raw := []byte(`
[verify]
max_repair_cycles = 0
`)
	c, err := ConfigFromTOML(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.MaxRepairCycles != 3 {
		t.Errorf("max_repair_cycles=0 should fallback to 3, got %d", c.MaxRepairCycles)
	}
}

func TestConfigFromTOML_runTestsFalse(t *testing.T) {
	raw := []byte(`
[verify]
run_tests = false
`)
	c, err := ConfigFromTOML(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.RunTests != false {
		t.Errorf("RunTests = %v, want false", c.RunTests)
	}
}

func TestConfigFromTOML_fullSection(t *testing.T) {
	raw := []byte(`
[verify]
enabled = true
command = "golangci-lint run"
scope = "all"
run_tests = true
max_repair_cycles = 5
`)
	c, err := ConfigFromTOML(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !c.Enabled {
		t.Error("Enabled should be true")
	}
	if c.Command != "golangci-lint run" {
		t.Errorf("Command = %q", c.Command)
	}
	if c.Scope != "all" {
		t.Errorf("Scope = %q", c.Scope)
	}
	if !c.RunTests {
		t.Error("RunTests should be true")
	}
	if c.MaxRepairCycles != 5 {
		t.Errorf("MaxRepairCycles = %d, want 5", c.MaxRepairCycles)
	}
}

func TestConfigFromTOML_missingSection(t *testing.T) {
	// TOML file with no [verify] section at all
	raw := []byte(`
[other]
key = "value"
`)
	c, err := ConfigFromTOML(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c != DefaultConfig() {
		t.Errorf("missing [verify] should give DefaultConfig, got %+v", c)
	}
}

func TestConfigFromTOML_syntaxError(t *testing.T) {
	raw := []byte(`this is not valid TOML = [[[`)
	_, err := ConfigFromTOML(raw)
	if err == nil {
		t.Fatal("syntax error should return an error")
	}
}
