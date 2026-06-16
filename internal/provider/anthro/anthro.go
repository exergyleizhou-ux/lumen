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
		p.stream(ctx, req, ch)
	}()
	return ch, nil
}

func (p *Provider) stream(ctx context.Context, req provider.Request, ch chan<- provider.Chunk) {
	body := p.buildRequest(req)
	b, err := json.Marshal(body)
	if err != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: err}
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(b))
	if err != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: err}
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: err}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: &provider.AuthError{
			Provider: p.name, Status: resp.StatusCode,
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

		content := []anthroBlock{{Type: "text", Text: text}}
		// Handle tool results
		if m.Role == provider.RoleTool && m.ToolCallID != "" {
			msgs = append(msgs, anthroMsg{
				Role: "user",
				Content: []anthroBlock{{
					Type: "tool_result",
					Text: fmt.Sprintf(`{"tool_use_id":"%s","content":"%s"}`, m.ToolCallID, text),
				}},
			})
			continue
		}
		if m.Role == provider.RoleTool {
			continue
		}

		role := string(m.Role)
		msgs = append(msgs, anthroMsg{Role: role, Content: content})

		// Assistant tool calls
		if len(m.ToolCalls) > 0 {
			msg := anthroMsg{Role: "assistant"}
			for _, tc := range m.ToolCalls {
				msg.Content = append(msg.Content, anthroBlock{
					Type: "tool_use",
					Text: fmt.Sprintf(`{"name":"%s","id":"%s","input":%s}`, tc.Name, tc.ID, tc.Arguments),
				})
			}
			msgs = append(msgs, msg)
		}
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
}
