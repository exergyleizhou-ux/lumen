// Package localprobe checks whether a local (or any OpenAI-compatible) model
// endpoint can actually drive lumen's agent loop. The decisive question is not
// "can it chat" but "can it emit an OpenAI tool_call" — a model that only
// produces prose can talk about editing a file but can never invoke edit_file,
// so it cannot drive the agent.
//
// The probe deliberately reuses the real provider/openai client, so it exercises
// the exact code path the agent uses. It also reports throughput (tokens/sec),
// which on local Metal inference is the real bottleneck (cost ≈ $0).
package localprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"lumen/internal/provider"
	_ "lumen/internal/provider/openai" // register the "openai" wire kind
)

// Config selects the endpoint to probe.
type Config struct {
	Label   string        // human label for the endpoint (e.g. preset name)
	BaseURL string
	APIKey  string        // empty/placeholder is fine for local endpoints
	Model   string        // served model id; if empty the first /v1/models id is used
	Timeout time.Duration // overall probe timeout; defaults to 90s
}

// Result is the capability verdict for one endpoint.
type Result struct {
	Name             string   // endpoint label (from Config.Label)
	Model            string   // model id actually probed
	ServedModels     []string // ids reported by GET /v1/models (best effort)
	CanToolCall      bool     // did the model emit an OpenAI tool_call?
	TextReply        string   // prose the model produced (for the no-tool-call case)
	PromptTokens     int
	CompletionTokens int
	TokensPerSec     float64 // completion tokens / wall-clock seconds
	ElapsedMs        int64
	Err              error // transport/setup failure; CanToolCall is false when set
}

// probeTool is the single tool offered to the model. A model that can drive the
// agent will call it; one that cannot will answer in prose instead.
var probeTool = provider.ToolSchema{
	Name:        "edit_file",
	Description: "Replace a line in a file. Call this to make the requested edit.",
	Parameters: json.RawMessage(`{
		"type":"object",
		"properties":{
			"path":{"type":"string","description":"file path"},
			"line":{"type":"integer","description":"1-based line number"},
			"text":{"type":"string","description":"replacement text"}
		},
		"required":["path","line","text"]
	}`),
}

// Probe runs the minimal "read file → change one line via a tool call" check
// against the endpoint and returns the capability verdict. It never panics on a
// dead endpoint — failures are reported in Result.Err.
func Probe(ctx context.Context, cfg Config) Result {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	res := Result{Name: cfg.Label, Model: cfg.Model}

	// Best-effort model discovery; never fatal — some servers omit /v1/models.
	res.ServedModels = listModels(ctx, cfg.BaseURL, cfg.APIKey)
	if res.Model == "" && len(res.ServedModels) > 0 {
		res.Model = res.ServedModels[0]
	}

	prov, err := provider.New("openai", provider.Config{
		Name:    "probe",
		BaseURL: cfg.BaseURL,
		Model:   res.Model,
		APIKey:  cfg.APIKey,
	})
	if err != nil {
		res.Err = fmt.Errorf("create provider: %w", err)
		return res
	}

	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "You are a coding agent. When asked to edit a file, you MUST call the edit_file tool. Do not answer in prose."},
			{Role: provider.RoleUser, Content: "In main.go, change line 3 to say hello. Use the edit_file tool."},
		},
		Tools:       []provider.ToolSchema{probeTool},
		Temperature: 0,
		MaxTokens:   256,
	}

	start := time.Now()
	ch, err := prov.Stream(ctx, req)
	if err != nil {
		res.Err = err
		return res
	}

	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkToolCallStart, provider.ChunkToolCall:
			res.CanToolCall = true
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkUsage:
			if chunk.Usage != nil {
				res.PromptTokens = chunk.Usage.PromptTokens
				res.CompletionTokens = chunk.Usage.CompletionTokens
			}
		case provider.ChunkError:
			if chunk.Err != nil {
				res.Err = chunk.Err
			}
		}
	}
	elapsed := time.Since(start)

	res.TextReply = strings.TrimSpace(text.String())
	res.ElapsedMs = elapsed.Milliseconds()
	if secs := elapsed.Seconds(); secs > 0 && res.CompletionTokens > 0 {
		res.TokensPerSec = float64(res.CompletionTokens) / secs
	}
	return res
}

// listModels queries GET {baseURL}/models and returns the served model ids. Any
// failure yields nil — discovery is a convenience, not a requirement.
func listModels(ctx context.Context, baseURL, apiKey string) []string {
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return nil
	}
	var ids []string
	for _, m := range parsed.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
