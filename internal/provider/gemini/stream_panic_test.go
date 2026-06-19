package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"lumen/internal/provider"
)

// Gemini wraps SSE chunks in a JSON array and the unwrap did data[1:len-1].
// A framing where the array bracket arrives on its own line ("data: [") makes
// len(data)==1, so data[1:0] panics with slice-bounds-out-of-range, crashing
// the whole agent. The stream must tolerate bracket-only / short lines.
func TestGeminiStreamSurvivesBracketOnlyLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [\n\n"))                                                                          // the crasher
		w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]}}]}` + "\n\n")) // a real chunk
		w.Write([]byte("data: ]\n\n"))
	}))
	defer srv.Close()

	prov, err := New(provider.Config{Name: "gemini", BaseURL: srv.URL, Model: "gemini-x"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Draining must not panic; the real chunk's text should still arrive.
	var text string
	for c := range ch {
		if c.Type == provider.ChunkText {
			text += c.Text
		}
	}
	if text != "hi" {
		t.Fatalf("text = %q, want %q (stream must survive the bracket-only line)", text, "hi")
	}
}
