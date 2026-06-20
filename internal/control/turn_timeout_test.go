package control

import (
	"testing"
	"time"

	"lumen/internal/config"
)

func TestAgentOptionsFromConfigAppliesTurnTimeout(t *testing.T) {
	cfg := &config.File{}
	cfg.Agent.TurnTimeout = "10m"
	if got := agentOptionsFromConfig(cfg).TurnTimeout; got != 10*time.Minute {
		t.Errorf("TurnTimeout = %v, want 10m", got)
	}
}

func TestAgentOptionsFromConfigTurnTimeoutFallback(t *testing.T) {
	// Empty/unparseable → the safe 5-minute default, never zero (which would
	// disable the per-turn deadline entirely).
	if got := agentOptionsFromConfig(&config.File{}).TurnTimeout; got != 5*time.Minute {
		t.Errorf("empty TurnTimeout = %v, want 5m fallback", got)
	}
}
