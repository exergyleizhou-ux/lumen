package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsTurnTimeout(t *testing.T) {
	if got := defaults().Agent.TurnTimeout; got != "5m" {
		t.Errorf("default turn_timeout = %q, want \"5m\"", got)
	}
}

func TestLoadTurnTimeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lumen.toml")
	if err := os.WriteFile(p, []byte("[agent]\nturn_timeout = \"15m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.TurnTimeout != "15m" {
		t.Errorf("turn_timeout = %q, want \"15m\"", cfg.Agent.TurnTimeout)
	}
}

func TestLoadTurnTimeoutInvalidIsRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lumen.toml")
	if err := os.WriteFile(p, []byte("[agent]\nturn_timeout = \"notaduration\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected Load to reject an invalid turn_timeout")
	}
}
