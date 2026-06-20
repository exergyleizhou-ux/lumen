package openai

import (
	"testing"
	"time"

	"lumen/internal/provider"
)

func TestNewHonorsConfiguredTimeout(t *testing.T) {
	p, err := New(provider.Config{BaseURL: "http://x", Timeout: 42 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.(*Provider).client.Timeout; got != 42*time.Second {
		t.Errorf("client.Timeout = %v, want 42s", got)
	}
}

func TestNewDefaultsTimeoutTo5Min(t *testing.T) {
	p, _ := New(provider.Config{BaseURL: "http://x"})
	if got := p.(*Provider).client.Timeout; got != 5*time.Minute {
		t.Errorf("default client.Timeout = %v, want 5m", got)
	}
}
