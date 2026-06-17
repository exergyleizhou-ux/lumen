package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"lumen/internal/provider"
)

func TestSSETextOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" World\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, err := New(provider.Config{
		Name:    "test",
		BaseURL: srv.URL,
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	ch, err := prov.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Temperature: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	var texts []string
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			texts = append(texts, chunk.Text)
		case provider.ChunkDone:
			// ok
		case provider.ChunkError:
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}

	joined := strings.Join(texts, "")
	if joined != "Hello World" {
		t.Errorf("text: want 'Hello World', got %q", joined)
	}
}

func TestSSEToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Stream a tool call in fragments
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":""}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\""}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"test.go\"}"}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"finish_reason":"tool_calls"}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})

	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "read test.go"}},
		Temperature: 0,
	})

	var toolCalls []*provider.ToolCall
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkToolCall:
			toolCalls = append(toolCalls, chunk.ToolCall)
		case provider.ChunkError:
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "read_file" {
		t.Errorf("tool name: want read_file, got %s", toolCalls[0].Name)
	}
	if toolCalls[0].ID != "call_1" {
		t.Errorf("tool ID: want call_1, got %s", toolCalls[0].ID)
	}
	args := toolCalls[0].Arguments
	if !strings.Contains(args, "test.go") {
		t.Errorf("tool args should contain test.go, got %s", args)
	}
}

func TestSseeasoningContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"reasoning_content":"Let me think..."}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"Answer is 42"}}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "deepseek-reasoner"})

	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "?"}},
		Temperature: 0,
	})

	var reasonings, texts []string
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkReasoning:
			reasonings = append(reasonings, chunk.Text)
		case provider.ChunkText:
			texts = append(texts, chunk.Text)
		case provider.ChunkError:
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}

	if strings.Join(reasonings, "") != "Let me think..." {
		t.Errorf("reasoning mismatch: %q", strings.Join(reasonings, ""))
	}
	if strings.Join(texts, "") != "Answer is 42" {
		t.Errorf("text mismatch: %q", strings.Join(texts, ""))
	}
}

func TestSSEUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})

	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Temperature: 0,
	})

	var usage *provider.Usage
	for chunk := range ch {
		if chunk.Type == provider.ChunkUsage {
			usage = chunk.Usage
		}
	}

	if usage == nil {
		t.Fatal("expected usage chunk")
	}
	if usage.TotalTokens != 12 {
		t.Errorf("total: want 12, got %d", usage.TotalTokens)
	}
}

func TestSSEMultipleToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// First tool call
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"grep","arguments":"{}"}}]}}]}` + "\n\n"))
		// Second tool call
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"t2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"finish_reason":"tool_calls"}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})

	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "find it"}},
		Temperature: 0,
	})

	var toolCalls []*provider.ToolCall
	for chunk := range ch {
		if chunk.Type == provider.ChunkToolCall {
			toolCalls = append(toolCalls, chunk.ToolCall)
		}
	}

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "grep" {
		t.Errorf("first: want grep, got %s", toolCalls[0].Name)
	}
	if toolCalls[1].Name != "read_file" {
		t.Errorf("second: want read_file, got %s", toolCalls[1].Name)
	}
}

func TestSSEChunkToolCallStartDelayed(t *testing.T) {
	// DeepSeek often sends ID without name first — name comes in later delta
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// First: ID only, no name (DeepSeek behavior)
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"","arguments":""}}]}}]}` + "\n\n"))
		// Second: name arrives
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"finish_reason":"tool_calls"}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})

	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "list"}},
		Temperature: 0,
	})

	var starts, fulls []*provider.ToolCall
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkToolCallStart:
			starts = append(starts, chunk.ToolCall)
		case provider.ChunkToolCall:
			fulls = append(fulls, chunk.ToolCall)
		}
	}

	// Start should only fire once, with both ID and Name populated
	if len(starts) != 1 {
		t.Fatalf("expected 1 ChunkToolCallStart, got %d", len(starts))
	}
	if starts[0].Name == "" {
		t.Error("ChunkToolCallStart should not have empty name (delayed until known)")
	}
	if starts[0].Name != "bash" {
		t.Errorf("start name: want bash, got %q", starts[0].Name)
	}

	// Full tool call should fire once
	if len(fulls) != 1 {
		t.Fatalf("expected 1 ChunkToolCall, got %d", len(fulls))
	}
}

func TestSSEHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})

	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Temperature: 0,
	})

	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			return // expected
		}
	}
	t.Error("expected an error chunk from 500 response")
}

func TestStreamDoesNotRetryNonRetryable(t *testing.T) {
	// 402 Insufficient Balance is a permanent error — retrying wastes time and
	// muddies the error message. The provider must fail fast: one request, then
	// surface a clear error chunk.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(402)
		w.Write([]byte(`{"error":{"message":"Insufficient Balance"}}`))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})

	var gotErr error
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			gotErr = chunk.Err
		}
	}
	if gotErr == nil {
		t.Fatal("expected an error chunk from 402")
	}
	if !strings.Contains(gotErr.Error(), "402") {
		t.Errorf("error should name the HTTP status, got %q", gotErr.Error())
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("402 is permanent — want exactly 1 request, got %d (a non-retryable error was retried)", n)
	}
}

func TestStreamRetriesTransient(t *testing.T) {
	// 503 is transient — the provider should retry and recover. Guards the retry
	// path so the no-retry fix above does not disable legitimate retries.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(503)
			w.Write([]byte("temporarily unavailable"))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"ok"}}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})

	var text string
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text += chunk.Text
		case provider.ChunkError:
			t.Fatalf("503 should be retried and recover, got error: %v", chunk.Err)
		}
	}
	if text != "ok" {
		t.Errorf("want recovered text 'ok', got %q", text)
	}
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Fatalf("want 2 requests (1 failed + 1 retry), got %d", n)
	}
}

func TestSSEFinishReasonLength(t *testing.T) {
	// finish_reason "length" means the model was cut off by max_tokens. Surface a
	// visible marker so the user can tell the answer is truncated.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"Partial answer"}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"delta":{"content":" that got cut"}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"finish_reason":"length"}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})

	var texts []string
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			texts = append(texts, chunk.Text)
		case provider.ChunkError:
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
	}
	joined := strings.Join(texts, "")
	if !strings.Contains(joined, "Partial answer that got cut") {
		t.Errorf("expected streamed text, got %q", joined)
	}
	if !strings.Contains(joined, "truncated") {
		t.Errorf("expected a truncation marker, got %q", joined)
	}
}

func TestSSEInBandError(t *testing.T) {
	// OpenAI-compatible proxies often return 200 + an in-band {"error":...} event
	// (rate limit / quota / overloaded). It must surface as a ChunkError, not be
	// silently dropped into an empty turn.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"error":{"message":"rate limit exceeded","type":"rate_limit_error"}}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	var gotErr error
	for c := range ch {
		if c.Type == provider.ChunkError {
			gotErr = c.Err
		}
	}
	if gotErr == nil {
		t.Fatal("in-band error event should surface as a ChunkError")
	}
	if !strings.Contains(gotErr.Error(), "rate limit exceeded") {
		t.Errorf("error should carry the provider message, got %q", gotErr.Error())
	}
}

func TestSSEInBandErrorAfterContentPreservesText(t *testing.T) {
	// A trailing in-band error AFTER valid content was streamed must NOT discard
	// the partial answer — keep it and append a marker, ending normally.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"Here is the answer"}}]}` + "\n\n"))
		w.Write([]byte(`data: {"error":{"message":"upstream hiccup"}}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	var texts []string
	var gotErr error
	for c := range ch {
		switch c.Type {
		case provider.ChunkText:
			texts = append(texts, c.Text)
		case provider.ChunkError:
			gotErr = c.Err
		}
	}
	joined := strings.Join(texts, "")
	if gotErr != nil {
		t.Errorf("trailing error after content must not become a ChunkError, got %v", gotErr)
	}
	if !strings.Contains(joined, "Here is the answer") {
		t.Errorf("partial answer must be preserved, got %q", joined)
	}
	if !strings.Contains(joined, "upstream hiccup") {
		t.Errorf("error marker should be appended, got %q", joined)
	}
}

func TestBuildRequest(t *testing.T) {
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "bot"},
			{Role: provider.RoleUser, Content: "hello"},
		},
		Tools: []provider.ToolSchema{
			{Name: "bash", Description: "run command", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
		Temperature: 0.7,
		MaxTokens:   2048,
	}

	cr := buildRequest(req, "test-model")
	if cr.Model != "test-model" {
		t.Errorf("model: want test-model, got %s", cr.Model)
	}
	if !cr.Stream {
		t.Error("stream should be true")
	}
	if len(cr.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(cr.Messages))
	}
	if len(cr.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(cr.Tools))
	}
	if cr.Temperature != 0.7 {
		t.Errorf("temperature: want 0.7, got %f", cr.Temperature)
	}
	if cr.MaxTokens != 2048 {
		t.Errorf("max_tokens: want 2048, got %d", cr.MaxTokens)
	}
}

func TestBuildRequestToolCalls(t *testing.T) {
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, Content: "", ToolCalls: []provider.ToolCall{
				{ID: "c1", Name: "bash", Arguments: `{"command":"ls"}`},
			}},
			{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "file1.txt"},
		},
		Temperature: 0,
	}

	cr := buildRequest(req, "model")
	if len(cr.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(cr.Messages))
	}
	if len(cr.Messages[0].ToolCalls) != 1 {
		t.Error("assistant message should have tool_calls")
	}
	if cr.Messages[0].ToolCalls[0].ID != "c1" {
		t.Errorf("tool call ID: want c1, got %s", cr.Messages[0].ToolCalls[0].ID)
	}
}

func TestProviderName(t *testing.T) {
	prov, err := New(provider.Config{Name: "my-deepseek", BaseURL: "http://localhost", Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if prov.Name() != "my-deepseek" {
		t.Errorf("Name: want my-deepseek, got %s", prov.Name())
	}
}

func TestStreamContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	prov, _ := New(provider.Config{Name: "test", BaseURL: "http://127.0.0.1:1", Model: "test"})

	ch, err := prov.Stream(ctx, provider.Request{
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Temperature: 0,
	})
	if err != nil {
		// Connection refusal before stream is ok too
		return
	}

	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			return // expected
		}
	}
}

func TestSSEUsageDeepSeekCacheTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n"))
		// DeepSeek's usage uses prompt_cache_hit_tokens / prompt_cache_miss_tokens.
		w.Write([]byte(`data: {"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":120,"completion_tokens":5,"total_tokens":125,"prompt_cache_hit_tokens":100,"prompt_cache_miss_tokens":20,"prompt_tokens_details":{"cached_tokens":100}}}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})

	var usage *provider.Usage
	for chunk := range ch {
		if chunk.Type == provider.ChunkUsage {
			usage = chunk.Usage
		}
	}
	if usage == nil {
		t.Fatal("expected a usage chunk")
	}
	if usage.CacheHitTokens != 100 {
		t.Errorf("CacheHitTokens: want 100, got %d", usage.CacheHitTokens)
	}
	if usage.CacheMissTokens != 20 {
		t.Errorf("CacheMissTokens: want 20, got %d", usage.CacheMissTokens)
	}
	if usage.PromptTokens != 120 || usage.TotalTokens != 125 {
		t.Errorf("prompt/total: want 120/125, got %d/%d", usage.PromptTokens, usage.TotalTokens)
	}
}

func TestSSEUsageOpenAICachedTokensFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n"))
		// OpenAI reports cache only via prompt_tokens_details.cached_tokens.
		w.Write([]byte(`data: {"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":120,"completion_tokens":5,"total_tokens":125,"prompt_tokens_details":{"cached_tokens":80}}}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "test"})
	ch, _ := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})

	var usage *provider.Usage
	for chunk := range ch {
		if chunk.Type == provider.ChunkUsage {
			usage = chunk.Usage
		}
	}
	if usage == nil {
		t.Fatal("expected a usage chunk")
	}
	if usage.CacheHitTokens != 80 {
		t.Errorf("CacheHitTokens (OpenAI fallback): want 80, got %d", usage.CacheHitTokens)
	}
	if usage.CacheMissTokens != 40 {
		t.Errorf("CacheMissTokens (120-80): want 40, got %d", usage.CacheMissTokens)
	}
}
