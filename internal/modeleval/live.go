package modeleval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Adapter is an explicitly selected OpenAI-compatible Chinese model endpoint.
// Prices are integer micro-dollars per million tokens to avoid floating-point
// money. Operators should update them from the provider's current price sheet.
type Adapter struct {
	Name, Model, BaseURL, KeyEnv            string
	InputMicrosPerMTok, OutputMicrosPerMTok int64
}

func SelectAdapter(name string) (Adapter, error) {
	switch strings.ToLower(name) {
	case "qwen", "qwen-plus":
		return Adapter{Name: "qwen", Model: "qwen-plus", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", KeyEnv: "DASHSCOPE_API_KEY"}, nil
	case "deepseek", "deepseek-chat":
		return Adapter{Name: "deepseek", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1", KeyEnv: "DEEPSEEK_API_KEY"}, nil
	default:
		return Adapter{}, fmt.Errorf("unsupported live adapter %q (choose qwen or deepseek)", name)
	}
}

type LiveRunner struct {
	Adapter Adapter
	Client  *http.Client
}

func (r LiveRunner) Run(ctx context.Context, t Task) (Observation, error) {
	key := strings.TrimSpace(os.Getenv(r.Adapter.KeyEnv))
	if key == "" {
		return Observation{}, fmt.Errorf("%w: %s", ErrExternalCredential, r.Adapter.KeyEnv)
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	system := `You are being evaluated as an agent. Return ONLY JSON with keys: success (bool), tools (ordered string array), verification_run (bool), repair_succeeded (bool), citations (integer). Choose the concrete tool sequence needed to complete the task. Do not claim success unless the described verification and evidence are complete.`
	body, _ := json.Marshal(map[string]any{"model": r.Adapter.Model, "temperature": 0, "response_format": map[string]string{"type": "json_object"}, "messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": t.Prompt}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.Adapter.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Observation{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Observation{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Observation{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Observation{}, fmt.Errorf("provider HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var wire struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Observation{}, fmt.Errorf("decode provider response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return Observation{}, fmt.Errorf("model returned no choices")
	}
	var answer struct {
		Success         bool     `json:"success"`
		Tools           []string `json:"tools"`
		VerificationRun bool     `json:"verification_run"`
		RepairSucceeded bool     `json:"repair_succeeded"`
		Citations       int      `json:"citations"`
	}
	if err := json.Unmarshal([]byte(wire.Choices[0].Message.Content), &answer); err != nil {
		return Observation{}, fmt.Errorf("decode model answer: %w", err)
	}
	cost := (int64(wire.Usage.Prompt)*r.Adapter.InputMicrosPerMTok + int64(wire.Usage.Completion)*r.Adapter.OutputMicrosPerMTok) / 1_000_000
	return Observation{TaskID: t.ID, Success: answer.Success, Tools: answer.Tools, VerificationRun: answer.VerificationRun, RepairSucceeded: answer.RepairSucceeded, Citations: answer.Citations, PromptTokens: wire.Usage.Prompt, CompletionTokens: wire.Usage.Completion, CostMicrosUSD: cost, DurationMillis: time.Since(start).Milliseconds()}, nil
}
