package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lumen/internal/provider"
)

// A functionCall part in the streamed response must be parsed and emitted as a
// ChunkToolCall — not dropped (the old geminiPart was Text-only).
func TestParseFunctionCallResponse(t *testing.T) {
	stream := "data: " + `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"edit_file","args":{"path":"a.go"}}}]}}]}` + "\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(stream))
	}))
	defer srv.Close()

	prov, _ := New(provider.Config{Name: "g", BaseURL: srv.URL, Model: "gemini-x"})
	ch, err := prov.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "fix it"}},
		Tools:    []provider.ToolSchema{{Name: "edit_file"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got *provider.ToolCall
	for c := range ch {
		if c.Type == provider.ChunkToolCall && c.ToolCall != nil {
			got = c.ToolCall
		}
	}
	if got == nil {
		t.Fatal("expected a ChunkToolCall from the functionCall part, got none")
	}
	if got.Name != "edit_file" {
		t.Errorf("name = %q, want edit_file", got.Name)
	}
	if got.Arguments != `{"path":"a.go"}` {
		t.Errorf("arguments = %q, want {\"path\":\"a.go\"}", got.Arguments)
	}
}

// An assistant message carrying tool calls must serialize to a structured
// functionCall part (role "model"), not a stringified name(args) text blob.
func TestBuildRequestAssistantToolCallsAreStructured(t *testing.T) {
	p := &Provider{name: "g", model: "gemini-x"}
	r := p.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "fix it"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "edit_file", Name: "edit_file", Arguments: `{"path":"a.go"}`}}},
		},
	})
	var fc *geminiFunctionCall
	for _, c := range r.Contents {
		for _, part := range c.Parts {
			if part.FunctionCall != nil {
				fc = part.FunctionCall
			}
		}
	}
	if fc == nil {
		t.Fatal("expected a functionCall part for the assistant tool call, got none")
	}
	if fc.Name != "edit_file" || string(fc.Args) != `{"path":"a.go"}` {
		t.Errorf("functionCall = %+v, want edit_file/{\"path\":\"a.go\"}", fc)
	}
}

// A tool result must serialize to a structured functionResponse part, not a
// "[tool_result id=…]" text blob.
func TestBuildRequestToolResultIsFunctionResponse(t *testing.T) {
	p := &Provider{name: "g", model: "gemini-x"}
	r := p.buildRequest(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleTool, ToolCallID: "edit_file", Content: "done"},
		},
	})
	var fr *geminiFunctionResponse
	for _, c := range r.Contents {
		for _, part := range c.Parts {
			if part.FunctionResponse != nil {
				fr = part.FunctionResponse
			}
		}
	}
	if fr == nil {
		t.Fatal("expected a functionResponse part for the tool result, got none")
	}
	if fr.Name != "edit_file" {
		t.Errorf("functionResponse.Name = %q, want edit_file", fr.Name)
	}
	if !strings.Contains(string(fr.Response), "done") {
		t.Errorf("functionResponse.Response = %s, want it to contain the result", fr.Response)
	}
	// sanity: the response field must be valid JSON
	if !json.Valid(fr.Response) {
		t.Errorf("functionResponse.Response is not valid JSON: %s", fr.Response)
	}
}
