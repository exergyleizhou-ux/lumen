package routing

import (
	"testing"

	"lumen/internal/config"
)

func TestNewRouterNoProviders(t *testing.T) {
	_, err := NewRouter(nil)
	if err == nil {
		t.Error("should error with no providers")
	}
}

func TestSelectByTask(t *testing.T) {
	providers := []config.ProviderConfig{
		{Name: "fast", Model: "flash"},
		{Name: "smart", Model: "pro"},
	}
	p := SelectByTask("explain this code", providers)
	if p == nil {
		t.Error("should return a provider")
	}
}

func TestSelectByTokenBudget(t *testing.T) {
	providers := []config.ProviderConfig{
		{Name: "expensive", Model: "pro"},
		{Name: "cheap", Model: "flash"},
	}
	p := SelectByTokenBudget(10000, providers)
	if p == nil {
		t.Error("should return a provider")
	}
	// Tight budget should pick the cheapest
	if p.Name != "cheap" {
		t.Errorf("tight budget should pick cheap, got %s", p.Name)
	}

	p2 := SelectByTokenBudget(1000000, providers)
	if p2.Name != "expensive" {
		t.Errorf("ample budget should pick default, got %s", p2.Name)
	}
}
