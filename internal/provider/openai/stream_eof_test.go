package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lumen/internal/provider"
)

// Some OpenAI-compatible servers (and abrupt proxies) end the SSE stream at EOF
// without a final [DONE] sentinel and without a finish_reason chunk. The
// accumulated tool call must still be flushed — otherwise the agent receives a
// ChunkToolCallStart (name only) but never the finalized call with arguments,
// silently dropping the model's tool call.
func TestSSEToolCallFlushedOnStreamEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Tool call delivered across two deltas, then the stream just ends (EOF):
		// no [DONE], no finish_reason.
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":"{\"path\":"}}]}}]}` + "\n\n"))
		w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x.go\"}"}}]}}]}` + "\n\n"))
	}))
	defer srv.Close()

	prov, err := New(provider.Config{Name: "test", BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got *provider.ToolCall
	for chunk := range ch {
		if chunk.Type == provider.ChunkToolCall {
			got = chunk.ToolCall
		}
	}
	if got == nil {
		t.Fatal("finalized tool call was dropped when the stream ended without [DONE]/finish_reason")
	}
	if got.Name != "read_file" {
		t.Fatalf("tool name = %q, want read_file", got.Name)
	}
	if !strings.Contains(got.Arguments, "x.go") {
		t.Fatalf("tool args = %q, want them to include the streamed arguments", got.Arguments)
	}
}
