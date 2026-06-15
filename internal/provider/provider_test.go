package provider

import (
	"encoding/json"
	"testing"
)

func TestSanitizeToolPairingNormal(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "ok"},
	}
	out := SanitizeToolPairing(msgs)
	if len(out) != 3 {
		t.Errorf("normal conversation: expected 3 msgs, got %d", len(out))
	}
}

func TestSanitizeToolPairingToolCalls(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "write main.go"},
		{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{
			{ID: "1", Name: "write_file", Arguments: `{"path":"main.go"}`},
		}},
		{Role: RoleTool, ToolCallID: "1", Name: "write_file", Content: "wrote ok"},
	}
	out := SanitizeToolPairing(msgs)
	if len(out) != 3 {
		t.Errorf("tool call roundtrip: expected 3 msgs, got %d", len(out))
	}
}

func TestSanitizeToolPairingUnansweredCalls(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "do it"},
		{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{
			{ID: "1", Name: "bash", Arguments: `{"command":"ls"}`},
			{ID: "2", Name: "read_file", Arguments: `{"path":"x"}`},
		}},
		{Role: RoleAssistant, Content: "done"},
	}
	out := SanitizeToolPairing(msgs)

	// Should have: user, assistant(with tool_calls), 2 backfilled tool results, final assistant
	if len(out) != 5 {
		t.Errorf("unanswered calls: expected 5 msgs, got %d", len(out))
		for i, m := range out {
			t.Logf("[%d] role=%s toolCallID=%s content=%q", i, m.Role, m.ToolCallID, m.Content)
		}
	}

	// Backfilled tool results should have placeholder content
	if out[2].Role != RoleTool || out[2].Content != "[no result: interrupted]" {
		t.Errorf("unanswered call 1: expected tool placeholder, got role=%s content=%q", out[2].Role, out[2].Content)
	}
}

func TestSanitizeToolPairingOrphanToolMessages(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleTool, ToolCallID: "orphan", Name: "bash", Content: "orphan result"},
		{Role: RoleAssistant, Content: "ok"},
	}
	out := SanitizeToolPairing(msgs)
	if len(out) != 2 {
		t.Errorf("orphan tool msg: expected 2 msgs, got %d", len(out))
	}
}

func TestCanonicalizeSchema(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	canon := CanonicalizeSchema(raw)

	canon2 := CanonicalizeSchema(raw)
	if string(canon) != string(canon2) {
		t.Error("same schema should canonicalize identically")
	}

	raw2 := json.RawMessage(`{"properties":{"name":{"type":"string"}},"type":"object"}`)
	canon3 := CanonicalizeSchema(raw2)
	if string(canon) != string(canon3) {
		t.Error("reordered schema should canonicalize to same output")
	}
}

func TestParseImageDataURL(t *testing.T) {
	media, b64, ok := parseImageDataURL("data:image/png;base64,iVBORw0KGgo=")
	if !ok {
		t.Error("valid data URL should parse")
	}
	if media != "image/png" {
		t.Errorf("media: want image/png, got %s", media)
	}
	if b64 != "iVBORw0KGgo=" {
		t.Errorf("b64: want iVBORw0KGgo=, got %s", b64)
	}
}

func TestParseImageDataURLInvalid(t *testing.T) {
	_, _, ok := parseImageDataURL("https://example.com/img.png")
	if ok {
		t.Error("http URL should not parse as data URL")
	}
	_, _, ok = parseImageDataURL("data:image/png;base64")
	if ok {
		t.Error("missing payload should not parse")
	}
}

func TestPricingCost(t *testing.T) {
	p := &Pricing{
		CacheHit: 1.0,
		Input:    2.0,
		Output:   5.0,
		Currency: "CNY",
	}

	u := &Usage{
		CacheHitTokens:   1000000,
		CacheMissTokens:  500000,
		CompletionTokens: 200000,
	}

	cost := p.Cost(u)
	expected := 1.0*1 + 2.0*0.5 + 5.0*0.2
	if cost != expected {
		t.Errorf("cost: want %f, got %f", expected, cost)
	}
}

func TestPricingNil(t *testing.T) {
	var p *Pricing
	cost := p.Cost(&Usage{PromptTokens: 1000})
	if cost != 0 {
		t.Errorf("nil pricing should cost 0, got %f", cost)
	}
}

func TestAuthError(t *testing.T) {
	ae := &AuthError{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", Status: 401, HasKey: true}
	msg := ae.Error()
	if msg == "" {
		t.Error("AuthError.Error() should not be empty")
	}
}

func TestStreamInterruptedError(t *testing.T) {
	orig := &StreamInterruptedError{Err: nil}
	if orig.Error() == "" {
		t.Error("StreamInterruptedError.Error() should not be empty")
	}
}

func TestProviderRegistry(t *testing.T) {
	kinds := Kinds()
	// Provider kinds are populated by init() in subpackages (e.g. provider/openai).
	// In isolation, there may be none. This test verifies the registry API works.
	t.Logf("Registered provider kinds: %v", kinds)
	_ = kinds
}

func TestIsStreamInterrupted(t *testing.T) {
	if IsStreamInterrupted(nil) {
		t.Error("nil error should not be stream interrupted")
	}
	se := &StreamInterruptedError{Err: nil}
	if !IsStreamInterrupted(se) {
		t.Error("StreamInterruptedError should be detected")
	}
}
