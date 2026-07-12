//go:build live

package gemini

import (
	"context"
	"os"
	"testing"
	"time"

	"lumen/internal/provider"
)

func TestLiveSmokeGemini(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		if key = os.Getenv("GOOGLE_API_KEY"); key == "" {
			t.Skip("GEMINI_API_KEY/GOOGLE_API_KEY not set — skipping live Gemini smoke")
		}
	}
	p, err := New(provider.Config{
		Name:    "gemini-live",
		BaseURL: "https://generativelanguage.googleapis.com",
		Model:   "gemini-2.0-flash",
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
		t.Fatal("empty response from live Gemini API")
	}
}
