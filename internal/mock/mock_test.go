package mock

import (
	"context"
	"strings"
	"testing"

	"lumen/internal/provider"
)

func TestStreamingText(t *testing.T) {
	s := NewService("test", "model", StreamingTextScenario())
	ch, err := s.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for chunk := range ch {
		if chunk.Type == provider.ChunkText {
			text += chunk.Text
		}
	}
	if !strings.Contains(text, "Lumen") {
		t.Errorf("unexpected response: %q", text)
	}
}

func TestReadFileRoundtrip(t *testing.T) {
	s := NewService("test", "model", ReadFileRoundtripScenario())
	ch, _ := s.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "read"}},
	})
	var toolCalls int
	for chunk := range ch {
		if chunk.Type == provider.ChunkToolCall {
			toolCalls++
		}
	}
	if toolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", toolCalls)
	}
}

func TestMultiToolTurn(t *testing.T) {
	s := NewService("test", "model", MultiToolTurnScenario())
	ch, _ := s.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "find"}},
	})
	var toolCalls int
	for chunk := range ch {
		if chunk.Type == provider.ChunkToolCall {
			toolCalls++
		}
	}
	if toolCalls != 2 {
		t.Errorf("expected 2 tool calls, got %d", toolCalls)
	}
}

func TestStormBreakerScenario(t *testing.T) {
	s := NewService("test", "model", StormBreakerScenario())
	// 5 turns: 4 tool-call turns + 1 text turn
	for i := 0; i < 5; i++ {
		ch, err := s.Stream(context.Background(), provider.Request{
			Messages: []provider.Message{{Role: provider.RoleUser, Content: "try"}},
		})
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		for range ch {
		}
	}
}

func TestAllScenarios(t *testing.T) {
	for _, sc := range AllScenarios() {
		t.Run(sc.Name, func(t *testing.T) {
			s := NewService("test", "model", sc)
			ch, err := s.Stream(context.Background(), provider.Request{
				Messages: []provider.Message{{Role: provider.RoleUser, Content: "test"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			for range ch {
			}
		})
	}
}

func TestMarshalScenario(t *testing.T) {
	sc := StreamingTextScenario()
	data, err := MarshalScenario(sc)
	if err != nil {
		t.Fatal(err)
	}
	unmarshaled, err := UnmarshalScenario(data)
	if err != nil {
		t.Fatal(err)
	}
	if unmarshaled.Name != sc.Name {
		t.Error("roundtrip failed")
	}
}
