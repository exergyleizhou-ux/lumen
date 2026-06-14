// Package openai implements the Provider interface for OpenAI-compatible APIs
// (DeepSeek, Grok, OpenAI, Ollama, and any other /v1/chat/completions endpoint).
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"lumen/internal/provider"
)

func init() {
	provider.Register("openai", New)
}

// New creates an OpenAI-compatible provider from config.
func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Provider{
		name:    cfg.Name,
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		client:  &http.Client{},
	}, nil
}

// Provider streams completions from an OpenAI-compatible endpoint.
type Provider struct {
	name    string
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 32)

	go func() {
		defer close(ch)
		p.stream(ctx, req, ch)
	}()

	return ch, nil
}

func (p *Provider) stream(ctx context.Context, req provider.Request, ch chan<- provider.Chunk) {
	body := buildRequest(req, p.model)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("marshal request: %w", err)}
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: err}
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			ch <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
			return
		}
		ch <- provider.Chunk{Type: provider.ChunkError, Err: err}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: &provider.AuthError{
			Provider: p.name,
			Status:   resp.StatusCode,
			HasKey:   p.apiKey != "",
		}}
		return
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		ch <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))}
		return
	}

	p.parseSSE(ctx, resp.Body, ch)
}

func (p *Provider) parseSSE(ctx context.Context, r io.Reader, ch chan<- provider.Chunk) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var (
		textBuf     strings.Builder
		reasonBuf   strings.Builder
		toolCallBuf *partialToolCall
	)

	for scanner.Scan() {
		if ctx.Err() != nil {
			ch <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
			return
		}

		line := scanner.Text()
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			// Flush any pending tool call
			if toolCallBuf != nil && toolCallBuf.name != "" {
				ch <- provider.Chunk{
					Type:     provider.ChunkToolCall,
					ToolCall: toolCallBuf.finalize(),
				}
				toolCallBuf = nil
			}
			ch <- provider.Chunk{Type: provider.ChunkDone}
			return
		}

		var sse struct {
			Choices []struct {
				Delta struct {
					Role             string `json:"role"`
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
				// DeepSeek-specific cache accounting.
				PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
				PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
				// OpenAI-style cache + reasoning accounting.
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokensDetails struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &sse); err != nil {
			continue
		}

		// Emit usage when present, normalizing cache accounting across providers.
		if sse.Usage != nil {
			u := &provider.Usage{
				PromptTokens:     sse.Usage.PromptTokens,
				CompletionTokens: sse.Usage.CompletionTokens,
				TotalTokens:      sse.Usage.TotalTokens,
				ReasoningTokens:  sse.Usage.CompletionTokensDetails.ReasoningTokens,
			}
			// Prefer DeepSeek's explicit hit/miss split; otherwise fall back to
			// OpenAI's cached_tokens (the rest of the prompt counts as a miss).
			if sse.Usage.PromptCacheHitTokens > 0 || sse.Usage.PromptCacheMissTokens > 0 {
				u.CacheHitTokens = sse.Usage.PromptCacheHitTokens
				u.CacheMissTokens = sse.Usage.PromptCacheMissTokens
			} else {
				u.CacheHitTokens = sse.Usage.PromptTokensDetails.CachedTokens
				u.CacheMissTokens = sse.Usage.PromptTokens - sse.Usage.PromptTokensDetails.CachedTokens
			}
			ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: u}
		}

		if len(sse.Choices) == 0 {
			continue
		}
		delta := sse.Choices[0].Delta

		// Reasoning content (DeepSeek R1, Grok think mode)
		if delta.ReasoningContent != "" {
			reasonBuf.WriteString(delta.ReasoningContent)
			ch <- provider.Chunk{Type: provider.ChunkReasoning, Text: delta.ReasoningContent}
		}

		// Text content
		if delta.Content != "" {
			textBuf.WriteString(delta.Content)
			ch <- provider.Chunk{Type: provider.ChunkText, Text: delta.Content}
		}

		// Tool calls (streaming fragments)
		for _, tc := range delta.ToolCalls {
			if toolCallBuf == nil || toolCallBuf.index != tc.Index {
				// Flush previous tool call
				if toolCallBuf != nil && toolCallBuf.name != "" {
					ch <- provider.Chunk{
						Type:     provider.ChunkToolCall,
						ToolCall: toolCallBuf.finalize(),
					}
				}
				toolCallBuf = &partialToolCall{index: tc.Index}
			}
			if tc.ID != "" {
				toolCallBuf.id = tc.ID
			}
			if tc.Function.Name != "" {
				toolCallBuf.name = tc.Function.Name
			}
			toolCallBuf.args.WriteString(tc.Function.Arguments)
			// Emit ToolCallStart only when both ID and Name are known
			if tc.ID != "" || (tc.Function.Name != "" && toolCallBuf.id != "") {
				if toolCallBuf.id != "" && toolCallBuf.name != "" && !toolCallBuf.started {
					toolCallBuf.started = true
					ch <- provider.Chunk{
						Type: provider.ChunkToolCallStart,
						ToolCall: &provider.ToolCall{
							ID:   toolCallBuf.id,
							Name: toolCallBuf.name,
						},
					}
				}
			}
		}

		// Finish reason (from non-streaming field on last chunk)
		if sse.Choices[0].FinishReason != "" && toolCallBuf != nil && toolCallBuf.name != "" {
			ch <- provider.Chunk{
				Type:     provider.ChunkToolCall,
				ToolCall: toolCallBuf.finalize(),
			}
			toolCallBuf = nil
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: err}}
		return
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
}

type partialToolCall struct {
	index   int
	id      string
	name    string
	args    strings.Builder
	started bool // ChunkToolCallStart already emitted for this call
}

func (p *partialToolCall) finalize() *provider.ToolCall {
	args := p.args.String()
	if args == "" {
		args = "{}"
	}
	return &provider.ToolCall{
		ID:        p.id,
		Name:      p.name,
		Arguments: args,
	}
}

// ── Request building ──────────────────────────────────────

type chatRequest struct {
	Model       string          `json:"model"`
	Messages    []chatMessage   `json:"messages"`
	Tools       []chatTool      `json:"tools,omitempty"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream"`
	StreamOpts  *streamOptions  `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role             string     `json:"role"`
	Content          any        `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatToolFunc `json:"function"`
}

type chatToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func buildRequest(req provider.Request, model string) chatRequest {
	msgs := make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		cm := chatMessage{
			Role:             string(m.Role),
			ReasoningContent: m.ReasoningContent,
			ToolCallID:       m.ToolCallID,
			Name:             m.Name,
		}

		if m.Role == provider.RoleTool {
			cm.Content = m.Content
		} else if len(m.Images) > 0 {
			// Vision content: array of text + image parts
			parts := make([]map[string]any, 0, 1+len(m.Images))
			if m.Content != "" {
				parts = append(parts, map[string]any{"type": "text", "text": m.Content})
			}
			for _, img := range m.Images {
				parts = append(parts, map[string]any{
					"type":      "image_url",
					"image_url": map[string]string{"url": img},
				})
			}
			cm.Content = parts
		} else {
			cm.Content = m.Content
		}

		// Assistant tool calls
		if len(m.ToolCalls) > 0 {
			cm.ToolCalls = make([]chatToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				cm.ToolCalls[i] = chatToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: chatFunction{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				}
			}
		}
		msgs = append(msgs, cm)
	}

	// Build tool schemas
	tools := make([]chatTool, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = chatTool{
			Type: "function",
			Function: chatToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	return chatRequest{
		Model:       model,
		Messages:    msgs,
		Tools:       tools,
		Temperature: req.Temperature,
		MaxTokens:   maxTokens,
		Stream:      true,
		StreamOpts:  &streamOptions{IncludeUsage: true},
	}
}
