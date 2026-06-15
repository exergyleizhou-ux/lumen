// Package modelpool provides a unified LLM client for OpenAI and Anthropic
// APIs with streaming support, automatic retry, cost tracking, token
// counting, and model failover. This is the real LLM integration layer
// that turns Lumen from a "model manager" into a production LLM caller.
package modelpool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ── Unified LLM Client ────────────────────────────────────

// Provider identifies the LLM provider.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
)

// ModelConfig holds per-model configuration.
type ModelConfig struct {
	Name        string   `json:"name"`
	Provider    Provider `json:"provider"`
	APIKey      string   `json:"-"`
	BaseURL     string   `json:"base_url"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature float64  `json:"temperature"`
}

// DefaultModelConfigs returns common model configs.
func DefaultModelConfigs() []ModelConfig {
	return []ModelConfig{
		{Name: "gpt-4o", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1", MaxTokens: 4096, Temperature: 0.7},
		{Name: "gpt-4o-mini", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1", MaxTokens: 2048, Temperature: 0.7},
		{Name: "claude-sonnet-4-20250514", Provider: ProviderAnthropic, BaseURL: "https://api.anthropic.com/v1", MaxTokens: 4096, Temperature: 0.7},
		{Name: "claude-3-5-haiku-20241022", Provider: ProviderAnthropic, BaseURL: "https://api.anthropic.com/v1", MaxTokens: 2048, Temperature: 0.7},
	}
}

// ── Message Types ──────────────────────────────────────────

// Role is the message author role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentBlock is one piece of message content (text or tool use).
type ContentBlock struct {
	Type      string         `json:"type"` // "text" or "tool_use"
	Text      string         `json:"text,omitempty"`
	ToolID    string         `json:"id,omitempty"`
	ToolName  string         `json:"name,omitempty"`
	ToolInput map[string]any `json:"input,omitempty"`
}

// Message is a chat message.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content,omitempty"`
	Text    string         `json:"-"` // Convenience for simple text messages
}

// NewTextMessage creates a simple text message.
func NewTextMessage(role Role, text string) Message {
	return Message{Role: role, Text: text, Content: []ContentBlock{{Type: "text", Text: text}}}
}

// ── Streaming ──────────────────────────────────────────────

// StreamEvent is one chunk from a streaming response.
type StreamEvent struct {
	Type       string         `json:"type"` // "text_delta", "tool_use_start", "tool_use_delta", "message_stop"
	Text       string         `json:"text,omitempty"`
	ToolID     string         `json:"tool_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolInput  map[string]any `json:"tool_input,omitempty"`
	StopReason string         `json:"stop_reason,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
}

// StreamHandler receives streaming events.
type StreamHandler func(event StreamEvent)

// ── Usage ──────────────────────────────────────────────────

// Usage tracks token usage.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Cost estimates the USD cost.
func (u *Usage) Cost(model string) float64 {
	// Approximate pricing per 1M tokens (as of 2025)
	pricing := map[string][2]float64{ // [input, output] per 1M tokens
		"gpt-4o":                    {2.50, 10.00},
		"gpt-4o-mini":               {0.15, 0.60},
		"claude-sonnet-4-20250514":  {3.00, 15.00},
		"claude-3-5-haiku-20241022": {0.25, 1.25},
	}
	if p, ok := pricing[model]; ok {
		return float64(u.InputTokens)*p[0]/1_000_000 + float64(u.OutputTokens)*p[1]/1_000_000
	}
	return 0
}

// ── Client ─────────────────────────────────────────────────

// Client is a unified LLM client.
type Client struct {
	mu       sync.Mutex
	configs  map[string]*ModelConfig
	http     *http.Client
	tracker  *CostTracker
	fallback bool
}

// CostTracker tracks cumulative usage.
type CostTracker struct {
	mu         sync.Mutex
	totalCost  float64
	totalCalls int64
	byModel    map[string]*modelStats
}

type modelStats struct {
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	Cost         float64
}

// NewCostTracker creates a cost tracker.
func NewCostTracker() *CostTracker {
	return &CostTracker{byModel: map[string]*modelStats{}}
}

// Record adds usage to the tracker.
func (ct *CostTracker) Record(model string, usage *Usage) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	cost := usage.Cost(model)
	ct.totalCost += cost
	ct.totalCalls++
	ms, ok := ct.byModel[model]
	if !ok {
		ms = &modelStats{}
		ct.byModel[model] = ms
	}
	ms.Calls++
	ms.InputTokens += int64(usage.InputTokens)
	ms.OutputTokens += int64(usage.OutputTokens)
	ms.Cost += cost
}

// Report returns a cost report.
func (ct *CostTracker) Report() string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "LLM Cost Report: %d calls, $%.4f total\n%s\n\n", ct.totalCalls, ct.totalCost, strings.Repeat("─", 50))
	for model, ms := range ct.byModel {
		fmt.Fprintf(&sb, "  %-35s %4d calls  in=%d out=%d $%.4f\n", model, ms.Calls, ms.InputTokens, ms.OutputTokens, ms.Cost)
	}
	return sb.String()
}

// NewClient creates an LLM client.
func NewClient() *Client {
	return &Client{
		configs:  map[string]*ModelConfig{},
		http:     &http.Client{Timeout: 120 * time.Second},
		tracker:  NewCostTracker(),
		fallback: true,
	}
}

// RegisterModel adds a model configuration.
func (c *Client) RegisterModel(cfg ModelConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configs[cfg.Name] = &cfg
}

// SetFallback enables/disables automatic model fallback on failure.
func (c *Client) SetFallback(on bool) { c.mu.Lock(); defer c.mu.Unlock(); c.fallback = on }

// Chat sends a chat completion request (non-streaming).
func (c *Client) Chat(ctx context.Context, model string, messages []Message) (string, *Usage, error) {
	c.mu.Lock()
	cfg, ok := c.configs[model]
	if !ok {
		c.mu.Unlock()
		return "", nil, fmt.Errorf("model %q not registered", model)
	}
	c.mu.Unlock()

	return c.chatWithRetry(ctx, cfg, messages, 3)
}

func (c *Client) chatWithRetry(ctx context.Context, cfg *ModelConfig, messages []Message, maxRetries int) (string, *Usage, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		result, usage, err := c.doChat(ctx, cfg, messages)
		if err == nil {
			c.tracker.Record(cfg.Name, usage)
			return result, usage, nil
		}
		lastErr = err

		// Try fallback model if enabled
		if c.fallback && attempt == maxRetries-1 {
			if fb := c.findFallback(cfg); fb != nil {
				result, usage, err := c.doChat(ctx, fb, messages)
				if err == nil {
					c.tracker.Record(fb.Name, usage)
					return result, usage, nil
				}
				lastErr = fmt.Errorf("primary + fallback both failed: %w", err)
			}
		}
	}
	return "", nil, lastErr
}

func (c *Client) doChat(ctx context.Context, cfg *ModelConfig, messages []Message) (string, *Usage, error) {
	switch cfg.Provider {
	case ProviderOpenAI:
		return c.openAIChat(ctx, cfg, messages)
	case ProviderAnthropic:
		return c.anthropicChat(ctx, cfg, messages)
	default:
		return "", nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}

func (c *Client) findFallback(cfg *ModelConfig) *ModelConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Prefer same provider, cheaper model
	candidates := []string{
		"gpt-4o-mini",
		"claude-3-5-haiku-20241022",
	}
	for _, name := range candidates {
		if fb, ok := c.configs[name]; ok && fb.Provider == cfg.Provider && fb.Name != cfg.Name {
			return fb
		}
	}
	return nil
}

// ── OpenAI Chat ────────────────────────────────────────────

type openAIRequest struct {
	Model       string      `json:"model"`
	Messages    []openAIMsg `json:"messages"`
	MaxTokens   int         `json:"max_tokens,omitempty"`
	Temperature float64     `json:"temperature,omitempty"`
	Stream      bool        `json:"stream"`
}

type openAIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *Client) openAIChat(ctx context.Context, cfg *ModelConfig, messages []Message) (string, *Usage, error) {
	var oaiMessages []openAIMsg
	for _, m := range messages {
		text := m.Text
		if text == "" {
			for _, block := range m.Content {
				if block.Type == "text" {
					text += block.Text
				}
			}
		}
		oaiMessages = append(oaiMessages, openAIMsg{Role: string(m.Role), Content: text})
	}

	body := openAIRequest{Model: cfg.Name, Messages: oaiMessages, MaxTokens: cfg.MaxTokens, Temperature: cfg.Temperature, Stream: false}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("openai %d: %s", resp.StatusCode, string(respBody))
	}

	var result openAIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, err
	}
	if len(result.Choices) == 0 {
		return "", nil, fmt.Errorf("no choices returned")
	}

	usage := &Usage{InputTokens: result.Usage.PromptTokens, OutputTokens: result.Usage.CompletionTokens}
	return result.Choices[0].Message.Content, usage, nil
}

// ── Anthropic Chat ─────────────────────────────────────────

type anthropicRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	Messages  []anthropicMsg `json:"messages"`
	System    string         `json:"system,omitempty"`
	Stream    bool           `json:"stream"`
}

type anthropicMsg struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (c *Client) anthropicChat(ctx context.Context, cfg *ModelConfig, messages []Message) (string, *Usage, error) {
	var systemPrompt string
	var antMessages []anthropicMsg

	for _, m := range messages {
		if m.Role == RoleSystem {
			systemPrompt = m.Text
			continue
		}
		text := m.Text
		if text == "" {
			for _, block := range m.Content {
				if block.Type == "text" {
					text += block.Text
				}
			}
		}
		antMessages = append(antMessages, anthropicMsg{
			Role:    string(m.Role),
			Content: []anthropicContent{{Type: "text", Text: text}},
		})
	}

	body := anthropicRequest{Model: cfg.Name, MaxTokens: cfg.MaxTokens, Messages: antMessages, System: systemPrompt, Stream: false}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.BaseURL+"/messages", bytes.NewReader(b))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(respBody))
	}

	var result anthropicResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, err
	}
	if len(result.Content) == 0 {
		return "", nil, fmt.Errorf("no content returned")
	}

	usage := &Usage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens}
	return result.Content[0].Text, usage, nil
}

// ── Streaming Chat ─────────────────────────────────────────

// ChatStream sends a chat request and streams the response.
func (c *Client) ChatStream(ctx context.Context, model string, messages []Message, handler StreamHandler) (*Usage, error) {
	c.mu.Lock()
	cfg, ok := c.configs[model]
	if !ok {
		c.mu.Unlock()
		return nil, fmt.Errorf("model %q not registered", model)
	}
	c.mu.Unlock()

	switch cfg.Provider {
	case ProviderOpenAI:
		return c.openAIStream(ctx, cfg, messages, handler)
	case ProviderAnthropic:
		return c.anthropicStream(ctx, cfg, messages, handler)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", cfg.Provider)
	}
}

func (c *Client) openAIStream(ctx context.Context, cfg *ModelConfig, messages []Message, handler StreamHandler) (*Usage, error) {
	var oaiMessages []openAIMsg
	for _, m := range messages {
		text := m.Text
		if text == "" {
			for _, block := range m.Content {
				if block.Type == "text" {
					text += block.Text
				}
			}
		}
		oaiMessages = append(oaiMessages, openAIMsg{Role: string(m.Role), Content: text})
	}

	body := openAIRequest{Model: cfg.Name, Messages: oaiMessages, MaxTokens: cfg.MaxTokens, Temperature: cfg.Temperature, Stream: true}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai stream %d: %s", resp.StatusCode, string(body))
	}

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	var fullText string
	var usage *Usage

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				fullText += delta
				handler(StreamEvent{Type: "text_delta", Text: delta})
			}
			if chunk.Choices[0].FinishReason != "" {
				handler(StreamEvent{Type: "message_stop", StopReason: chunk.Choices[0].FinishReason})
			}
		}
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = &Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
	}

	if usage == nil {
		usage = &Usage{}
	}
	c.tracker.Record(cfg.Name, usage)
	return usage, nil
}

func (c *Client) anthropicStream(ctx context.Context, cfg *ModelConfig, messages []Message, handler StreamHandler) (*Usage, error) {
	var systemPrompt string
	var antMessages []anthropicMsg
	for _, m := range messages {
		if m.Role == RoleSystem {
			systemPrompt = m.Text
			continue
		}
		text := m.Text
		if text == "" {
			for _, block := range m.Content {
				if block.Type == "text" {
					text += block.Text
				}
			}
		}
		antMessages = append(antMessages, anthropicMsg{Role: string(m.Role), Content: []anthropicContent{{Type: "text", Text: text}}})
	}

	body := anthropicRequest{Model: cfg.Name, MaxTokens: cfg.MaxTokens, Messages: antMessages, System: systemPrompt, Stream: true}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.BaseURL+"/messages", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic stream %d: %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	var fullText string
	var usage *Usage
	var currentToolID string

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				currentToolID = event.ContentBlock.ID
				handler(StreamEvent{Type: "tool_use_start", ToolID: event.ContentBlock.ID, ToolName: event.ContentBlock.Name})
			}
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				fullText += event.Delta.Text
				handler(StreamEvent{Type: "text_delta", Text: event.Delta.Text})
			} else if event.Delta.Type == "input_json_delta" {
				handler(StreamEvent{Type: "tool_use_delta", ToolID: currentToolID, Text: event.Delta.PartialJSON})
			}
		case "message_stop":
			handler(StreamEvent{Type: "message_stop"})
		}

		if event.Usage.InputTokens > 0 || event.Usage.OutputTokens > 0 {
			usage = &Usage{InputTokens: event.Usage.InputTokens, OutputTokens: event.Usage.OutputTokens}
		}
	}

	if usage == nil {
		usage = &Usage{}
	}
	c.tracker.Record(cfg.Name, usage)
	return usage, nil
}

// ── Cost Report ────────────────────────────────────────────

// CostReport returns the accumulated cost report.
func (c *Client) CostReport() string { return c.tracker.Report() }

// ── Simple One-shot ────────────────────────────────────────

// Ask is a convenience method for a single-turn chat.
func (c *Client) Ask(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	var messages []Message
	if systemPrompt != "" {
		messages = append(messages, NewTextMessage(RoleSystem, systemPrompt))
	}
	messages = append(messages, NewTextMessage(RoleUser, userPrompt))
	result, _, err := c.Chat(ctx, model, messages)
	return result, err
}
