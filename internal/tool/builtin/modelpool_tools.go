package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"lumen/internal/modelpool"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&LLMChatTool{})
	tool.RegisterBuiltin(&LLMCostTool{})
	tool.RegisterBuiltin(&LLMStreamTool{})
}

type LLMChatTool struct{ client *modelpool.Client }
func (t *LLMChatTool) Name() string { return "llm_chat" }
func (t *LLMChatTool) ReadOnly() bool { return true }
func (t *LLMChatTool) Description() string { return "Send a chat request to a registered LLM model (OpenAI or Anthropic). Returns the model's response and token usage." }
func (t *LLMChatTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"model":{"type":"string","description":"Model name (e.g. gpt-4o, claude-sonnet-4-20250514, deepseek-chat)"},"system":{"type":"string","description":"System prompt"},"user":{"type":"string","description":"User message"}},"required":["model","user"]}`)
}
func (t *LLMChatTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Model, System, User string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	client := modelpool.NewClient()
	for _, cfg := range modelpool.DefaultModelConfigs() { client.RegisterModel(cfg) }
	result, usage, err := client.Chat(ctx, p.Model, []modelpool.Message{
		modelpool.NewTextMessage(modelpool.RoleSystem, p.System),
		modelpool.NewTextMessage(modelpool.RoleUser, p.User),
	})
	if err != nil { return "", err }
	return fmt.Sprintf("%s\n\n── Tokens: in=%d out=%d cost=$%.4f", result, usage.InputTokens, usage.OutputTokens, usage.Cost(p.Model)), nil
}

type LLMCostTool struct{}
func (t *LLMCostTool) Name() string { return "llm_cost_report" }
func (t *LLMCostTool) ReadOnly() bool { return true }
func (t *LLMCostTool) Description() string { return "Show accumulated LLM cost and usage report across all models." }
func (t *LLMCostTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (t *LLMCostTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	tracker := modelpool.NewCostTracker()
	return tracker.Report(), nil
}

type LLMStreamTool struct{}
func (t *LLMStreamTool) Name() string { return "llm_stream" }
func (t *LLMStreamTool) ReadOnly() bool { return true }
func (t *LLMStreamTool) Description() string { return "Stream a chat response from a registered LLM model token by token." }
func (t *LLMStreamTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"model":{"type":"string"},"system":{"type":"string"},"user":{"type":"string"}},"required":["model","user"]}`)
}
func (t *LLMStreamTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Model, System, User string }
	if err := json.Unmarshal(args, &p); err != nil { return "", err }
	client := modelpool.NewClient()
	for _, cfg := range modelpool.DefaultModelConfigs() { client.RegisterModel(cfg) }
	var fullText string
	_, err := client.ChatStream(ctx, p.Model, []modelpool.Message{
		modelpool.NewTextMessage(modelpool.RoleSystem, p.System),
		modelpool.NewTextMessage(modelpool.RoleUser, p.User),
	}, func(ev modelpool.StreamEvent) {
		if ev.Type == "text_delta" { fullText += ev.Text; fmt.Print(ev.Text); _ = ev.Text }
	})
	fmt.Println()
	return fullText, err
}
