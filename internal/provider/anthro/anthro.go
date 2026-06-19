// Package anthro provides an Anthropic Claude provider implementing
// provider.Provider. The Anthropic Messages API uses a different wire format
// from OpenAI but the same HTTP streaming pattern.
package anthro

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"lumen/internal/provider"
)

func init() { provider.Register("anthropic", New) }

type Provider struct {
	name    string
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Provider{
		name:   cfg.Name,
		baseURL: cfg.BaseURL,
		model:  cfg.Model,
		apiKey: cfg.APIKey,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}, nil
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 64)
	go func() {
		defer close(ch)
		provider.StreamWithRetry(ctx, ch, func(int) error {
			return p.stream(ctx, req, ch)
		})
	}()
	return ch, nil
}

// stream performs one streaming attempt. It returns a typed error for setup and
// HTTP-status failures (so StreamWithRetry can classify/retry); successful SSE
// bodies are streamed to ch and it returns nil.
func (p *Provider) stream(ctx context.Context, req provider.Request, ch chan<- provider.Chunk) error {
	body := p.buildRequest(req)
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(b))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err // network error — retryable
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return provider.ClassifyHTTPError(p.name, resp.StatusCode, body)
	}

	p.parseSSE(ctx, resp.Body, ch)
	return nil
}

type anthroRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Messages  []anthroMsg   `json:"messages"`
	System    string        `json:"system,omitempty"`
	Stream    bool          `json:"stream"`
}

type anthroMsg struct {
	Role    string          `json:"role"`
	Content []anthroBlock   `json:"content"`
}

type anthroBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// tool_use blocks (assistant)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result blocks (user)
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

func (p *Provider) buildRequest(req provider.Request) anthroRequest {
	var system string
	var msgs []anthroMsg

	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem {
			system = m.Content
			continue
		}
		text := m.Content
		if text == "" && m.ReasoningContent != "" {
			text = m.ReasoningContent
		}

		// Tool result → a structured tool_result block (not JSON-in-text, which
		// Anthropic rejects, and which broke on quotes/newlines in the content).
		if m.Role == provider.RoleTool {
			if m.ToolCallID != "" {
				msgs = append(msgs, anthroMsg{
					Role:    "user",
					Content: []anthroBlock{{Type: "tool_result", ToolUseID: m.ToolCallID, Content: text}},
				})
			}
			continue
		}

		// Assistant turn with tool calls → ONE message holding the optional text
		// block plus structured tool_use blocks (id/name/input as native fields).
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			var blocks []anthroBlock
			if text != "" {
				blocks = append(blocks, anthroBlock{Type: "text", Text: text})
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Arguments)
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthroBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input})
			}
			msgs = append(msgs, anthroMsg{Role: "assistant", Content: blocks})
			continue
		}

		msgs = append(msgs, anthroMsg{Role: string(m.Role), Content: []anthroBlock{{Type: "text", Text: text}}})
	}

	maxTokens := 4096
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	return anthroRequest{
		Model:     p.model,
		MaxTokens: maxTokens,
		Messages:  msgs,
		System:    system,
		Stream:    true,
	}
}

// parseSSE parses Anthropic SSE stream format (server-sent events).
func (p *Provider) parseSSE(ctx context.Context, r io.Reader, ch chan<- provider.Chunk) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var textBuf strings.Builder
	var reasoningBuf strings.Builder
	var hasContent bool

	for sc.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "error":
			// In-band error event: surface it instead of a silent empty turn.
			ch <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("provider error: %s", event.Error.Message)}
			return
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				hasContent = true
				textBuf.WriteString(event.Delta.Text)
				ch <- provider.Chunk{
					Type: provider.ChunkText,
					Text: event.Delta.Text,
				}
			case "thinking_delta":
				reasoningBuf.WriteString(event.Delta.Text)
				ch <- provider.Chunk{
					Type: provider.ChunkReasoning,
					Text: event.Delta.Text,
				}
			}

		case "message_stop":
			ch <- provider.Chunk{
				Type: provider.ChunkDone,
				Text: textBuf.String(),
			}
			if reasoningBuf.Len() > 0 {
				ch <- provider.Chunk{
					Type: provider.ChunkReasoning,
					Text: reasoningBuf.String(),
				}
			}
		}

		// Usage
		if event.Usage.InputTokens > 0 || event.Usage.OutputTokens > 0 {
			ch <- provider.Chunk{
				Type: provider.ChunkUsage,
				Usage: &provider.Usage{
					PromptTokens:     event.Usage.InputTokens,
					CompletionTokens: event.Usage.OutputTokens,
					TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
				},
			}
		}
		if event.Message.Usage.InputTokens > 0 || event.Message.Usage.OutputTokens > 0 {
			ch <- provider.Chunk{
				Type: provider.ChunkUsage,
				Usage: &provider.Usage{
					PromptTokens:     event.Message.Usage.InputTokens,
					CompletionTokens: event.Message.Usage.OutputTokens,
					TotalTokens:      event.Message.Usage.InputTokens + event.Message.Usage.OutputTokens,
				},
			}
		}

		if !hasContent && textBuf.Len() == 0 {
			for _, c := range event.Message.Content {
				if c.Type == "text" && c.Text != "" {
					hasContent = true
					textBuf.WriteString(c.Text)
					ch <- provider.Chunk{Type: provider.ChunkText, Text: c.Text}
				}
			}
		}
	}

	// Mid-stream transport cut: surface it instead of silently truncating the
	// reply (mirrors the openai provider). ctx.Err()==nil skips normal cancels.
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: err}}
	}
}
