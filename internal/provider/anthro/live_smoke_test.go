//go:build live

package anthro

import (
	"context"
	"os"
	"testing"
	"time"

	"lumen/internal/provider"
)

func TestLiveSmokeAnthropic(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set — skipping live Anthropic smoke")
	}
	p, err := New(provider.Config{
		Name:    "anthropic-live",
		BaseURL: "https://api.anthropic.com",
		Model:   "claude-3-5-haiku-latest",
		APIKey:  key,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "Reply with exactly: pong"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for c := range ch {
		if c.Err != nil {
			t.Fatal(c.Err)
		}
		text += c.Text
	}
	if text == "" {
		t.Fatal("empty response from live Anthropic API")
	}
}
