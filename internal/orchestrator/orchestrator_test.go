package orchestrator

import (
	"testing"
)

func TestNewAgentPool(t *testing.T) {
	ap := NewAgentPool()
	if ap == nil {
		t.Error("nil")
	}
}
func TestConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxParallel <= 0 {
		t.Error("maxparallel")
	}
}
func TestExecutor(t *testing.T) {
	pool := NewAgentPool()
	e := NewExecutor(DefaultConfig(), pool)
	_ = e
}
