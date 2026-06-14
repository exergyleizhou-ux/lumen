package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
