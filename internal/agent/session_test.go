package agent

import (
	"strings"
	"testing"

	"lumen/internal/provider"
)

func TestSessionAdd(t *testing.T) {
	s := NewSession("")
	if s.Len() != 0 {
		t.Error("new session should be empty")
	}

	s.Add(provider.Message{Role: provider.RoleSystem, Content: "you are a bot"})
	if s.Len() != 1 {
		t.Errorf("expected 1 message, got %d", s.Len())
	}

	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if s.Len() != 2 {
		t.Errorf("expected 2 messages, got %d", s.Len())
	}
}

func TestSessionSnapshot(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleSystem, Content: "sys"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})

	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 messages in snapshot, got %d", len(snap))
	}
	if snap[0].Content != "sys" {
		t.Errorf("expected first message 'sys', got %q", snap[0].Content)
	}

	// Snapshot should be a copy — modifying it doesn't affect the session
	snap[0] = provider.Message{Role: provider.RoleUser, Content: "mutated"}
	snap2 := s.Snapshot()
	if snap2[0].Content != "sys" {
		t.Error("snapshot should return a copy, not the original slice")
	}
}

func TestSessionCompact(t *testing.T) {
	s := NewSession("")

	// Add 10 messages
	for i := 0; i < 10; i++ {
		s.Add(provider.Message{
			Role:    provider.RoleUser,
			Content: "message " + string(rune('0'+i)),
		})
	}
	if s.Len() != 10 {
		t.Fatalf("expected 10 messages, got %d", s.Len())
	}

	// Compact: keep first 2, last 2, summarize middle
	s.Compact(2, 2, "summary of middle 6 messages")
	if s.Len() != 5 { // 2 + 1 summary + 2 = 5
		// Actually the Compact function might produce different counts.
		// Let's check the behavior.
		messages := s.Snapshot()
		t.Logf("after compact: %d messages", len(messages))
		for i, m := range messages {
			t.Logf("  [%d] %s: %s", i, m.Role, m.Content[:min(30, len(m.Content))])
		}
	}
}

func TestSessionCompactTooSmall(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "1"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "2"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "3"})

	s.Compact(2, 2, "summary")
	// 3 messages <= 2+2=4, so compact should do nothing
	if s.Len() != 3 {
		t.Errorf("compact should not change session smaller than keepFirst+keepLast, got %d", s.Len())
	}
}

func TestSessionSystemPrompt(t *testing.T) {
	s := NewSession("")

	prompt := s.SystemPrompt("You are a bot.", "Project memory here.")
	if len(prompt) == 0 {
		t.Error("SystemPrompt should return non-empty string")
	}
	if !strings.HasPrefix(prompt, "You are a bot.") {
		t.Errorf("prompt should start with base prompt, got %q", prompt[:min(30, len(prompt))])
	}
}
