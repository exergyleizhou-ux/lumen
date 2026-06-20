// Package gemini provides a Google Gemini provider implementing
// provider.Provider. Uses the generateContent streamGenerateContent API.
package gemini

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

func init() { provider.Register("gemini", New) }

type Provider struct {
	name    string
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func New(cfg provider.Config) (provider.Provider, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://generativelanguage.googleapis.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Provider{
		name:    cfg.Name,
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	// This backend has no typed functionCall part and serializes tool calls as
	// plain text, so it cannot drive the agent loop. Fail loudly when a request
	// carries tools instead of silently degrading to a chat-only reply.
	if len(req.Tools) > 0 {
		return nil, &provider.UnsupportedToolsError{Provider: p.name, Backend: "Gemini"}
	}
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

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		p.baseURL, p.model, p.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

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

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	GenerationConfig  geminiGenConfig  `json:"generationConfig"`
	Tools             []geminiToolDecl `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiGenConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
	TopP            float64 `json:"topP,omitempty"`
}

type geminiToolDecl struct {
	FunctionDeclarations []geminiFuncDecl `json:"functionDeclarations,omitempty"`
}

type geminiFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func (p *Provider) buildRequest(req provider.Request) geminiRequest {
	var systemParts []geminiPart
	var contents []geminiContent

	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem {
			systemParts = append(systemParts, geminiPart{Text: m.Content})
			continue
		}

		role := "user"
		switch m.Role {
		case provider.RoleAssistant:
			role = "model"
		case provider.RoleTool:
			// Gemini uses "function" role for tool results
			role = "function"
		default:
			role = "user"
		}

		text := m.Content
		if text == "" && len(m.ToolCalls) > 0 {
			// Tool calls from assistant: merge into one part
			var calls []string
			for _, tc := range m.ToolCalls {
				calls = append(calls, fmt.Sprintf("%s(%s)", tc.Name, tc.Arguments))
			}
			text = strings.Join(calls, "; ")
		}

		if text != "" || len(m.ToolCalls) > 0 || m.ToolCallID != "" {
			content := geminiContent{Role: role}
			if m.Role == provider.RoleTool {
				content.Parts = []geminiPart{{Text: fmt.Sprintf("[tool_result id=%s]: %s", m.ToolCallID, m.Content)}}
			} else {
				content.Parts = []geminiPart{{Text: text}}
			}
			contents = append(contents, content)
		}
	}

	req2 := geminiRequest{
		Contents: contents,
		GenerationConfig: geminiGenConfig{
			MaxOutputTokens: 4096,
			Temperature:     0.7,
			TopP:            0.95,
		},
	}
	if len(systemParts) > 0 {
		req2.SystemInstruction = &geminiContent{Parts: systemParts}
	}
	if len(req.Tools) > 0 {
		var decls []geminiFuncDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFuncDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
		req2.Tools = []geminiToolDecl{{FunctionDeclarations: decls}}
	}
	return req2
}

func (p *Provider) parseSSE(ctx context.Context, r io.Reader, ch chan<- provider.Chunk) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var textBuf strings.Builder
	var totalInput, totalOutput int

	for sc.Scan() {
		select {
		case <-ctx.Done():
			ch <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
			return
		default:
		}

		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		// Gemini SSE chunks are wrapped in a JSON array: [{"candidates": [...]}]
		// (some versions send plain objects, and the brackets may arrive on their
		// own line). Strip them with TrimPrefix/TrimSuffix — which can't panic,
		// unlike data[1:len-1] when len<2 (e.g. a bare "[") — and skip the
		// resulting empty / bracket-only / comma-only lines.
		data = strings.TrimSpace(data)
		data = strings.TrimPrefix(data, "[")
		data = strings.TrimSuffix(data, "]")
		data = strings.TrimSpace(data)
		data = strings.TrimSuffix(data, ",")
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}

		var event struct {
			Candidates []struct {
				Content struct {
					Role  string `json:"role"`
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			PromptFeedback struct {
				BlockReason string `json:"blockReason"`
			} `json:"promptFeedback"`
			UsageMetadata struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		// A blocked prompt returns 200 + a blockReason and no usable text — surface
		// it as an error instead of ending the turn as a silent empty success.
		if event.PromptFeedback.BlockReason != "" {
			ch <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("gemini blocked the prompt: %s", event.PromptFeedback.BlockReason)}
			return
		}

		for _, cand := range event.Candidates {
			hasText := false
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					hasText = true
					textBuf.WriteString(part.Text)
					ch <- provider.Chunk{Type: provider.ChunkText, Text: part.Text}
				}
			}
			// A response cut off for safety/recitation with no text is a failure,
			// not a clean completion (STOP = normal). MAX_TOKENS gets a visible
			// truncation marker, mirroring the openai provider.
			switch cand.FinishReason {
			case "SAFETY", "RECITATION", "OTHER":
				if !hasText {
					ch <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("gemini stopped the response: %s", cand.FinishReason)}
					return
				}
			case "MAX_TOKENS":
				ch <- provider.Chunk{Type: provider.ChunkText, Text: "\n[truncated: hit max_tokens]"}
			}
		}

		if event.UsageMetadata.TotalTokenCount > 0 {
			totalInput = event.UsageMetadata.PromptTokenCount
			totalOutput = event.UsageMetadata.CandidatesTokenCount
			ch <- provider.Chunk{
				Type: provider.ChunkUsage,
				Usage: &provider.Usage{
					PromptTokens:     totalInput,
					CompletionTokens: totalOutput,
					TotalTokens:      totalInput + totalOutput,
				},
			}
		}
	}

	// A cancelled/timed-out turn that ended the scan must surface as an error, not
	// a clean done that looks like a successful (truncated) completion.
	if ctx.Err() != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
		return
	}
	// Mid-stream transport cut: surface it instead of silently truncating the
	// reply (mirrors the openai provider).
	if err := sc.Err(); err != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: &provider.StreamInterruptedError{Err: err}}
		return
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
}
