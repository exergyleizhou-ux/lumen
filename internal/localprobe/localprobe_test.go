package localprobe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sseToolCall is a minimal OpenAI-style streamed response that emits a tool
// call, then usage, then [DONE].
const sseToolCall = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"edit_file","arguments":"{\"path\":\"x\"}"}}]}}]}

data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":50,"completion_tokens":20,"total_tokens":70}}

data: [DONE]

`

// sseTextOnly is a model that ignores the tool and only emits prose — i.e. it
// "talks about" editing but never calls the tool.
const sseTextOnly = `data: {"choices":[{"delta":{"content":"Sure, I would edit the file."}}]}

data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":50,"completion_tokens":10,"total_tokens":60}}

data: [DONE]

`

func sseServer(t *testing.T, models []string, stream string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var b strings.Builder
		b.WriteString(`{"data":[`)
		for i, m := range models {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"id":"` + m + `"}`)
		}
		b.WriteString(`]}`)
		_, _ = w.Write([]byte(b.String()))
	})
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// emit some elapsed time so tokens/sec is finite and positive
		time.Sleep(5 * time.Millisecond)
		_, _ = w.Write([]byte(stream))
	})
	return httptest.NewServer(mux)
}

func TestProbeDetectsToolCall(t *testing.T) {
	srv := sseServer(t, []string{"qwen3.6-27b"}, sseToolCall)
	defer srv.Close()

	res := Probe(context.Background(), Config{BaseURL: srv.URL, Model: "qwen3.6-27b"})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if !res.CanToolCall {
		t.Errorf("CanToolCall = false, want true (server emitted a tool_call)")
	}
	if res.TokensPerSec <= 0 {
		t.Errorf("TokensPerSec = %v, want > 0", res.TokensPerSec)
	}
	if res.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want 20", res.CompletionTokens)
	}
}

func TestProbeDetectsNoToolCall(t *testing.T) {
	srv := sseServer(t, []string{"gemma-4"}, sseTextOnly)
	defer srv.Close()

	res := Probe(context.Background(), Config{BaseURL: srv.URL, Model: "gemma-4"})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.CanToolCall {
		t.Errorf("CanToolCall = true, want false (server emitted only text)")
	}
	if !strings.Contains(res.TextReply, "edit the file") {
		t.Errorf("TextReply = %q, want it to contain the prose reply", res.TextReply)
	}
}

func TestProbeListsServedModels(t *testing.T) {
	srv := sseServer(t, []string{"qwen3.6-27b", "gemma-4-coder"}, sseToolCall)
	defer srv.Close()

	res := Probe(context.Background(), Config{BaseURL: srv.URL, Model: "qwen3.6-27b"})
	if len(res.ServedModels) != 2 {
		t.Fatalf("ServedModels = %v, want 2 entries", res.ServedModels)
	}
	if res.ServedModels[0] != "qwen3.6-27b" {
		t.Errorf("ServedModels[0] = %q, want qwen3.6-27b", res.ServedModels[0])
	}
}

func TestProbeUnreachableEndpoint(t *testing.T) {
	// Nothing is listening here; probe must return an error, not panic.
	res := Probe(context.Background(), Config{BaseURL: "http://127.0.0.1:1/v1", Model: "x"})
	if res.Err == nil {
		t.Error("Err = nil, want a connection error for an unreachable endpoint")
	}
	if res.CanToolCall {
		t.Error("CanToolCall = true on an unreachable endpoint")
	}
}
